package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpMachineManagementTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpMachineManagement) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpMachineManagement { path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            })
        }
    }

    @Test fun launchersReconnectAndDeployGoToTheMachine() = withHub { server, machines ->
        server.enqueue(MockResponse().setBody("""{"launchers":[{"name":"claude","model":"opus"},{"name":"codex","agent_type":"codex"}]}"""))
        val launchers = machines.launchers("mac/one")
        assertEquals(listOf("claude", "codex"), launchers.map { it.name })
        assertEquals("opus", launchers[0].model)
        assertEquals(listOf("claude", "codex"), launchers.map { it.agentType })
        assertEquals("/api/v1/machines/mac%2Fone/launchers", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        machines.reconnect("mac")
        server.takeRequest().let { assertEquals("POST", it.method); assertEquals("/api/v1/machines/mac/reconnect", it.path) }

        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"这台电脑已经是 hub 这一版"}"""))
        val refused = runCatching { machines.deploy("mac") }.exceptionOrNull() as HubFailure
        assertEquals(409, refused.status)
        assertEquals("这台电脑已经是 hub 这一版", refused.message)
        server.takeRequest().let { assertEquals("POST", it.method); assertEquals("/api/v1/machines/mac/deploy", it.path) }
    }

    @Test fun putsTheEngineAndDecodesTheMachine() = withHub { server, machines ->
        server.enqueue(MockResponse().setBody("""{"id":"m1","engine":"orca","engines":[{"id":"orca","name":"Orca","available":true}]}"""))
        val saved = machines.setEngine("m1", "orca")
        val put = server.takeRequest()
        assertEquals("PUT", put.method)
        assertEquals("/api/v1/machines/m1/engine", put.path)
        assertEquals("""{"engine":"orca"}""", put.body.readUtf8())
        assertEquals("orca", saved.engine)
        assertTrue(saved.engines.single().available)

        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"unknown engine"}"""))
        val unknown = runCatching { machines.setEngine("m1", "nope") }.exceptionOrNull() as HubFailure
        assertEquals(400, unknown.status)
        assertEquals("unknown engine", unknown.message)
        server.takeRequest()
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"no such machine"}"""))
        assertEquals(404, (runCatching { machines.setEngine("x", "herdr") }.exceptionOrNull() as HubFailure).status)
        server.takeRequest()
        // Another machine's state is never taken for this one's.
        server.enqueue(MockResponse().setBody("""{"id":"other","engine":"orca"}"""))
        assertTrue(runCatching { machines.setEngine("m1", "orca") }.exceptionOrNull() is HubFailure)
    }

    @Test fun claudeSettingsWriteOneKeyAndClearingSendsJsonNull() = withHub { server, machines ->
        val table = """{"settings":[{"key":"language","title":"回复语言","type":"string","default":"","value":"中文"}]}"""
        server.enqueue(MockResponse().setBody(table))
        assertEquals("中文", machines.claudeSettings("mac").single().stringValue)
        assertEquals("/api/v1/machines/mac/claude-settings", server.takeRequest().path)

        server.enqueue(MockResponse().setBody(table))
        machines.setClaudeSetting("mac", "language", JsonPrimitive("English"))
        server.takeRequest().let {
            assertEquals("PUT", it.method)
            assertEquals("""{"key":"language","value":"English"}""", it.body.readUtf8())
        }
        server.enqueue(MockResponse().setBody("""{"settings":[{"key":"language","type":"string","value":null}]}"""))
        val cleared = machines.setClaudeSetting("mac", "language", JsonNull)
        assertEquals("""{"key":"language","value":null}""", server.takeRequest().body.readUtf8())
        assertTrue(cleared.single().isDefault)
    }
}

class HttpModelConfigurationTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpModelConfiguration) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpModelConfiguration { path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            })
        }
    }

    private val detail = """{"purpose":{"id":"lark.agent","name":"飞书 /agent","kind":"launcher","revision":4,"override":{},
        "effective":{"provider":"strong","model":"main"},"fields":["provider","model","effort"]},"providers":[]}"""

    @Test fun loadsAndPatchesOnePurposeWithNullAndRevision() = withHub { server, models ->
        server.enqueue(MockResponse().setBody("""{"purposes":[],"providers":[]}"""))
        assertTrue(models.catalog().purposes.isEmpty())
        assertEquals("/api/v1/model-config", server.takeRequest().path)
        server.enqueue(MockResponse().setBody(detail))
        val purpose = models.purpose("lark.agent").purpose
        assertEquals("/api/v1/model-config/purposes/lark.agent", server.takeRequest().path)

        server.enqueue(MockResponse().setBody(detail))
        assertTrue(models.save(purpose, ModelConfigurationDraft()) is ModelPurposeSave.Saved)
        val patch = server.takeRequest()
        assertEquals("PATCH", patch.method)
        val body = JsonCodec.parseToJsonElement(patch.body.readUtf8()).jsonObject
        assertEquals(setOf("revision", "provider", "model", "effort"), body.keys)
        assertEquals("4", body.getValue("revision").jsonPrimitive.content)
        assertEquals(JsonNull, body["provider"])

        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"revision conflict"}"""))
        assertEquals(ModelPurposeSave.Conflict, models.save(purpose, ModelConfigurationDraft(model = "next")))
        assertEquals("next", JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject["model"]?.jsonPrimitive?.content)
    }

    @Test fun oldHubsAndMissingPurposesAreToldApart() = withHub { server, models ->
        // An old hub's router: 404 with a plain text body.
        server.enqueue(MockResponse().setResponseCode(404).setBody("404 page not found"))
        assertTrue(runCatching { models.catalog() }.exceptionOrNull() is ModelConfigurationUnsupported)
        server.enqueue(MockResponse().setResponseCode(404).setBody("404 page not found"))
        assertTrue(runCatching { models.purpose("agent.new") }.exceptionOrNull() is ModelConfigurationUnsupported)
        // A new hub without that purpose says so.
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"模型用途不存在"}"""))
        val missing = runCatching { models.purpose("task.gone") }.exceptionOrNull()
        assertTrue(missing is ModelPurposeNotFound)
        assertEquals("模型用途不存在", missing?.message)
        server.enqueue(MockResponse().setResponseCode(500).setBody("""{"error":"boom"}"""))
        assertEquals(500, (runCatching { models.catalog() }.exceptionOrNull() as HubFailure).status)
    }
}
