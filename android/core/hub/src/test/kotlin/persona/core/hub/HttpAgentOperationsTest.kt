package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpAgentOperationsTest {
    @Test fun pathsBodiesAndFailuresMatchHubContractWithoutRetryingWrites() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAgentOperations("scope", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("{}"))
            api.renameSession(SessionKey("machine/id", "session#id"), "新名称", null)
            val rename = server.takeRequest()
            assertEquals("POST", rename.method)
            assertEquals("/api/v1/sessions/machine%2Fid/session%23id/rename", rename.path)
            assertEquals("Bearer test-token", rename.getHeader("Authorization"))
            assertEquals(buildJsonObject { put("title", "新名称") }, JsonCodec.parseToJsonElement(rename.body.readUtf8()))
            server.enqueue(MockResponse().setBody("{}"))
            api.renameSpace("machine", "workspace/id", "新空间")
            assertEquals("/api/v1/machines/machine/spaces/workspace%2Fid/rename", server.takeRequest().path)
            server.enqueue(MockResponse().setBody("{}"))
            api.saveMutes(AgentOperationPreferences(spaces = listOf("machine/新空间"), allowed = setOf("machine/session")))
            val mute = server.takeRequest()
            assertEquals("PUT", mute.method)
            assertEquals("/api/v1/mutes", mute.path)
            assertEquals("machine/session", JsonCodec.parseToJsonElement(mute.body.readUtf8()).jsonObject.getValue("allowed").jsonArray.single().jsonPrimitive.content)
            server.enqueue(MockResponse().setResponseCode(400).setBody("{}"))
            try { api.closeSession(SessionKey("machine", "session")); fail("Close must fail") }
            catch (e: AgentOperationFailure) { assertFalse(e.uncertain) }
            assertEquals("/api/v1/sessions/machine/session/close", server.takeRequest().path)
            server.enqueue(MockResponse().setResponseCode(503).setBody("{}"))
            try { api.closeSession(SessionKey("machine", "session")); fail("Close must be uncertain") }
            catch (e: AgentOperationFailure) { assertTrue(e.uncertain) }
            server.takeRequest()
            assertEquals(5, server.requestCount)
        }
    }
}
