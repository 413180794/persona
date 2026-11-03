package persona.core.hub

import persona.core.contracts.api.AssistantConfigUpdate
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

/** 设置 › 助手: where the assistant runs, read and written through GET / PUT /api/v1/assistant. */
class AssistantConfigTest {
    @Test fun readsWhereTheAssistantRunsAndIgnoresTheConversations() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"machine_id":"laptop","launcher":"claude","cwd":"/home/me/.jarvis/assistant",
                "timezone":"Asia/Shanghai","notes_effective":"from hub.json","main":"main-id",
                "conversations":[{"machine_id":"laptop","session_id":"main-id","assistant":true}]}"""))
            val config = api.config()
            val get = server.takeRequest()
            assertEquals("GET", get.method)
            assertEquals("/api/v1/assistant", get.path)
            assertEquals("Bearer test-token", get.getHeader("Authorization"))
            assertEquals("laptop", config.machineId)
            assertEquals("claude", config.launcher)
            assertEquals("/home/me/.jarvis/assistant", config.cwd)
            assertEquals("Asia/Shanghai", config.timezone)
            assertNull("never edited in the app", config.notes)
            assertEquals("from hub.json", config.notesEffective)
            assertEquals("main-id", config.main)
        }
    }

    @Test fun writesOnlyWhatChanges() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient())
            val answer = """{"machine_id":"server","launcher":"claude","cwd":"","notes":"","notes_effective":""}"""
            repeat(4) { server.enqueue(MockResponse().setBody(answer)) }

            // Moving machines also drops the main conversation: it stays on the machine it ran on.
            val moved = api.updateConfig(AssistantConfigUpdate(machineId = "server", main = AssistantConfigUpdate.CLEAR_MAIN))
            val move = server.takeRequest()
            assertEquals("PUT", move.method)
            assertEquals("/api/v1/assistant", move.path)
            assertEquals("""{"machine_id":"server","main":"-"}""", move.body.readUtf8())
            assertEquals("server", moved.machineId)
            assertEquals("", moved.notes)

            api.updateConfig(AssistantConfigUpdate(launcher = "example_launcher"))
            assertEquals("""{"launcher":"example_launcher"}""", server.takeRequest().body.readUtf8())
            api.updateConfig(AssistantConfigUpdate(timezone = "Europe/Paris"))
            assertEquals("""{"timezone":"Europe/Paris"}""", server.takeRequest().body.readUtf8())
            // An empty 关于我 is a choice (tell the assistant nothing), not a missing field.
            api.updateConfig(AssistantConfigUpdate(notes = ""))
            assertEquals("""{"notes":""}""", server.takeRequest().body.readUtf8())
        }
    }

    @Test fun listsTheLaunchersOfTheChosenMachine() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"launchers":[{"name":"claude","model":"opus"},
                {"name":"example_wrapper","agent_type":"codex"}]}"""))
            val launchers = api.launchers("laptop")
            assertEquals("/api/v1/machines/laptop/launchers", server.takeRequest().path)
            assertEquals(listOf("claude", "example_wrapper"), launchers.map { it.name })
            assertEquals("claude", launchers.first().agentType)
            assertEquals("codex", launchers.last().agentType)

            server.enqueue(MockResponse().setResponseCode(503).setBody("""{"error":"laptop: machine is offline"}"""))
            val failure = runCatching { api.launchers("laptop") }.exceptionOrNull()
            assertTrue(failure is HubFailure)
            assertEquals(503, (failure as HubFailure).status)
        }
    }
}
