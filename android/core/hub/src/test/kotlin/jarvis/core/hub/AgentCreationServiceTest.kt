package jarvis.core.hub

import jarvis.core.contracts.JsonCodec
import jarvis.core.contracts.api.*
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.SocketPolicy
import org.junit.Assert.*
import org.junit.Test
import java.util.Base64

class AgentCreationServiceTest {
    @Test fun lostStartResponseQueriesTheReceiptWithoutPostingAgain() = runBlocking {
        MockWebServer().use { server ->
            val service = HttpAgentCreationService("hub/pairing", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"launchers":[{"name":"legacy"},{"name":"wrapper","agent_type":"codex"}]}"""))
            val launchers = service.launchers("machine")
            assertEquals(listOf("claude", "codex"), launchers.map { it.agentType })
            server.takeRequest()
            val operation = "00000000-0000-4000-8000-000000000002"
            val saved = Base64.getEncoder().encodeToString("""{"session":{"machine_id":"machine","session_id":"created","agent":"codex"}}""".toByteArray())
            server.enqueue(MockResponse().setSocketPolicy(SocketPolicy.DISCONNECT_AFTER_REQUEST))
            server.enqueue(MockResponse().setBody("""{"kind":"start","state":"done","result":{"status":200,"body":"$saved"}}"""))
            val result = service.start(AgentStartRequest("machine", "/code", "wrapper", agentType = "codex"), operation)
            assertEquals("created", (result as AgentStartResult.Created).session.sessionId)
            val post = server.takeRequest()
            assertEquals("POST", post.method)
            assertEquals(operation, post.getHeader("X-Jarvis-Op-Id"))
            assertEquals("codex", JsonCodec.parseToJsonElement(post.body.readUtf8()).jsonObject["agent_type"]?.jsonPrimitive?.content)
            val lookup = server.takeRequest()
            assertEquals("GET", lookup.method)
            assertEquals("/api/v1/ops/$operation", lookup.path)
            assertEquals(3, server.requestCount)
        }
    }

    @Test fun directoryEncodingAndStartReceiptPreserveTheOperation() = runBlocking {
        MockWebServer().use { server ->
            val service = HttpAgentCreationService("hub/pairing", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"path":"/code/a & b","entries":[],"recent":[]}"""))
            assertEquals("/code/a & b", service.directories("machine/one", "/code/a & b").path)
            val dirs = server.takeRequest()
            assertEquals("/api/v1/machines/machine%2Fone/dirs", dirs.requestUrl?.encodedPath)
            assertEquals("/code/a & b", dirs.requestUrl?.queryParameter("path"))

            val operation = "00000000-0000-4000-8000-000000000001"
            server.enqueue(MockResponse().setResponseCode(202).setBody("""{"state":"pending"}"""))
            assertEquals(AgentStartResult.Pending, service.start(AgentStartRequest("machine", "/code", "claude", "首句", "plan", "high", resumeSessionId = "history"), operation))
            val start = server.takeRequest()
            assertEquals("/api/v1/sessions", start.path)
            assertEquals(operation, start.getHeader("X-Jarvis-Op-Id"))
            assertEquals("Bearer test-token", start.getHeader("Authorization"))
            val body = JsonCodec.parseToJsonElement(start.body.readUtf8()).jsonObject
            assertEquals("plan", body["permission_mode"]?.jsonPrimitive?.content)
            assertEquals("high", body["effort"]?.jsonPrimitive?.content)
            assertEquals("首句", body["prompt"]?.jsonPrimitive?.content)
            assertEquals("history", body["resume_session_id"]?.jsonPrimitive?.content)

            val saved = Base64.getEncoder().encodeToString("""{"session":{"machine_id":"machine","session_id":"session"}}""".toByteArray())
            server.enqueue(MockResponse().setBody("""{"state":"done","result":{"status":200,"body":"$saved"}}"""))
            val result = service.operation(operation) as AgentStartResult.Created
            assertEquals("session", result.session.sessionId)
            assertEquals("GET", server.takeRequest().method)
            server.enqueue(MockResponse().setResponseCode(404).setBody("{}"))
            assertEquals(AgentStartResult.Unknown, service.operation(operation))
            assertEquals("/api/v1/ops/$operation", server.takeRequest().path)
        }
    }
}
