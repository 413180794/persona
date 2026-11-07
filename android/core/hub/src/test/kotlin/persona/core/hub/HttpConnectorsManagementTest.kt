package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.AddMailAccountRequest
import persona.core.contracts.api.GoogleFinish

class HttpConnectorsManagementTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpConnectorsManagement) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(
                server,
                HttpConnectorsManagement({ path, method, body ->
                    http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
                }),
            )
        }
    }

    @Test
    fun larkChatChoicesArePinnedThenRecentWithoutRepeats() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"conversations":[{"id":"c1","name":"设计评审","is_group":true}]}"""))
        server.enqueue(MockResponse().setBody("""{"conversations":[
            {"id":"c1","name":"设计评审"},{"id":"c2","name":"","avatar":"https://example.com/a.png","is_group":false},{"name":"no id"}
        ]}"""))
        val list = api.larkChatChoices()
        assertEquals("/api/v1/lark/pinned", (server.takeRequest().path).zhAny())
        assertEquals("/api/v1/lark/conversations?scope=chats&limit=60", (server.takeRequest().path).zhAny())
        assertEquals(listOf("c1", "c2"), (list.map { it.id }).zhAny())
        assertEquals("c2", (list[1].name).zhAny()) // a nameless chat shows its id
        assertFalse(list[1].isGroup)
        assertEquals("https://example.com/a.png", (list[1].avatar).zhAny())
    }

    @Test
    fun larkChatChoicesWithoutPinnedOnAnOlderHub() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(404))
        server.enqueue(MockResponse().setBody("""{"conversations":[{"id":"c3","name":"周会"}]}"""))
        assertEquals(listOf("c3"), (api.larkChatChoices().map { it.id }).zhAny())
    }

    @Test
    fun listConnectors() = withHub { server, api ->
        server.enqueue(
            MockResponse().setBody(
                """{"connectors":[
                {"id":"mail","name":"邮箱","status":"ok"},
                {"id":"lark","name":"飞书","status":"ok"},
                {"id":"github","name":"GitHub","status":"ok","account":"octocat"}
            ]}""",
            ),
        )
        val list = api.connectors(all = true)
        assertEquals("/api/v1/connectors?all=1", (server.takeRequest().path).zhAny())
        assertEquals(3, list.size)
        assertEquals("GitHub", (list[2].name).zhAny())
        assertEquals("octocat", (list[2].account).zhAny())
    }

    @Test
    fun listConnectorsFailureIsThrownNotAnEmptyList() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(500).setBody("""{"error":"internal","message":"boom"}"""))
        val failure = runCatching { api.connectors(all = true) }.exceptionOrNull()
        assertTrue("expected HubFailure, got $failure", failure is HubFailure)
        assertEquals(500, (failure as HubFailure).status)
    }

    @Test
    fun listConnectorsOnAnOlderHubIsEmpty() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(404))
        assertEquals(emptyList<Any>(), api.connectors(all = true))
    }

    @Test
    fun setConnectorEnabled() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"id":"github","name":"GitHub","status":"ok","enabled":true}"""))
        val result = api.setConnectorEnabled("github", true)
        val req = server.takeRequest()
        assertEquals("/api/v1/connectors/github/settings", (req.path).zhAny())
        assertEquals("PUT", (req.method).zhAny())
        assertTrue(req.body.readUtf8().contains("\"enabled\":true"))
        assertEquals(true, result.enabled)
    }

    @Test
    fun mailAccountsAndTest() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"accounts":[{"id":"a1","email":"user@example.com","provider":"imap","status":"ok"}]}"""))
        val accounts = api.mailAccounts()
        assertEquals("/api/v1/mail/accounts", (server.takeRequest().path).zhAny())
        assertEquals(1, accounts.size)
        assertEquals("user@example.com", (accounts[0].email).zhAny())

        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        val testResult = api.testMailAccount("a1")
        assertEquals("/api/v1/mail/accounts/a1/test", (server.takeRequest().path).zhAny())
        assertTrue(testResult.ok)
    }

    @Test
    fun addMailAccountAndGoogleSignIn() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"account":{"id":"a2","email":"new@example.com","provider":"feishu","status":"ok"}}"""))
        val added = api.addMailAccount(AddMailAccountRequest(email = "new@example.com", provider = "feishu"))
        assertEquals("/api/v1/mail/accounts", (server.takeRequest().path).zhAny())
        assertEquals("a2", (added.id).zhAny())

        server.enqueue(MockResponse().setBody("""{"auth_url":"https://accounts.google.com/o/oauth2/auth","callback_scheme":"com.example.app","state":"st123"}"""))
        val start = api.googleSignInStart("hint@example.com")
        val startReq = server.takeRequest()
        assertEquals("/api/v1/mail/oauth/google/start", (startReq.path).zhAny())
        assertTrue(startReq.body.readUtf8().contains("hint@example.com"))
        assertEquals("st123", (start.state).zhAny())

        server.enqueue(MockResponse().setBody("""{"account":{"id":"a3","email":"gmail@example.com","provider":"gmail","auth":"oauth2","status":"ok"}}"""))
        val finished = api.googleSignInFinish(GoogleFinish("st123", "code123"))
        assertEquals("/api/v1/mail/oauth/google/finish", (server.takeRequest().path).zhAny())
        assertEquals("gmail@example.com", (finished.email).zhAny())
    }

    @Test
    fun googleConfigAndNoneOnAnOlderHub() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"configured":false}"""))
        assertEquals(false, api.googleConfig()?.configured)
        val req = server.takeRequest()
        assertEquals("GET", (req.method).zhAny())
        assertEquals("/api/v1/mail/oauth/google/config", (req.path).zhAny())

        server.enqueue(MockResponse().setResponseCode(404))
        assertNull(api.googleConfig())

        server.enqueue(MockResponse().setResponseCode(500).setBody("""{"error":"internal"}"""))
        assertTrue(runCatching { api.googleConfig() }.exceptionOrNull() is HubFailure)
    }

    @Test
    fun larkStatusAndAccounts() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"configured":true,"connected":true,"name":"张三","last_event_at":1700000000}"""))
        val status = api.larkStatus()
        assertEquals("GET", (server.takeRequest().method).zhAny())
        assertTrue(status.connected)
        assertEquals("张三", (status.name).zhAny())

        server.enqueue(MockResponse().setBody("""{"accounts":[{"id":"u1","title":"张三","tenant":"Corp"}],"default":"u1"}"""))
        val accs = api.larkAccounts()
        assertEquals("/api/v1/lark/accounts", (server.takeRequest().path).zhAny())
        assertNotNull(accs)
        assertEquals(1, accs?.accounts?.size)
        assertEquals("Corp", (accs?.accounts?.first()?.tenant).zhAny())
    }
}
