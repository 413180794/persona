package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.AssignPurposes
import persona.core.contracts.api.ComputerOs
import persona.core.contracts.api.KeptPurpose
import persona.core.contracts.api.NewProvider
import persona.core.contracts.api.ProviderProbe
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test

/**
 * The wire of the onboarding pieces (docs/assistant/onboarding.md 5.3 / 5.4): a provider from a preset with assign, the key test and
 * the model list, a code for another device, and a computer's code, confirm and reject.
 */
class OnboardingWireTest {
    private fun <T> withHub(block: suspend (MockWebServer, OkHttpClient) -> T): T = runBlocking {
        MockWebServer().use { server -> block(server, OkHttpClient.Builder().retryOnConnectionFailure(false).build()) }
    }

    private fun providers(server: MockWebServer, http: OkHttpClient) = HttpProviderSetup({ path, method, body ->
        http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
    })

    private fun body(server: MockWebServer): Pair<String, JsonObject> {
        val request = server.takeRequest()
        return "${request.method} ${request.path}" to JsonCodec.parseToJsonElement(request.body.readUtf8()).jsonObject
    }

    @Test fun presetProviderWithAssignReadsWhatNowRunsOnIt() = withHub { server, http ->
        val api = providers(server, http)
        server.enqueue(MockResponse().setBody("""{"launcher":{"name":"deepseek","display_name":"DeepSeek","has_key":true},
            "assigned":["assistant"],"kept":[{"purpose":"agent.new","reason":"customized"}]}"""))
        val added = api.create(NewProvider("", "sk-example", preset = "builtin/deepseek", assign = AssignPurposes))
        val (line, sent) = body(server)
        assertEquals("POST /api/v1/launchers", line)
        assertEquals(setOf("name", "api_key", "preset", "assign"), sent.keys)
        assertEquals("", sent["name"]!!.jsonPrimitive.content)
        assertEquals(listOf("assistant", "agent.new"), (sent["assign"] as JsonArray).map { it.jsonPrimitive.content })
        assertEquals("deepseek", added.name)
        assertEquals("DeepSeek", added.title)
        assertEquals(listOf("assistant"), added.assigned)
        assertEquals(listOf(KeptPurpose("agent.new", "customized")), added.kept)

        // An older hub answers without assigned: nothing is said about it (not "nothing changed").
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        val old = api.create(NewProvider("jv_team", "k", preset = "example/team"))
        body(server)
        assertEquals("jv_team", old.name)
        assertNull(old.assigned)
    }

    @Test fun keyTestAndModelListSendOnlyWhatIsFilledIn() = withHub { server, http ->
        val api = providers(server, http)
        server.enqueue(MockResponse().setBody("""{"ok":true,"ms":420,"model":"deepseek-chat"}"""))
        val ok = api.test(ProviderProbe(preset = "builtin/deepseek", apiKey = " sk-example ", auth = "bearer", model = " "))
        val (line, sent) = body(server)
        assertEquals("POST /api/v1/launchers/test", line)
        assertEquals(setOf("preset", "api_key", "auth"), sent.keys)
        assertEquals("sk-example", sent["api_key"]!!.jsonPrimitive.content)
        assertTrue(ok.ok)
        assertEquals(420, ok.ms)
        assertEquals("deepseek-chat", ok.model)

        server.enqueue(MockResponse().setBody("""{"ok":false,"ms":80,"model":"m","error":"Key 不对（401）"}"""))
        assertEquals("Key 不对（401）", api.test(ProviderProbe(baseUrl = "https://gateway.example.com", apiKey = "k", model = "m")).error)
        body(server)

        server.enqueue(MockResponse().setBody("""{"models":[{"id":"m1","name":"Model One"},{"id":"m2"}]}"""))
        val found = api.discover(ProviderProbe(baseUrl = "https://gateway.example.com", apiKey = "k", auth = "api_key", model = "m1"))
        val (discover, asked) = body(server)
        assertEquals("POST /api/v1/launchers/discover", discover)
        assertFalse("the list takes no model", "model" in asked.keys)
        assertEquals(listOf("Model One", "m2"), found.models.map { it.label })
        server.enqueue(MockResponse().setBody("""{"models":[],"error":"拉不到"}"""))
        assertEquals("拉不到", api.discover(ProviderProbe(baseUrl = "https://gateway.example.com", apiKey = "k")).error)
    }

    @Test fun aCodeForAnotherDeviceAsksForTheBrowserLinkAtTheHubsAddress() = withHub { server, http ->
        val api = HttpDeviceSettings(server.url("/sub/").toString(), "example-token", "phone", http)
        val qr = """{"size":2,"rows":["10","01"]}"""
        server.enqueue(MockResponse().setBody("""{"token":"jp_example","expires_at":"2026-11-06T10:10:00+08:00","pair_url":"persona://pair?token=jp_example",
            "qr":$qr,"web_url":"https://hub.example.com/sub/#token=jp_example","web_qr":$qr}"""))
        val code = api.pairCode()
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/sub/api/v1/auth/pair-codes", request.path)
        assertEquals(server.url("/sub/").toString(), JsonCodec.parseToJsonElement(request.body.readUtf8()).jsonObject["web"]!!.jsonPrimitive.content)
        assertEquals("persona://pair?token=jp_example", code.pairUrl)
        assertEquals(java.time.OffsetDateTime.parse("2026-11-06T10:10:00+08:00").toInstant().toEpochMilli(), code.expiresAt)
        assertEquals(listOf("10", "01"), code.qr?.rows)
        assertEquals("https://hub.example.com/sub/#token=jp_example", code.webUrl)
        assertTrue("the mock server is on loopback", code.loopbackOnly)

        // An older hub: no QR codes and no web link; a QR code that does not add up is left out.
        server.enqueue(MockResponse().setBody("""{"token":"jp_x","expires_at":"bad","pair_url":"persona://pair?token=jp_x","qr":{"size":3,"rows":["1"]}}"""))
        val old = api.pairCode()
        assertNull(old.qr)
        assertNull(old.webUrl)
        assertEquals(0L, old.expiresAt)
    }

    @Test fun webBaseAndLoopback() {
        assertEquals("https://hub.example.com:18662/", HttpDeviceSettings.webBase("https://user:pw@hub.example.com:18662?x=1#f".toHttpUrl()))
        assertEquals("https://hub.example.com/persona/", HttpDeviceSettings.webBase("https://hub.example.com/persona".toHttpUrl()))
        assertTrue(HttpDeviceSettings.loopbackOnly("127.0.0.1"))
        assertTrue(HttpDeviceSettings.loopbackOnly("[::1]"))
        assertTrue(HttpDeviceSettings.loopbackOnly("dev.localhost"))
        assertFalse(HttpDeviceSettings.loopbackOnly("192.0.2.10"))
    }

    @Test fun aComputersCodeConfirmAndReject() = withHub { server, http ->
        val api = HttpComputerPairing({ path, method, body -> http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute() })
        server.enqueue(MockResponse().setBody("""{"token":"jp_x","expires_at":"2026-11-06T02:10:00Z","machine":"macbook","name":"小林的 MacBook",
            "trust":"pending","added":true,"pair_url":"persona://link?machine=macbook","command":"persona-link pair 'persona://link?machine=macbook'",
            "install":{"linux":"curl -fsSL https://example.com/install.sh | sh -s -- --node --pair 'persona://link?machine=macbook'"}}"""))
        val code = api.add(" 小林的 MacBook ", ComputerOs.Mac)
        val (line, sent) = body(server)
        assertEquals("POST /api/v1/auth/pair-codes", line)
        assertEquals("""{"add":{"name":"小林的 MacBook","os":"mac"}}""", sent.toString())
        assertEquals("macbook", code.machine)
        assertEquals("小林的 MacBook", code.name)
        assertTrue(code.added)
        assertEquals("pending", code.trust)
        assertTrue(code.linuxInstall!!.startsWith("curl"))

        server.enqueue(MockResponse().setBody("""{"token":"jp_y","expires_at":"2026-11-06T02:20:00Z","machine":"macbook","trust":"pending","pair_url":"persona://link?machine=macbook"}"""))
        val again = api.again("macbook")
        assertEquals("""{"machine":"macbook"}""", body(server).second.toString())
        assertFalse(again.added)
        assertNull(again.linuxInstall)

        server.enqueue(MockResponse().setResponseCode(429).setBody("""{"code":"too_many_pending","error":"已经有 3 台电脑在等确认"}"""))
        val refused = runCatching { api.add("x", ComputerOs.Linux) }.exceptionOrNull() as HubFailure
        assertEquals("too_many_pending", refused.code)
        server.takeRequest()

        server.enqueue(MockResponse().setBody("""{"machines":[{"id":"macbook","name":"小林的 MacBook","online":true,"trust":"pending","error_code":"never_connected"}]}"""))
        val listed = api.machines().single()
        assertEquals("GET /api/v1/machines", server.takeRequest().let { "${it.method} ${it.path}" })
        assertTrue(listed.pending)
        assertEquals("never_connected", listed.errorCode)

        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        api.confirm("mac book")
        assertEquals("POST /api/v1/machines/mac%20book/confirm", server.takeRequest().let { "${it.method} ${it.path}" })
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        api.reject("macbook")
        assertEquals("POST /api/v1/machines/macbook/reject", server.takeRequest().let { "${it.method} ${it.path}" })
    }
}
