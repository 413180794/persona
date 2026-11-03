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

    @Test fun aLaunchersModelsAreReadAskedAgainAndAbsentOnOldHubs() = runBlocking {
        MockWebServer().use { server ->
            val service = HttpAgentCreationService("hub/pairing", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"agent":"claude","models":[
                {"id":"default","name":"Default (recommended)","description":"Use the default model","resolved":"claude-opus-5-5",
                 "default":true,"efforts":["low","medium","high","xhigh","max"]},
                {"id":"haiku","name":"Haiku 5.5","resolved":"claude-haiku-5-5"},
                {"id":"gateway/glm-5.3","name":"glm-5.3","description":"From gateway","efforts":["low","medium","high"],"future":1}]}"""))
            val list = requireNotNull(service.launcherModels("machine/one", "my launcher"))
            assertEquals("claude", list.agent)
            assertNull(list.locked)
            assertEquals(listOf("default", "haiku", "gateway/glm-5.3"), list.models.map { it.id })
            assertEquals(listOf(true, false, false), list.models.map { it.isDefault })
            assertEquals("claude-opus-5-5", list.models[0].resolved)
            assertEquals(emptyList<String>(), list.models[1].efforts)
            assertEquals(listOf("low", "medium", "high"), list.models[2].efforts)
            val first = server.takeRequest()
            assertEquals("/api/v1/machines/machine%2Fone/launchers/my%20launcher/models", first.requestUrl?.encodedPath)
            assertNull(first.requestUrl?.queryParameter("refresh"))
            assertEquals("Bearer test-token", first.getHeader("Authorization"))

            server.enqueue(MockResponse().setBody("""{"agent":"codex","locked":"启动器固定了模型参数，不能覆盖具体模型","models":[
                {"id":"gpt-5.5","name":"GPT-5.5","default":true,"efforts":["low","medium"],"default_effort":"medium"}]}"""))
            val codex = requireNotNull(service.launcherModels("mac", "codex", refresh = true))
            assertEquals("启动器固定了模型参数，不能覆盖具体模型", codex.locked)
            assertEquals("medium", codex.models.single().defaultEffort)
            assertEquals("1", server.takeRequest().requestUrl?.queryParameter("refresh"))

            // An agent without a list, a hub without the endpoint, and a CLI that did not answer.
            server.enqueue(MockResponse().setBody("""{"agent":"traex","models":[]}"""))
            assertEquals(LauncherModels("traex"), service.launcherModels("mac", "traex"))
            server.enqueue(MockResponse().setResponseCode(404).setBody("404 page not found"))
            assertNull(service.launcherModels("mac", "claude"))
            server.enqueue(MockResponse().setResponseCode(502).setBody("""{"error":"claude: command not found"}"""))
            val failure = runCatching { service.launcherModels("mac", "claude") }.exceptionOrNull() as HubFailure
            assertEquals(502, failure.status)
            assertEquals("claude: command not found", failure.message)
        }
    }

    @Test fun aPickedModelGoesWithTheStartAndNoneIsLeftOut() = runBlocking {
        MockWebServer().use { server ->
            val service = HttpAgentCreationService("hub/pairing", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"session":{"machine_id":"mac","session_id":"a"}}"""))
            service.start(AgentStartRequest("mac", "/code", "claude", model = "gateway/glm-5.3"), "00000000-0000-4000-8000-000000000004")
            val picked = JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
            assertEquals("gateway/glm-5.3", picked.getValue("model").jsonPrimitive.content)
            server.enqueue(MockResponse().setBody("""{"session":{"machine_id":"mac","session_id":"b"}}"""))
            service.start(AgentStartRequest("mac", "/code", "claude"), "00000000-0000-4000-8000-000000000005")
            assertFalse(JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject.containsKey("model"))
        }
    }
}
