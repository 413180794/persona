package persona.core.hub

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class InboxActionsTransportTest {
    @Test fun draftPostsTheInstructionAndReturnsTheText() = runBlocking {
        MockWebServer().use { server ->
            var current = true
            val transport = HttpInboxActionsTransport(OkHttpClient(), { server.url("/").toString().toHttpUrl() }, "fixture-token", { current })
            server.enqueue(MockResponse().setBody("""{"text":"  能赶上，周五前给你。\n","future":1}"""))
            assertEquals("能赶上，周五前给你。", (transport.draft("lark:f1/10#x", "语气轻松一点\n在这版上改：能赶上")).zhAny())
            val request = server.takeRequest()
            assertEquals("POST", (request.method).zhAny())
            assertEquals("/api/v1/inbox/lark:f1%2F10%23x/draft", (request.path).zhAny())
            assertEquals("Bearer fixture-token", (request.getHeader("Authorization")).zhAny())
            assertEquals("""{"instruction":"语气轻松一点\n在这版上改：能赶上"}""", (request.body.readUtf8()).zhAny())

            // An empty instruction still goes on the wire: the hub drafts from the item alone.
            server.enqueue(MockResponse().setBody("""{"text":"好的"}"""))
            transport.draft("mail:a1:t:7", "")
            assertEquals("""{"instruction":""}""", (server.takeRequest().body.readUtf8()).zhAny())

            server.enqueue(MockResponse().setResponseCode(502).setBody("""{"error":"model unavailable"}"""))
            val failure = runCatching { transport.draft("x", "") }.exceptionOrNull() as HubFailure
            assertEquals("model unavailable", (failure.message).zhAny())
            server.takeRequest()

            server.enqueue(MockResponse().setBody("""{"text":"   "}"""))
            assertEquals(HubError.Protocol, (runCatching { transport.draft("x", "") }.exceptionOrNull() as HubFailure).kind)
            server.takeRequest()

            current = false
            assertTrue(runCatching { transport.draft("x", "") }.exceptionOrNull() is CancellationException)
            assertEquals("Unexpected request: ${if (server.requestCount > 4) server.takeRequest(200, java.util.concurrent.TimeUnit.MILLISECONDS) else null}",
                4, server.requestCount)
        }
    }
}
