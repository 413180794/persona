package jarvis.core.hub

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test
import jarvis.core.contracts.api.MailReference
import jarvis.core.contracts.api.MailReplyTarget
import jarvis.core.contracts.api.MailReplyTargetChanged
import java.io.ByteArrayOutputStream

class HttpMailTransportTest {
    @Test fun attachmentResumeAndCheckedReplyConflict() = runBlocking {
        MockWebServer().use { server ->
            val transport = HttpMailTransport("scope", server.url("/").toString(), "test-token", OkHttpClient())
            val ref = MailReference("account/1", "Archive & work", listOf(7))
            server.enqueue(MockResponse().setResponseCode(206).setHeader("Content-Range", "bytes 3-5/6").setBody("def"))
            val output = ByteArrayOutputStream().apply { write("abc".toByteArray()) }
            var last: Pair<Long, Long?>? = null
            transport.downloadAttachment(ref, 7, "stable-id", output, 3) { read, total -> last = read to total }
            assertEquals("abcdef", output.toString("UTF-8"))
            assertEquals(6L to 6L, last)
            val download = server.takeRequest()
            assertEquals("bytes=3-", download.getHeader("Range"))
            assertEquals("Bearer test-token", download.getHeader("Authorization"))
            assertEquals("/api/v1/mail/messages/account%2F1/7/attachments/stable-id?mailbox=Archive%20%26%20work", download.path)

            val inbox = HttpInboxAssistantTransport("scope", server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"对象已变化","code":"mail_reply_target_changed","retryable":false,"delivered":"none"}"""))
            try { inbox.replyMail("item", "reply", "first-id", MailReplyTarget(7, "version")); fail("Must require confirmation") }
            catch (_: MailReplyTargetChanged) { }
            val rejected = server.takeRequest().body.readUtf8()
            assertTrue(rejected.contains("\"expected_uid\":7"))
            assertTrue(rejected.contains("\"expected_version\":\"version\""))
            assertTrue(rejected.contains("\"client_id\":\"first-id\""))
            server.enqueue(MockResponse().setBody("""{"id":"item"}"""))
            inbox.replyMail("item", "reply", "confirmed-id", MailReplyTarget(8, "new-version"))
            assertTrue(server.takeRequest().body.readUtf8().contains("\"expected_uid\":8"))
        }
    }
    @Test fun readsNullableMetadataEncodesMailboxAndWritesSeenWithoutMarkingDone() = runBlocking {
        MockWebServer().use { server ->
            var current = true
            val transport = HttpMailTransport("scope", server.url("/").toString(), "test-token", OkHttpClient()) { current }
            server.enqueue(MockResponse().setBody("""{"id":"item/1","mail":{"account_id":"account/1","mailbox":"Archive & work","uids":[4,7]}}"""))
            val ref = requireNotNull(transport.item("item/1").mail)
            assertEquals("/api/v1/inbox/item%2F1", server.takeRequest().path)
            server.enqueue(MockResponse().setBody("""{"message":{"account_id":"account/1","mailbox":"Archive & work","uid":7,"to":null,"cc":null,"attachments":null,"html":"<p>正文</p>","extra":true}}"""))
            val mail = transport.message(ref, 7)
            assertTrue(mail.attachments.isEmpty())
            assertTrue(mail.to.isEmpty())
            assertEquals("<p>正文</p>", mail.html)
            val read = server.takeRequest()
            assertEquals("/api/v1/mail/messages/account%2F1/7?mailbox=Archive%20%26%20work", read.path)
            assertEquals("Bearer test-token", read.getHeader("Authorization"))
            server.enqueue(MockResponse().setBody("""{"ok":true}"""))
            transport.setSeen(ref, 7, true)
            val flags = server.takeRequest()
            assertEquals("/api/v1/mail/messages/account%2F1/7/flags", flags.path)
            assertEquals("""{"mailbox":"Archive & work","seen":true}""", flags.body.readUtf8())
            server.enqueue(MockResponse().setBody("""{"items":[]}"""))
            transport.refreshInbox()
            assertEquals("/api/v1/inbox/refresh", server.takeRequest().path)
            current = false
            try { transport.item("item/1"); fail("Stale pairing must not send requests") }
            catch (_: CancellationException) { }
            assertEquals(4, server.requestCount)
        }
    }
}
