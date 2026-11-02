package jarvis.android.mail

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.ZoneId

class MailComposeTest {
    private val zone = ZoneId.of("Asia/Shanghai")

    private val original = MailMessage(
        accountId = "acc1", uid = 501, messageId = "<501@example.com>", subject = "下周的分享",
        from = MailAddress("Lucy Wang", "lucy@example.com"),
        to = listOf(MailAddress("我", "me@example.com"), MailAddress(null, "bob@example.com")),
        cc = listOf(MailAddress("Tom", "tom@example.com"), MailAddress(null, "ME@example.com")),
        dateText = "2026-10-30T07:05:00Z",
        text = (1..50).joinToString("\n") { "line $it" },
        references = listOf("<499@example.com>"),
    )

    @Test fun parsesAddressFields() {
        val list = MailCompose.parseAddresses(" a@example.com，Lucy Wang <lucy@example.com>; \"Tom T\" <tom@example.com>\n;; ")
        assertEquals(3, list.size)
        assertEquals(MailAddress(null, "a@example.com"), list[0])
        assertEquals(MailAddress("Lucy Wang", "lucy@example.com"), list[1])
        assertEquals(MailAddress("Tom T", "tom@example.com"), list[2])
        assertEquals("a@example.com, Lucy Wang <lucy@example.com>", MailCompose.formatAddresses(list.take(2)))
    }

    @Test fun validatesFields() {
        assertTrue(MailCompose.looksValid("me@example.com"))
        assertFalse(MailCompose.looksValid("me@example"))
        assertFalse(MailCompose.looksValid("@example.com"))
        assertFalse(MailCompose.looksValid("a b@example.com"))
        assertFalse(MailCompose.fieldValid("", required = true))
        assertTrue(MailCompose.fieldValid("", required = false))
        assertFalse(MailCompose.fieldValid("me@example.com, nope", required = true))
    }

    @Test fun subjects() {
        assertEquals("Re: 下周的分享", MailCompose.replySubject("下周的分享"))
        assertEquals("RE: x", MailCompose.replySubject("RE: x"))
        assertEquals("回复：x", MailCompose.replySubject("回复：x"))
        assertEquals("Fwd: x", MailCompose.forwardSubject("x"))
        assertEquals("FW: x", MailCompose.forwardSubject("FW: x"))
    }

    @Test fun replyQuotesFortyLinesAndThreads() {
        val d = MailCompose.reply(original, "acc1", "me@example.com", all = false, zone = zone)
        assertEquals("acc1", d.accountId)
        assertEquals("Lucy Wang <lucy@example.com>", d.to)
        assertEquals("", d.cc)
        assertEquals("Re: 下周的分享", d.subject)
        assertEquals("<501@example.com>", d.inReplyTo)
        assertEquals(listOf("<499@example.com>", "<501@example.com>"), d.references)
        assertTrue(d.body.startsWith("\n\n2026年10月30日 15:05，Lucy Wang 写道：\n> line 1\n"))
        assertTrue(d.body.contains("> line 40"))
        assertFalse(d.body.contains("line 41"))
    }

    @Test fun replyAllDropsMeAndDuplicates() {
        val d = MailCompose.reply(original, "acc1", "me@example.com", all = true, zone = zone)
        assertEquals("Lucy Wang <lucy@example.com>, bob@example.com", d.to)
        assertEquals("Tom <tom@example.com>", d.cc)
    }

    @Test fun forwardCarriesTheHeaderAndText() {
        val d = MailCompose.forward(original, "acc1", zone)
        assertEquals("Fwd: 下周的分享", d.subject)
        assertEquals("", d.to)
        assertNull(d.inReplyTo)
        assertTrue(d.body.contains("---------- 转发的邮件 ----------"))
        assertTrue(d.body.contains("发件人：Lucy Wang <lucy@example.com>"))
        assertTrue(d.body.contains("line 50"))
    }

    @Test fun htmlOnlyMailIsQuotedAsText() {
        val m = original.copy(text = "", html = "<p>Hello&nbsp;<b>there</b></p><p>second&amp;more</p>")
        val d = MailCompose.reply(m, "acc1", null, all = false, zone = zone)
        assertTrue(d.body, d.body.contains("> Hello there\n> second&more"))
    }

    @Test fun requestJoinsReferencesForTheHub() {
        val d = MailCompose.reply(original, "acc1", "me@example.com", all = true, zone = zone)
        val r = MailCompose.request(d, bcc = "x@example.com")
        assertEquals("<499@example.com> <501@example.com>", r.references)
        assertEquals(2, r.to.size)
        assertEquals(listOf(MailAddress(null, "x@example.com")), r.bcc)
        val json = jarvis.android.data.JsonCodec.encodeToString(SendMailRequest.serializer(), r)
        assertTrue(json, json.contains("\"in_reply_to\":\"<501@example.com>\""))
        assertTrue(json, json.contains("\"account_id\":\"acc1\""))
    }
}
