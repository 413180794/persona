package persona.core.hub

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.ExtensionAuthEvent
import persona.core.contracts.ServerEvent
import persona.core.contracts.api.ExtensionOperation
import persona.core.contracts.api.HubExtension
import persona.core.contracts.JsonCodec

/** An extension's sign-in (#4507): the three routes, what they send, and the pushed event; the install's tools step. */
class HttpExtensionAuthTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpExtensionManagement) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpExtensionManagement({ path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            }))
        }
    }

    @Test fun theRoutesAndWhatTheySend() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"title":"飞书登录","page":"login","state":"signed_out","checked_at":"2026-11-06T08:00:00Z"}"""))
        val auth = api.auth("example-cis")
        val first = server.takeRequest()
        assertEquals("GET /api/v1/extensions/example-cis/-/auth", "${first.method} ${first.path}")
        assertEquals("login", auth.page)
        assertTrue(auth.needsSignIn)

        server.enqueue(MockResponse().setBody("""{"step":"wait","kind":"device_code","url":"https://example.com/device","user_code":"ABCD-1234",
            "interval":3,"qr":{"size":2,"rows":["01","10"]},"expires_at":"2026-11-06T08:10:00Z"}"""))
        val prompt = api.authStep("example-cis", "", mapOf("phone" to "123"))
        val step = server.takeRequest()
        assertEquals("POST /api/v1/extensions/example-cis/-/auth/step", "${step.method} ${step.path}")
        assertEquals(buildJsonObject { put("step", ""); put("values", buildJsonObject { put("phone", "123") }) },
            Json.parseToJsonElement(step.body.readUtf8()))
        assertEquals("device_code", prompt.kind)
        assertEquals(listOf("01", "10"), prompt.qr?.rows)
        assertTrue(prompt.polled)

        server.enqueue(MockResponse().setBody("""{"title":"飞书登录","state":"signed_out"}"""))
        api.authLogout("example-cis")
        val out = server.takeRequest()
        assertEquals("POST /api/v1/extensions/example-cis/-/auth/logout", "${out.method} ${out.path}")
    }

    @Test fun theEventTheListAndTheToolsStep() {
        val event = JsonCodec.decodeFromString(ServerEvent.serializer(),
            """{"type":"extension_auth","extension":"example-cis","auth":{"title":"飞书登录","state":"signed_in","account":"me@example.com"}}""")
        assertEquals("example-cis", (event as ExtensionAuthEvent).extension)
        assertEquals("me@example.com", event.auth.account)

        val ext = JsonCodec.decodeFromString(HubExtension.serializer(), """{"id":"example-cis","name":"CIS","status":"running",
            "auth":{"title":"飞书登录","state":"expired"},"tools":[{"name":"node","title":"Node.js","version":"22.23.4","reused":true},
            {"name":"example-cli","version":"0.59.0"}]}""")
        assertEquals("expired", ext.auth?.state)
        assertEquals(listOf(true, false), ext.tools?.map { it.reused })

        val op = JsonCodec.decodeFromString(ExtensionOperation.serializer(), """{"id":"op1","kind":"install","extension":"example-cis",
            "state":"succeeded","next":"auth","steps":[{"id":"tools","state":"running","bytes":10,"total":100,
            "tools":[{"name":"node","state":"done","reused":true},{"name":"example-cli","state":"running"}]}]}""")
        assertEquals("auth", op.next)
        assertEquals(listOf("node" to true, "example-cli" to false), op.steps[0].tools?.map { it.name to it.reused })
    }
}
