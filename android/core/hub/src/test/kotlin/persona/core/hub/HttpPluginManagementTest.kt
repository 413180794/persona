package persona.core.hub

import persona.core.contracts.api.PluginConfigRejected
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpPluginManagementTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpPluginManagement) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpPluginManagement({ path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            }))
        }
    }

    private val detail = """{"id":"example-notes","name":"Example Notes","kind":"connectors","status":"needs_setup","version":"1.2.0",
        "source":"installed","enabled":true,"description":"同步笔记","contributes":{"connectors":["examplenotes"]},
        "needs_setup":["密钥「API Key」"],"publisher":"example","homepage":"https://example.com/p","origin":"dir:/opt/p",
        "previous_version":"1.1.0","read_only":false,
        "form":{"fields":[{"name":"base_url","type":"string","title":"服务地址","format":"uri","default":"https://notes.example.com","required":true},
          {"name":"mode","type":"string","enum":[{"value":"fast","title":"只要新的"},{"value":"safe"}]},
          {"name":"tags","type":"array","items":{"type":"string","enum":[{"value":"a","title":"甲"}]},"min_items":1},
          {"name":"proxy","type":"object","title":"代理","fields":[{"name":"port","type":"integer","default":8080,"minimum":1}]}]},
        "config":{"folders":["工作"]},
        "secrets":[{"name":"api_key","title":"API Key","required":true,"set":false}],
        "permissions":[{"key":"runs_code","title":"在 hub 机器上运行程序","detail":"不在沙箱里"}],
        "unknown_key":1}"""

    @Test fun listsThePluginsWithTheNewFieldsAndToleratesAnOlderHub() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"plugins":[
            {"id":"voice","name":"语音","kind":"voice","status":"running"},
            {"id":"example-notes","name":"Example Notes","kind":"connectors","status":"needs_setup","source":"installed",
             "enabled":true,"needs_setup":["密钥「API Key」"],"contributes":{"connectors":["examplenotes"]},"warnings":["w"]}],
            "can_install":true}"""))
        val list = api.plugins()
        assertEquals("/api/v1/plugins", server.takeRequest().path)
        assertEquals(listOf("voice", "example-notes"), list.map { it.id })
        assertNull(list[0].source)
        assertEquals("installed", list[1].source)
        assertEquals(listOf("密钥「API Key」"), list[1].needsSetup)
        assertEquals(mapOf("connectors" to listOf("examplenotes")), list[1].contributes)
        assertEquals(listOf("w"), list[1].warnings)
    }

    @Test fun decodesTheDetailFromOneObject() = withHub { server, api ->
        server.enqueue(MockResponse().setBody(detail))
        val d = api.plugin("example-notes")
        assertEquals("/api/v1/plugins/example-notes/-/", server.takeRequest().path)
        assertEquals("Example Notes", d.plugin.name)
        assertEquals("1.1.0", d.previousVersion)
        assertFalse(d.readOnly)
        assertEquals(listOf("base_url", "mode", "tags", "proxy"), d.form.map { it.name })
        assertEquals(JsonPrimitive("https://notes.example.com"), d.form[0].fallback)
        assertEquals(listOf("只要新的", "safe"), d.form[1].enum!!.map { it.label })
        assertEquals("甲", d.form[2].items!!.enum!!.single().label)
        assertEquals(1, d.form[2].minItems)
        assertEquals(JsonPrimitive(8080), d.form[3].fields!!.single().fallback)
        assertEquals(buildJsonObject { putJsonArray("folders") { add("工作") } }, d.config)
        assertFalse(d.secrets.single().set)
        assertEquals("runs_code", d.permissions.single().key)
    }

    @Test fun aHubJsonPluginHasNoConfigOrSecrets() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"id":"voice","name":"语音","kind":"voice","status":"running","source":"hub.json","read_only":true}"""))
        val d = api.plugin("voice")
        assertTrue(d.readOnly)
        assertTrue(d.form.isEmpty() && d.config.isEmpty() && d.secrets.isEmpty())
    }

    @Test fun switchesSavesAndSetsSecrets() = withHub { server, api ->
        repeat(4) { server.enqueue(MockResponse().setBody(detail)) }
        api.setEnabled("example-notes", false)
        server.takeRequest().let {
            assertEquals("PUT", it.method); assertEquals("/api/v1/plugins/example-notes/-/enabled", it.path)
            assertEquals("""{"enabled":false}""", it.body.readUtf8())
        }
        api.setConfig("example-notes", buildJsonObject { put("base_url", "https://x.example.com") })
        server.takeRequest().let {
            assertEquals("PUT", it.method); assertEquals("/api/v1/plugins/example-notes/-/config", it.path)
            assertEquals("""{"config":{"base_url":"https://x.example.com"}}""", it.body.readUtf8())
        }
        api.setSecret("example-notes", "api_key", "s3cret")
        server.takeRequest().let {
            assertEquals("PUT", it.method); assertEquals("/api/v1/plugins/example-notes/-/secrets/api_key", it.path)
            assertEquals("""{"value":"s3cret"}""", it.body.readUtf8())
        }
        api.clearSecret("example-notes", "api_key")
        server.takeRequest().let { assertEquals("DELETE", it.method); assertEquals("/api/v1/plugins/example-notes/-/secrets/api_key", it.path) }
    }

    @Test fun aRefusedFieldComesBackAsItsPathAndReason() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(400).setBody(
            """{"error":"proxy.host: 要是主机名","code":"invalid_config","field":"proxy.host","message":"要是主机名"}"""))
        val refused = runCatching { api.setConfig("example-notes", JsonObject(emptyMap())) }.exceptionOrNull()
        assertTrue(refused is PluginConfigRejected)
        assertEquals("proxy.host", (refused as PluginConfigRejected).field)
        assertEquals("要是主机名", refused.message)
        server.takeRequest()

        // Another 400 is the hub's error as usual.
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"bad json"}"""))
        val other = runCatching { api.setConfig("example-notes", JsonObject(emptyMap())) }.exceptionOrNull() as HubFailure
        assertEquals(400, other.status)
        assertEquals("bad json", other.message)
    }

    // OkHttpHubConnection.request throws the hub's error itself instead of returning the response (found on the
    // emulator: the refused field showed as a page error); the field must survive that.
    @Test fun aRefusedFieldSurvivesTheConnectionThrowingFirst() = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            val api = HttpPluginManagement({ path, method, body ->
                val response = http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
                if (!response.isSuccessful) throw response.use { HubFailure.from(it) }
                response
            })
            server.enqueue(MockResponse().setResponseCode(400).setBody(
                """{"error":"base_url: 要是一个网址","code":"invalid_config","field":"base_url","message":"要是一个网址"}"""))
            val refused = runCatching { api.setConfig("example-notes", JsonObject(emptyMap())) }.exceptionOrNull() as PluginConfigRejected
            assertEquals("base_url", refused.field)
            assertEquals("要是一个网址", refused.message)
        }
    }

    @Test fun refusesIdsAndNamesThatWouldBecomeAnotherPath() = withHub { server, api ->
        assertTrue(runCatching { api.plugin("..") }.exceptionOrNull() is IllegalArgumentException)
        assertTrue(runCatching { api.setSecret("example-notes", "..", "x") }.exceptionOrNull() is IllegalArgumentException)
        assertTrue(runCatching { api.clearSecret("a/b", "api_key") }.exceptionOrNull() is IllegalArgumentException)
        assertEquals(0, server.requestCount)
    }

    @Test fun anUnreadableAnswerIsAProtocolError() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"nope":true}"""))
        assertEquals(HubError.Protocol, (runCatching { api.plugins() }.exceptionOrNull() as HubFailure).kind)
        server.enqueue(MockResponse().setBody("not json"))
        assertEquals(HubError.Protocol, (runCatching { api.plugin("x") }.exceptionOrNull() as HubFailure).kind)
    }
}
