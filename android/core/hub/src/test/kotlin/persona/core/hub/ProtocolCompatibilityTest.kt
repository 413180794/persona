package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.ResponseBody.Companion.toResponseBody
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.Session
import okio.Buffer

class ProtocolCompatibilityTest {
    @Test fun baselineHttpAcceptsTheNewClientsIdentityAndOperationHeaders() = runBlocking {
        MockWebServer().use { server ->
            server.enqueue(MockResponse().setBody("""{"session_id":"test-session","started_by_jarvis":true}"""))
            HubHttpClient(null, null, "1").use { hub ->
                val id = "00000000-0000-4000-8000-000000000001"
                val request = Request.Builder().url(server.url("/api/v1/sessions/test-machine/test-session/messages"))
                    .header("X-Persona-Op-Id", id)
                    .post(compatibleRequestBody("/api/v1/sessions/test-machine/test-session/messages",
                        """{"text":"<persona-context kind=\"workspace\" title=\"Project\" url=\"\" machine=\"test-machine\" cwd=\"/workspace\">Notes</persona-context>","client_msg_id":"$id"}""".toRequestBody("application/json".toMediaType()))!!).build()
                val response = hub.client.newCall(request).awaitResponse().use { JsonCodec.decodeFromString<Session>(it.body!!.string()) }
                assertTrue(response.startedByPersona)
                val sent = server.takeRequest()
                assertEquals("android/1", sent.getHeader("X-Jarvis-Client"))
                assertEquals(sent.getHeader("X-Jarvis-Client"), sent.getHeader("X-Persona-Client"))
                assertEquals(id, sent.getHeader("X-Jarvis-Op-Id"))
                assertTrue(sent.body.readUtf8().contains("<jarvis-context"))
            }
        }
    }

    @Test fun generatedMarkupChangesOnlyAtMessageEndpoints() {
        val json = """{"text":"<persona-file kind=\"video\" path=\"/workspace/demo.mp4\"/>","input":{"text":"<persona-context>opaque</persona-context>"}}"""
        val body = json.toRequestBody("application/json".toMediaType())
        assertSame(body, compatibleRequestBody("/api/v1/extensions/test/action", body))
        val encoded = Buffer().also(compatibleRequestBody("/api/v1/sessions/test-machine/test-session/messages", body)!!::writeTo).readUtf8()
        assertTrue(encoded.contains("<jarvis-file"))
        assertTrue(encoded.contains("<persona-context>opaque</persona-context>"))
    }

    @Test fun theRealComposerSubmissionUsesBaselineMarkupNotJustTheProbe() = runBlocking {
        val scope = persona.core.contracts.HubScope("test-hub", "generation")
        val transport = object : persona.core.contracts.api.MessageTransport {
            override val readyToSend = kotlinx.coroutines.flow.MutableStateFlow(true)
            override val supportsClientOps = true
            override suspend fun send(request: persona.core.contracts.api.MessageRequest): persona.core.contracts.api.MessageTransportResult = error("The composer owns submission")
        }
        var sent = ""
        val composer = HttpChatComposerService(scope, transport, request = { _, body, headers ->
            sent = Buffer().also(body::writeTo).readUtf8()
            assertEquals(headers["X-Persona-Op-Id"], headers["X-Jarvis-Op-Id"])
            okhttp3.Response.Builder().request(Request.Builder().url("http://localhost/").build())
                .protocol(okhttp3.Protocol.HTTP_1_1).code(200).message("OK")
                .body("{}".toResponseBody()).build()
        })
        composer.send(persona.core.contracts.api.MessageRequest(scope, persona.core.contracts.SessionKey("test-machine", "test-session"),
            "<persona-context kind=\"workspace\" title=\"Project\" url=\"\" machine=\"test-machine\" cwd=\"/workspace\">Notes</persona-context>",
            "00000000-0000-4000-8000-000000000001"))
        assertTrue(sent.contains("<jarvis-context"))
        assertFalse(sent.contains("<persona-context"))
    }
}
