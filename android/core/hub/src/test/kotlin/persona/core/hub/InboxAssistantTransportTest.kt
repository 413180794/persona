package persona.core.hub

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import persona.core.contracts.JsonCodec
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class InboxAssistantTransportTest {
    @Test fun documentReadsUseTheReadonlyProxyAndMediaRemainsPairingBound() = runBlocking {
        MockWebServer().use { server ->
            var current = true
            val transport = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient()) { current }
            server.enqueue(MockResponse().setBody("{\"title\":\"文档\"}"))
            transport.readDocument("https://example.com/docx/token?from=message")
            val read = server.takeRequest()
            assertEquals("GET", read.method)
            assertEquals("/api/v1/lark/raw/documents/read", read.requestUrl?.encodedPath)
            assertEquals("https://example.com/docx/token?from=message", read.requestUrl?.queryParameter("url"))
            assertEquals("json", read.requestUrl?.queryParameter("format"))
            assertEquals("Bearer test-token", read.getHeader("Authorization"))
            server.enqueue(MockResponse().setBody("image"))
            assertArrayEquals("image".toByteArray(), transport.documentMedia("/api/v1/lark/raw/documents/images/cover"))
            server.takeRequest()
            for (ref in listOf("https://example.com/api/v1/lark/raw/documents/images/cover", "/api/v1/lark/raw/contacts")) {
                try { transport.documentMedia(ref); fail("Invalid document media must be rejected") }
                catch (_: IllegalArgumentException) { }
            }
            current = false
            try { transport.readDocument("https://example.com/docx/token"); fail("Stale pairing must be rejected") }
            catch (_: kotlinx.coroutines.CancellationException) { }
            assertEquals(2, server.requestCount)
        }
    }

    @Test fun repliesPreserveClientIdAndMediaNeverForwardsCredentialsOffOrigin() = runBlocking {
        MockWebServer().use { server ->
            val transport = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("{}"))
            transport.reply("item/with#separator", null, "回复", "reply-id")
            val reply = server.takeRequest()
            assertEquals("/api/v1/inbox/item%2Fwith%23separator/reply", reply.path)
            assertEquals("reply-id", JsonCodec.parseToJsonElement(reply.body.readUtf8()).jsonObject["client_id"]?.jsonPrimitive?.content)
            assertEquals("Bearer test-token", reply.getHeader("Authorization"))

            server.enqueue(MockResponse().setBody("image"))
            assertArrayEquals("image".toByteArray(), transport.media("/api/v1/lark/media/image?size=120"))
            val media = server.takeRequest()
            assertEquals("/api/v1/lark/media/image?size=120", media.path)
            assertEquals("Bearer test-token", media.getHeader("Authorization"))
            try {
                transport.media("https://example.com/api/v1/lark/media/image")
                fail("Foreign media origin must be rejected")
            } catch (_: IllegalArgumentException) { }
            server.enqueue(MockResponse().setResponseCode(302).setHeader("Location", "https://example.com/image"))
            try {
                transport.media("/api/v1/lark/media/redirect")
                fail("Media redirects must be rejected")
            } catch (_: HubFailure) { }
            server.takeRequest()
            assertEquals(3, server.requestCount)
        }
    }
}
