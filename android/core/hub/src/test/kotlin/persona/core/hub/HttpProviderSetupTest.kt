package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.NewProvider
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

/** 新增模型供应商's calls: presets (none from an older hub), and the body that adds one, blank fields left out. */
class HttpProviderSetupTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpProviderSetup) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpProviderSetup { path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            })
        }
    }

    @Test fun presetsDecodeAndAnOlderHubHasNone() = withHub { server, providers ->
        server.enqueue(MockResponse().setBody("""{"presets":[{"id":"example-gateway/team","plugin":"example-gateway","name":"示例网关 · 团队",
            "kind":"launcher","base_url":"https://gateway.example.com","model":"example-pro","models":[{"id":"example-mini","name":"Example Mini"}],
            "key_hint":"以 eg- 开头","unknown":1},{"id":"example-gateway/api","kind":"api","api":"responses","base_url":"https://api.example.com/v1",
            "models":[{"id":"m1"}],"models_error":"超时"}]}"""))
        val presets = providers.presets()
        assertEquals("/api/v1/launchers/presets", (server.takeRequest().path).zhAny())
        assertEquals(listOf("example-gateway/team", "example-gateway/api"), (presets.map { it.id }).zhAny())
        assertEquals("example-pro", (presets[0].defaultModel).zhAny())
        assertEquals(listOf("example-pro", "example-mini"), (presets[0].choices.map { it.id }).zhAny())
        assertEquals(listOf("example-pro", "Example Mini"), (presets[0].choices.map { it.label }).zhAny())
        assertEquals("以 eg- 开头", (presets[0].keyHint).zhAny())
        assertEquals("m1", (presets[1].defaultModel).zhAny())
        assertEquals("超时", (presets[1].modelsError).zhAny())

        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"没有这个启动器"}"""))
        assertTrue(providers.presets().isEmpty())
        assertEquals("/api/v1/launchers/presets", (server.takeRequest().path).zhAny())
    }

    @Test fun addingSendsOnlyWhatWasFilledIn() = withHub { server, providers ->
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        providers.add(NewProvider("jv_team", "eg-secret", preset = "example-gateway/team"))
        val request = server.takeRequest()
        assertEquals("POST", (request.method).zhAny())
        assertEquals("/api/v1/launchers", (request.path).zhAny())
        val body = JsonCodec.parseToJsonElement(request.body.readUtf8()).jsonObject
        assertEquals(setOf("name", "api_key", "preset"), body.keys)
        assertEquals("example-gateway/team", (body["preset"]!!.jsonPrimitive.content).zhAny())

        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"没有这个供应商预设（提供它的扩展停用或卸载了？）"}"""))
        val refused = runCatching { providers.add(NewProvider("jv_team", "k", preset = "gone/x")) }.exceptionOrNull() as HubFailure
        assertEquals(400, refused.status)
        assertTrue(refused.message.contains("没有这个供应商预设"))
    }
}
