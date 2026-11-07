package persona.core.hub

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test

class LarkThreadTransportTest {
    @Test fun replyPreservesClientIdAndPendingReceiptAndRejectsExpiredPairing() = runBlocking {
        MockWebServer().use { server ->
            var current = true
            val base = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient()) { current }
            val transport = HttpLarkConversationTransport(base, server.url("/").toString(), "test-token", OkHttpClient()) { current }
            server.enqueue(MockResponse().setResponseCode(202).setBody("""{"state":"pending"}"""))
            val pending = transport.replyInThread("root/with#separator", "回复", "client-1")
            assertEquals("pending", pending["state"]?.toString()?.trim('"'))
            server.enqueue(MockResponse().setBody("""{"ok":true}"""))
            transport.replyInThread("root/with#separator", "回复", "client-1")
            val first = server.takeRequest()
            val replay = server.takeRequest()
            assertEquals("/api/v1/lark/messages/root%2Fwith%23separator/replies", (first.requestUrl!!.encodedPath).zhAny())
            assertEquals("POST", (first.method).zhAny())
            assertEquals("Bearer test-token", (first.getHeader("Authorization")).zhAny())
            assertEquals("""{"text":"回复","thread":true,"client_id":"client-1"}""", (first.body.readUtf8()).zhAny())
            assertEquals("""{"text":"回复","thread":true,"client_id":"client-1"}""", (replay.body.readUtf8()).zhAny())
            current = false
            try {
                transport.replyInThread("root", "回复", "client-2")
                fail("Expired pairing must not send")
            } catch (_: CancellationException) { }
            assertEquals(2, server.requestCount)
        }
    }

    @Test fun threadUsesEncodedMessageAndCursorAndRejectsExpiredPairing() = runBlocking {
        MockWebServer().use { server ->
            var current = true
            val base = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient()) { current }
            val transport = HttpLarkConversationTransport(base, server.url("/").toString(), "test-token", OkHttpClient()) { current }
            server.enqueue(MockResponse().setBody("""{"messages":[],"has_older":false}"""))
            transport.thread("root/with#separator", "cursor/+ value")
            val request = server.takeRequest()
            assertEquals("/api/v1/lark/messages/root%2Fwith%23separator/thread", (request.requestUrl!!.encodedPath).zhAny())
            assertEquals("cursor/+ value", (request.requestUrl!!.queryParameter("before")).zhAny())
            assertEquals("60", (request.requestUrl!!.queryParameter("limit")).zhAny())
            assertNull(request.requestUrl!!.queryParameter("after"))
            assertEquals("Bearer test-token", (request.getHeader("Authorization")).zhAny())
            current = false
            try {
                transport.thread("root")
                fail("Expired pairing must not request thread history")
            } catch (_: CancellationException) { }
            assertEquals(1, server.requestCount)
        }
    }
}
