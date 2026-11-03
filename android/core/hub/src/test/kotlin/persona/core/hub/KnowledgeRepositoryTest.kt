package persona.core.hub

import persona.core.contracts.HubScope
import persona.core.contracts.api.KnowledgeKind
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
}
