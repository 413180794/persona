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
    @Test fun bulkPostsTheActionAndAnOldHubGetsDoneTheOldWay() = runBlocking {
        MockWebServer().use { server ->
            val transport = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient())
            val targets = listOf(persona.core.contracts.api.InboxDoneTarget("a", "va"), persona.core.contracts.api.InboxDoneTarget("b"))
            server.enqueue(MockResponse().setBody("{\"action\":\"dismiss\",\"changed\":[],\"ids\":[]}"))
            transport.bulk(targets, persona.core.contracts.api.InboxBulkAction.Dismiss)
            val bulk = server.takeRequest()
            assertEquals("/api/v1/inbox/bulk", bulk.path)
            assertEquals("""{"action":"dismiss","items":[{"id":"a","version":"va"},{"id":"b"}]}""", bulk.body.readUtf8())

            // A hub from before /inbox/bulk: 已处理 goes to /inbox/done; 忽略 says it can't.
            server.enqueue(MockResponse().setResponseCode(405).setBody("{\"error\":\"method not allowed\"}"))
            server.enqueue(MockResponse().setBody("{\"changed\":[]}"))
            transport.bulk(targets, persona.core.contracts.api.InboxBulkAction.Done)
            server.takeRequest()
            val old = server.takeRequest()
            assertEquals("/api/v1/inbox/done", old.path)
            assertEquals("""{"done":true,"items":[{"id":"a","version":"va"},{"id":"b"}]}""", old.body.readUtf8())
            server.enqueue(MockResponse().setResponseCode(405).setBody("{\"error\":\"method not allowed\"}"))
            try { transport.bulk(targets, persona.core.contracts.api.InboxBulkAction.Dismiss); fail("An old hub has no dismiss") }
            catch (e: HubFailure) { assertEquals(405, e.status) }
            assertEquals(4, server.requestCount)
        }
    }

    @Test fun documentReadsUseTheReadonlyProxyAndMediaRemainsPairingBound() = runBlocking {
        MockWebServer().use { server ->
            var current = true
            val transport = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient()) { current }
            server.enqueue(MockResponse().setBody("{\"title\":\"文档\"}"))
            transport.readDocument("https://example.com/docx/token?from=message")
            val read = server.takeRequest()
            assertEquals("GET", (read.method).zhAny())
            assertEquals("/api/v1/lark/raw/documents/read", (read.requestUrl?.encodedPath).zhAny())
            assertEquals("https://example.com/docx/token?from=message", (read.requestUrl?.queryParameter("url")).zhAny())
            assertEquals("json", (read.requestUrl?.queryParameter("format")).zhAny())
            assertEquals("Bearer test-token", (read.getHeader("Authorization")).zhAny())
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

    @Test fun documentDetailsReadOnlyTheReadersThreeExtras() = runBlocking {
        MockWebServer().use { server ->
            val transport = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient())
            for ((path, wire) in listOf("reader/info" to "/api/v1/lark/raw/documents/reader/info", "header" to "/api/v1/lark/raw/documents/header",
                "ai-summary" to "/api/v1/lark/raw/documents/ai-summary")) {
                server.enqueue(MockResponse().setBody("{\"views\":3}"))
                assertEquals(3, transport.documentExtra(path, "https://example.com/docx/token")["views"]?.jsonPrimitive?.content?.toInt())
                val read = server.takeRequest()
                assertEquals(wire, read.requestUrl?.encodedPath)
                assertEquals("https://example.com/docx/token", read.requestUrl?.queryParameter("url"))
                assertEquals("Bearer test-token", read.getHeader("Authorization"))
            }
            for ((path, link) in listOf("comments" to "https://example.com/docx/token", "../contacts" to "https://example.com/docx/token",
                "header" to "https://user:pass@example.com/docx/token")) {
                try { transport.documentExtra(path, link); fail("$path for $link must be rejected") }
                catch (_: IllegalArgumentException) { }
            }
            assertEquals(3, server.requestCount)
        }
    }

    @Test fun repliesPreserveClientIdAndMediaNeverForwardsCredentialsOffOrigin() = runBlocking {
        MockWebServer().use { server ->
            val transport = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("{}"))
            transport.reply("item/with#separator", null, "回复", "reply-id")
            val reply = server.takeRequest()
            assertEquals("/api/v1/inbox/item%2Fwith%23separator/reply", (reply.path).zhAny())
            assertEquals("reply-id", (JsonCodec.parseToJsonElement(reply.body.readUtf8()).jsonObject["client_id"]?.jsonPrimitive?.content).zhAny())
            assertEquals("Bearer test-token", (reply.getHeader("Authorization")).zhAny())

            server.enqueue(MockResponse().setBody("image"))
            assertArrayEquals("image".toByteArray(), transport.media("/api/v1/lark/media/image?size=120"))
            val media = server.takeRequest()
            assertEquals("/api/v1/lark/media/image?size=120", (media.path).zhAny())
            assertEquals("Bearer test-token", (media.getHeader("Authorization")).zhAny())
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

    @Test fun aConversationOnALauncherNamesItInThePost() = runBlocking {
        MockWebServer().use { server ->
            val transport = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("{\"session\":{\"machine_id\":\"mac\",\"session_id\":\"c\"}}"))
            transport.createConversation(false, "cc_x")
            val launched = server.takeRequest()
            assertEquals("POST", launched.method)
            assertEquals("/api/v1/assistant/conversations", launched.path)
            assertEquals("""{"prompt":"","main":false,"launcher":"cc_x"}""", launched.body.readUtf8())
            server.enqueue(MockResponse().setBody("{\"session\":{\"machine_id\":\"mac\",\"session_id\":\"d\"}}"))
            transport.createConversation(true)
            assertEquals("""{"prompt":"","main":true}""", server.takeRequest().body.readUtf8())
        }
    }
}
