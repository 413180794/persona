package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.AssistantRefused
import persona.core.contracts.api.NewMainResult
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
            assertEquals("PUT", (put.method).zhAny())
            assertEquals("/api/v1/assistant/routines", (put.path).zhAny())
            assertEquals("Bearer test-token", (put.getHeader("Authorization")).zhAny())
            val routine = JsonCodec.parseToJsonElement(put.body.readUtf8()).jsonObject.getValue("routines").jsonArray.single().jsonObject
            assertEquals("09:00", (routine.getValue("time").jsonPrimitive.content).zhAny())
            assertTrue(routine.getValue("enabled").jsonPrimitive.boolean)
            assertEquals(JsonArray(emptyList()), routine["weekdays"])
            assertFalse(routine.containsKey("next_run"))
            assertNotNull(saved.single().nextRun)

            server.enqueue(MockResponse().setBody("{\"routines\":[]}"))
            api.setRoutines(emptyList())
            assertEquals("{\"routines\":[]}", (server.takeRequest().body.readUtf8()).zhAny())
            server.enqueue(MockResponse().setBody("{}"))
            api.endConversation(SessionKey("demo", "branch/one"))
            val close = server.takeRequest()
            assertEquals("POST", (close.method).zhAny())
            assertEquals("/api/v1/sessions/demo/branch%2Fone/close", (close.path).zhAny())

            server.enqueue(MockResponse().setBody("""{"machine_id":"demo","session_id":"started"}"""))
            assertEquals(SessionKey("demo", "started"), api.runRoutine("routine"))
            assertEquals("/api/v1/assistant/routines/routine/run", (server.takeRequest().path).zhAny())
            server.enqueue(MockResponse().setBody("{}"))
            api.setNotes("")
            assertEquals("{\"notes\":\"\"}", (server.takeRequest().body.readUtf8()).zhAny())
            server.enqueue(MockResponse().setBody("{}"))
            api.saveMemory("example.md", "")
            val memory = server.takeRequest()
            assertEquals("/api/v1/assistant/memory/example.md", (memory.path).zhAny())
            assertEquals("{\"content\":\"\"}", (memory.body.readUtf8()).zhAny())

            current = false
            try { api.warm(); fail("Stale pairings must not send requests") }
            catch (_: CancellationException) { }
            assertEquals(6, server.requestCount)
        }
    }

    @Test fun archivingPostsToTheConversationsArchiveRoute() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"ok":true,"archived":true}"""))
            api.archiveConversation(SessionKey("demo", "side-1"))
            val archive = server.takeRequest()
            assertEquals("POST", (archive.method).zhAny())
            assertEquals("/api/v1/assistant/conversations/side-1/archive", (archive.path).zhAny())
        }
    }

    @Test fun compactAndNewMainSendTheirBodiesAndTurnRefusalsIntoTheHubsWords() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"ok":true}"""))
            api.compactConversation(SessionKey("demo", "main-1"))
            val compact = server.takeRequest()
            assertEquals("POST", (compact.method).zhAny())
            assertEquals("/api/v1/assistant/conversations/main-1/compact", (compact.path).zhAny())
            assertEquals("""{"machine_id":"demo"}""", (compact.body.readUtf8()).zhAny())

            server.enqueue(MockResponse().setResponseCode(409).setBody("""{"code":"busy","error":"正在回答，等这一轮结束再压缩"}"""))
            try { api.compactConversation(SessionKey("demo", "main-1")); fail("A refusal must throw") }
            catch (refused: AssistantRefused) {
                assertEquals("busy", (refused.code).zhAny())
                assertEquals("正在回答，等这一轮结束再压缩", (refused.message).zhAny())
            }
            server.takeRequest()

            server.enqueue(MockResponse().setBody("""{"ok":true,"archived":"main-1","carried":true}"""))
            assertEquals(NewMainResult("main-1", true), api.newMainConversation(true, "main-1"))
            val created = server.takeRequest()
            assertEquals("/api/v1/assistant/main/new", (created.path).zhAny())
            assertEquals("""{"carry":"summary","expect":"main-1"}""", (created.body.readUtf8()).zhAny())

            server.enqueue(MockResponse().setBody("""{"ok":true,"archived":"","carried":false}"""))
            assertEquals(NewMainResult(), api.newMainConversation(false, null))
            assertEquals("""{"carry":"none"}""", (server.takeRequest().body.readUtf8()).zhAny())

            server.enqueue(MockResponse().setResponseCode(409).setBody("""{"code":"main_changed","error":"主对话已经换过了，刷新后再试"}"""))
            try { api.newMainConversation(true, "main-1"); fail("A refusal must throw") }
            catch (refused: AssistantRefused) { assertEquals("main_changed", (refused.code).zhAny()) }
            server.takeRequest()

            // Anything else stays the hub failure it is.
            server.enqueue(MockResponse().setResponseCode(500).setBody("""{"error":"boom"}"""))
            try { api.newMainConversation(true, "main-1"); fail("A failure must throw") }
            catch (failure: HubFailure) { assertEquals(500, failure.status) }
        }
    }
}
