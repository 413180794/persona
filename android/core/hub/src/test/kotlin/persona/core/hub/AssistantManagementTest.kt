package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.Routine
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class AssistantManagementTest {
    @Test fun migratedActionsPreserveDefaultsPathsAndPairingScope() = runBlocking {
        MockWebServer().use { server ->
            var current = true
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient()) { current }
            server.enqueue(MockResponse().setBody("""{"routines":[{"id":"routine","time":"09:00","enabled":true,"next_run":"2026-11-02T09:00:00+08:00"}]}"""))
            val saved = api.setRoutines(listOf(Routine(id = "routine", title = "早报", prompt = "整理安排", nextRun = "server-owned")))
            val put = server.takeRequest()
            assertEquals("PUT", put.method)
            assertEquals("/api/v1/assistant/routines", put.path)
            assertEquals("Bearer test-token", put.getHeader("Authorization"))
            val routine = JsonCodec.parseToJsonElement(put.body.readUtf8()).jsonObject.getValue("routines").jsonArray.single().jsonObject
            assertEquals("09:00", routine.getValue("time").jsonPrimitive.content)
            assertTrue(routine.getValue("enabled").jsonPrimitive.boolean)
            assertEquals(JsonArray(emptyList()), routine["weekdays"])
            assertFalse(routine.containsKey("next_run"))
            assertNotNull(saved.single().nextRun)

            server.enqueue(MockResponse().setBody("{\"routines\":[]}"))
            api.setRoutines(emptyList())
            assertEquals("{\"routines\":[]}", server.takeRequest().body.readUtf8())
            server.enqueue(MockResponse().setBody("{}"))
            api.endConversation(SessionKey("demo", "branch/one"))
            val close = server.takeRequest()
            assertEquals("POST", close.method)
            assertEquals("/api/v1/sessions/demo/branch%2Fone/close", close.path)

            server.enqueue(MockResponse().setBody("""{"machine_id":"demo","session_id":"started"}"""))
            assertEquals(SessionKey("demo", "started"), api.runRoutine("routine"))
            assertEquals("/api/v1/assistant/routines/routine/run", server.takeRequest().path)
            server.enqueue(MockResponse().setBody("{}"))
            api.setNotes("")
            assertEquals("{\"notes\":\"\"}", server.takeRequest().body.readUtf8())
            server.enqueue(MockResponse().setBody("{}"))
            api.saveMemory("example.md", "")
            val memory = server.takeRequest()
            assertEquals("/api/v1/assistant/memory/example.md", memory.path)
            assertEquals("{\"content\":\"\"}", memory.body.readUtf8())

            current = false
            try { api.warm(); fail("Stale pairings must not send requests") }
            catch (_: CancellationException) { }
            assertEquals(6, server.requestCount)
        }
    }
}
