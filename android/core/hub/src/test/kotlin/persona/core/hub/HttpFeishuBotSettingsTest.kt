package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.FeishuBotUpdate

class HttpFeishuBotSettingsTest {
    private val timeouts = mutableListOf<Long?>()

    private fun <T> withHub(block: suspend (MockWebServer, HttpFeishuBotSettings) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpFeishuBotSettings { path, method, body, timeout ->
                timeouts += timeout
                val response = http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
                if (!response.isSuccessful) throw response.use { HubFailure.from(it) }
                response
            })
        }
    }

    @Test fun readsTheBotAndToleratesWhatItDoesNotKnow() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"enabled":true,"app_id":"cli_a1","domain":"https://larksuite.com","has_secret":true,"from_config":false,
            "features":{"main":true,"cards":true,"all_cards":false,"pushes":false,"sync":false,"agents":true},"connected":true,
            "last_event_at":1762300000,"bot_name":"Persona","owner":{"name":"示例用户","paired_at":1762200000},
            "target":"主对话","agents":[{"title":"修登录页","machine_id":"laptop","session_id":"s1"}],"unknown":[1]}"""))
        val status = api.status()
        assertEquals("cli_a1", status.appId)
        assertEquals("https://larksuite.com", status.domain)
        assertTrue(status.hasSecret && status.connected)
        assertEquals("示例用户", status.owner?.name)
        assertEquals("laptop", status.agents?.single()?.machineId)
        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/bots/feishu", request.path)
    }

    @Test fun savesOnlyWhatChangedAndWaitsForTheCheck() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"enabled":true}"""))
        api.update(FeishuBotUpdate(enabled = true))
        assertEquals("""{"enabled":true}""", server.takeRequest().body.readUtf8())
        server.enqueue(MockResponse().setBody("""{"app_id":"cli_b2","has_secret":true}"""))
        // An empty secret is not sent: the hub keeps the one it has.
        api.update(FeishuBotUpdate(appId = "cli_b2", appSecret = "", domain = "feishu"))
        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("""{"app_id":"cli_b2","domain":"feishu"}""", request.body.readUtf8())
        assertEquals(listOf(null, 40L), timeouts)
    }

    @Test fun theHubsReasonComesBack() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"连不上飞书开放平台：app secret invalid"}"""))
        val failure = assertThrows(HubFailure::class.java) { runBlocking { api.update(FeishuBotUpdate(appId = "cli_x", appSecret = "bad")) } }
        assertEquals(400, failure.status)
        assertTrue(failure.message.contains("app secret invalid"))
    }
}
