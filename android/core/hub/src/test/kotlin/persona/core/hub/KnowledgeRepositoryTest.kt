package persona.core.hub

import persona.core.contracts.HubScope
import persona.core.contracts.api.KnowledgeKind
import persona.core.contracts.api.KnowledgeUpdate
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class KnowledgeRepositoryTest {
    @Test fun readsExistingWireFormatEncodesParametersAndRejectsRetiredPairingsAndRedirects() = runBlocking {
        MockWebServer().use { server ->
            var current = true
            val repository = HttpKnowledgeRepository(HubScope("demo", "generation"), server.url("/").toString(),
                "fixture-token", OkHttpClient(), { current })
            server.enqueue(MockResponse().setBody("""{"items":[{"kind":"projects","id":"sample","name":"示例项目","relevance":80,"tags":null}],"tags":null,"future":true}"""))
            assertEquals("示例项目", repository.list(KnowledgeKind.Projects, "A & B").items.single().name)
            val list = server.takeRequest()
            assertEquals("A & B", list.requestUrl!!.queryParameter("tag"))
            assertEquals("Bearer fixture-token", list.getHeader("Authorization"))
            server.enqueue(MockResponse().setBody("""{"detail":{"kind":"people","id":"sample/#id","path":"data/people/sample","summary":"# 档案","log":null,"links":null},"agents":null}"""))
            assertEquals("# 档案", repository.detail(KnowledgeKind.People, "sample/#id").summary)
            assertEquals("/api/v1/kb/people/sample%2F%23id", server.takeRequest().path)
            server.enqueue(MockResponse().setBody("""{"hits":[{"path":"projects/sample/summary.md","line":"进展"}]}"""))
            assertEquals(1, repository.search("进展 & 状态").size)
            val search = server.takeRequest()
            assertEquals("进展 & 状态", search.requestUrl!!.queryParameter("q"))
            assertEquals("/api/v1/kb-search", search.requestUrl!!.encodedPath)
            server.enqueue(MockResponse().setResponseCode(302).setHeader("Location", "https://example.com/redirect"))
            try { repository.list(KnowledgeKind.Chats); fail("Redirect must not receive pairing credentials") }
            catch (_: HubFailure) { }
            server.takeRequest()
            current = false
            try { repository.search("query"); fail("Retired pairing must not issue requests") }
            catch (_: CancellationException) { }
            assertEquals(4, server.requestCount)
        }
    }

    @Test fun entryKeepsLiveAgentsAndTaggingSendsOnlyWhatIsSet() = runBlocking {
        MockWebServer().use { server ->
            val repository = HttpKnowledgeRepository(HubScope("demo", "generation"), server.url("/").toString(),
                "fixture-token", OkHttpClient(), { true })
            server.enqueue(MockResponse().setBody("""{"detail":{"kind":"projects","id":"weather","path":"data/projects/weather",
                "meta":{"dirs":["laptop:/code/weather"]}},"agents":[
                {"machine_id":"laptop","session_id":"s1","title":"小组件","cwd":"/code/weather","status":"working"},
                {"machine_id":"laptop","session_id":"s2","cwd":"/code/weather","status":"offline"},
                {"machine_id":"server","session_id":"jarvis","assistant":true,"status":"idle"}]}"""))
            val entry = repository.entry(KnowledgeKind.Projects, "weather")
            assertEquals(listOf("s1"), entry.agents.orEmpty().map { it.sessionId })
            assertEquals("/api/v1/kb/projects/weather", server.takeRequest().path)
            server.enqueue(MockResponse().setBody("""{"detail":{"kind":"projects","id":"other","path":"x"}}"""))
            assertTrue(runCatching { repository.entry(KnowledgeKind.Projects, "weather") }.exceptionOrNull() is HubFailure)
            server.takeRequest()

            server.enqueue(MockResponse().setBody("""{"id":"weather"}"""))
            repository.update(KnowledgeKind.Projects, "weather", KnowledgeUpdate("P1", 55, emptyList(), "paused", "long"))
            val put = server.takeRequest()
            assertEquals("PUT", put.method)
            assertEquals("/api/v1/kb/projects/weather", put.path)
            assertEquals("Bearer fixture-token", put.getHeader("Authorization"))
            assertEquals("""{"importance":"P1","relevance":55,"tags":[],"status":"paused","project_kind":"long"}""", put.body.readUtf8())
            server.enqueue(MockResponse().setBody("{}"))
            repository.update(KnowledgeKind.People, "p", KnowledgeUpdate("P0", 0, listOf("a")))
            assertEquals("""{"importance":"P0","relevance":0,"tags":["a"]}""", server.takeRequest().body.readUtf8())
            server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"not found"}"""))
            assertTrue(runCatching { repository.update(KnowledgeKind.People, "gone", KnowledgeUpdate(tags = emptyList())) }.exceptionOrNull() is HubFailure)
        }
    }
}
