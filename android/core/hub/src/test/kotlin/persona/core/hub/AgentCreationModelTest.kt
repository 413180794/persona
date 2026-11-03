package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

/** New agents follow the 新建 Agent model configuration and start in the background (docs/protocol.md「新建会话」). */
class AgentCreationModelTest {
    @Test fun anAsyncStartWithoutLauncherFollowsTheConfigurationAndCarriesTheNameHint() = runBlocking {
        MockWebServer().use { server ->
            val service = HttpAgentCreationService("hub/pairing", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"session":{"machine_id":"mac","session_id":"new","status":"idle","starting":true}}"""))
            val result = service.start(AgentStartRequest("mac", "/code", "", async = true, nameHint = "看下首页"),
                "00000000-0000-4000-8000-000000000003")
            val created = (result as AgentStartResult.Created).session
            assertTrue(created.starting)
            val body = JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
            assertEquals("", body.getValue("launcher").jsonPrimitive.content)
            assertEquals(true, body.getValue("async").jsonPrimitive.boolean)
            assertEquals("看下首页", body.getValue("name_hint").jsonPrimitive.content)
            assertFalse(body.containsKey("prompt"))
        }
    }

    @Test fun theNewAgentPurposeIsReadAndOldHubsHaveNone() = runBlocking {
        MockWebServer().use { server ->
            val service = HttpAgentCreationService("hub/pairing", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"purpose":{"id":"agent.new","name":"新建 Agent","kind":"launcher",
                "effective":{"provider":"launcher.claude","model":"model-chat"},"revision":1},
                "providers":[{"id":"launcher.claude","name":"claude"}]}"""))
            val detail = requireNotNull(service.newAgentModel())
            assertEquals(listOf("claude", "model-chat"), detail.purpose.summaryParts(detail.providers))
            val request = server.takeRequest()
            assertEquals("/api/v1/model-config/purposes/agent.new", request.path)
            assertEquals("Bearer test-token", request.getHeader("Authorization"))
            server.enqueue(MockResponse().setResponseCode(404).setBody("404 page not found"))
            assertNull(service.newAgentModel())
            server.enqueue(MockResponse().setResponseCode(502).setBody("""{"error":"bad gateway"}"""))
            assertEquals(502, (runCatching { service.newAgentModel() }.exceptionOrNull() as HubFailure).status)
        }
    }
}
