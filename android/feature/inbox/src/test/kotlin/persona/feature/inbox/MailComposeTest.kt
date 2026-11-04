package persona.feature.inbox

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.MailAddress
import persona.core.contracts.api.MailMessage
import java.time.ZoneId

class MailComposeTest {
    @Test
    fun parseAndFormatAddresses() {
        val input = "Alice <alice@example.com>, bob@example.com； Charlie <charlie@example.com>\nDavid <david@example.com>，,"
        val list = MailCompose.parseAddresses(input)
        assertEquals(4, list.size)
        assertEquals(MailAddress("Alice", "alice@example.com"), list[0])
        assertEquals(MailAddress("", "bob@example.com"), list[1])
        assertEquals(MailAddress("Charlie", "charlie@example.com"), list[2])
        assertEquals(MailAddress("David", "david@example.com"), list[3])

        val formatted = MailCompose.formatAddresses(list)
        assertEquals("Alice <alice@example.com>, bob@example.com, Charlie <charlie@example.com>, David <david@example.com>", formatted)
    }

    @Test
    fun addressValidation() {
        assertTrue(MailCompose.looksValid("a@example.com"))
        assertTrue(MailCompose.looksValid("test.user@sub.example.org"))
        assertFalse(MailCompose.looksValid("invalid"))
        assertFalse(MailCompose.looksValid("a@b"))
        assertFalse(MailCompose.looksValid("@example.com"))
        assertFalse(MailCompose.looksValid("a@example."))
        assertFalse(MailCompose.looksValid("a@.example.com"))
        assertFalse(MailCompose.looksValid("a b@example.com"))

        assertTrue(MailCompose.fieldValid("", required = false))
        assertFalse(MailCompose.fieldValid("", required = true))
        assertTrue(MailCompose.fieldValid("a@example.com, c@example.org", required = true))
        assertFalse(MailCompose.fieldValid("a@example.com, invalid", required = false))
    }

    @Test
    fun subjects() {
        assertEquals("Re: 项目进展", MailCompose.replySubject("项目进展"))
        assertEquals("Re: 项目进展", MailCompose.replySubject("Re: 项目进展"))
        assertEquals("re: 项目进展", MailCompose.replySubject("re: 项目进展"))
        assertEquals("回复：项目进展", MailCompose.replySubject("回复：项目进展"))
        assertEquals("Fwd: 报销单", MailCompose.forwardSubject("报销单"))
        assertEquals("Fwd: 报销单", MailCompose.forwardSubject("Fwd: 报销单"))
        assertEquals("fw: 报销单", MailCompose.forwardSubject("fw: 报销单"))
        assertEquals("转发：报销单", MailCompose.forwardSubject("转发：报销单"))
    }

    @Test
    fun replySingleAndAll() {
        val zone = ZoneId.of("Asia/Shanghai")
        val original = MailMessage(
            accountId = "acc-1",
            uid = 100,
            subject = "周会纪要",
            from = MailAddress("Boss", "boss@example.com"),
            to = listOf(MailAddress("Alice", "alice@example.com"), MailAddress("Me", "me@example.com")),
            cc = listOf(MailAddress("Bob", "bob@example.com")),
            date = "2026-11-04T10:00:00Z",
            text = "今天讨论了Q4目标。\n请大家确认。",
            messageId = "<msg-100@example.com>",
            references = listOf("<root@example.com>"),
        )

        // 单人回复
        val draftSingle = MailCompose.reply(original, "acc-1", me = "me@example.com", all = false, zone = zone)
        assertEquals("Boss <boss@example.com>", draftSingle.to)
        assertEquals("", draftSingle.cc)
        assertEquals("Re: 周会纪要", draftSingle.subject)
        assertEquals("<msg-100@example.com>", draftSingle.inReplyTo)
        assertEquals(listOf("<root@example.com>", "<msg-100@example.com>"), draftSingle.references)
        assertTrue(draftSingle.body.contains("Boss"))
        assertTrue(draftSingle.body.contains("> 今天讨论了Q4目标。"))

        // 全部回复（应排除我自己的邮箱 me@example.com）
        val draftAll = MailCompose.reply(original, "acc-1", me = "me@example.com", all = true, zone = zone)
        assertEquals("Boss <boss@example.com>, Alice <alice@example.com>", draftAll.to)
        assertEquals("Bob <bob@example.com>", draftAll.cc)
        assertEquals("Re: 周会纪要", draftAll.subject)
    }

    @Test
    fun forwardMail() {
        val zone = ZoneId.of("Asia/Shanghai")
        val original = MailMessage(
            accountId = "acc-1",
            uid = 101,
            subject = "产品设计案",
            from = MailAddress("Designer", "designer@example.com"),
            to = listOf(MailAddress("Team", "team@example.com")),
            date = "2026-11-04T10:00:00Z",
            text = "附上初版设计稿供评审。",
        )
        val fwd = MailCompose.forward(original, "acc-1", zone = zone)
        assertEquals("Fwd: 产品设计案", fwd.subject)
        assertEquals("", fwd.to)
        assertTrue(fwd.body.contains("---------- 转发的邮件 ----------"))
        assertTrue(fwd.body.contains("发件人：Designer <designer@example.com>"))
        assertTrue(fwd.body.contains("主题：产品设计案"))
        assertTrue(fwd.body.contains("附上初版设计稿供评审。"))
    }

    @Test
    fun parseMailtoUri() {
        val uri = "mailto:support@example.com?subject=Hello%20World&body=Need%20Help%21&cc=lead@example.com"
        val draft = MailCompose.parseMailto(uri, defaultAccountId = "acc-1")
        assertEquals("acc-1", draft.accountId)
        assertEquals("support@example.com", draft.to)
        assertEquals("Hello World", draft.subject)
        assertEquals("Need Help!", draft.body)
        assertEquals("lead@example.com", draft.cc)
    }

    @Test
    fun buildSendRequest() {
        val draft = MailDraft(
            accountId = "acc-1",
            to = "Alice <alice@example.com>",
            cc = "bob@example.com",
            subject = "测试",
            body = "你好",
            inReplyTo = "<m1>",
            references = listOf("<root>", "<m1>"),
        )
        val req = MailCompose.request(draft, bcc = "secret@example.com")
        assertEquals("acc-1", req.accountId)
        assertEquals(listOf(MailAddress("Alice", "alice@example.com")), req.to)
        assertEquals(listOf(MailAddress("", "bob@example.com")), req.cc)
        assertEquals(listOf(MailAddress("", "secret@example.com")), req.bcc)
        assertEquals("测试", req.subject)
        assertEquals("你好", req.text)
        assertEquals("<m1>", req.inReplyTo)
        assertEquals("<root> <m1>", req.references)
    }
}
