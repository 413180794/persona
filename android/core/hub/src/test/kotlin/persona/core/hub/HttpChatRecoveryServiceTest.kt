package persona.core.hub

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpChatRecoveryServiceTest {
    @Test fun resumeUsesExistingContractAndLostReceiptOnlyReadsBeforeReconnectTargetsMachine() = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            val service = HttpChatRecoveryService { path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            }
            val session = Session(machineId = "machine/one", sessionId = "session", cwd = "/workspace", launcher = "default", status = "offline")
            server.enqueue(MockResponse().setResponseCode(502).setBody("""{"delivered":"unknown"}"""))
            assertEquals(ChatRecoveryResult.Unknown, service.resume(session))
            val start = server.takeRequest()
            assertEquals("POST", (start.method).zhAny())
            assertEquals("/api/v1/sessions", (start.path).zhAny())
            val body = JsonCodec.parseToJsonElement(start.body.readUtf8()).jsonObject
            assertEquals(JsonPrimitive(session.sessionId), body["resume_session_id"])
            assertEquals(JsonPrimitive(session.machineId), body["machine_id"])
            assertEquals(JsonPrimitive(session.cwd), body["cwd"])
            assertEquals(JsonPrimitive(session.launcher), body["launcher"])
            assertFalse(body.containsKey("prompt"))
            server.enqueue(MockResponse().setBody("""{"sessions":[{"machine_id":"machine/one","session_id":"session","status":"unknown"}]}"""))
            assertEquals(ChatRecoveryResult.Unknown, service.resume(session))
            assertEquals("GET", (server.takeRequest().method).zhAny())
            server.enqueue(MockResponse().setBody("""{"sessions":[{"machine_id":"machine/one","session_id":"session","status":"idle"}]}"""))
            assertEquals(ChatRecoveryResult.Restored(session.key), service.resume(session))
            val verify = server.takeRequest()
            assertEquals("GET", (verify.method).zhAny())
            assertEquals("/api/v1/sessions", (verify.path).zhAny())
            server.enqueue(MockResponse().setBody("{}"))
            assertEquals(ChatActionResult.Done, service.reconnect(session.machineId))
            val reconnect = server.takeRequest()
            assertEquals("POST", (reconnect.method).zhAny())
            assertEquals("/api/v1/machines/machine%2Fone/reconnect", (reconnect.path).zhAny())
            assertEquals(4, server.requestCount)
        }
    }
}
