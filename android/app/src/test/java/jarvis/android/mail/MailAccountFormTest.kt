package jarvis.android.mail

import jarvis.android.data.JsonCodec
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MailAccountFormTest {
    @Test fun imapRequestOnlyCarriesTypedServers() {
        val f = MailAccountForm(MailAccountForm.Kind.IMAP, "me@example.com", "pw", "", "993", "smtp.example.com", "587")
        val r = f.request()
        assertEquals("imap", r.provider)
        assertEquals("password", r.auth)
        assertEquals("me@example.com", r.name)
        assertNull(r.imapHost)
        assertNull(r.imapPort)
        assertEquals("smtp.example.com", r.smtpHost)
        assertEquals(587, r.smtpPort)
        assertEquals("starttls", r.smtpSecurity)
        val json = JsonCodec.encodeToString(AddMailAccountRequest.serializer(), r)
        assertFalse(json, json.contains("imap_host"))
        assertTrue(json, json.contains("\"smtp_security\":\"starttls\""))
    }

    @Test fun feishuAndGmailFallback() {
        assertEquals("feishu", MailAccountForm(MailAccountForm.Kind.FEISHU, "me@example.com", "pw").request().provider)
        val g = MailAccountForm(MailAccountForm.Kind.GMAIL, "me@example.com", "abcd efgh")
        assertFalse(g.canConnect(gmailPasswordAllowed = false))
        assertTrue(g.canConnect(gmailPasswordAllowed = true))
        assertEquals("gmail", g.request().provider)
        assertFalse(MailAccountForm(MailAccountForm.Kind.IMAP, "nope", "pw").canConnect(false))
    }

    @Test fun googleRedirectParsing() {
        assertEquals(GoogleSignIn.Result.Code("c1", "s1"), GoogleSignIn.parse("c1", "s1", null))
        assertEquals(GoogleSignIn.Result.Cancelled, GoogleSignIn.parse(null, "s1", "access_denied"))
        assertEquals(GoogleSignIn.Result.Failed("server_error", null), GoogleSignIn.parse(null, null, "server_error"))
        assertEquals(GoogleSignIn.Result.Cancelled, GoogleSignIn.parse(null, null, null))
    }

    @Test fun decodesAccountsAndMessages() {
        val a = JsonCodec.decodeFromString(MailAccount.serializer(),
            """{"id":"a1","name":"x","email":"me@example.com","provider":"gmail","auth":"oauth2","username":"","imap_host":"imap.gmail.com","imap_port":993,"smtp_host":"","smtp_port":0,"smtp_security":"","status":"ok","error":"","extra":1}""")
        assertEquals("Gmail · Google 账号登录", a.detail)
        val bad = a.copy(status = "error", error = "Google authorization expired or was revoked")
        assertEquals("Google authorization expired or was revoked", bad.detail)
        assertEquals("飞书邮箱", a.copy(auth = "password", provider = "feishu").detail)
        val m = JsonCodec.decodeFromString(MailMessage.serializer(),
            """{"account_id":"a1","mailbox":"INBOX","uid":4294967295,"message_id":"<x>","subject":"s","from":{"name":"","email":"b@example.com"},"to":null,"date":"2026-10-30T01:02:03Z","snippet":"","seen":false,"flagged":false,"has_attachments":true,"cc":null,"text":"t","html":"","attachments":[{"filename":"a.pdf","mime":"application/pdf","size":12}],"in_reply_to":"","references":null}""")
        assertEquals(4294967295L, m.uid)
        assertEquals("b@example.com", m.from!!.display)
        assertEquals(12L, m.attachments!!.single().size)
        assertEquals(java.time.Instant.parse("2026-10-30T01:02:03Z"), m.date)
    }

    /** 设置 › 连接器's rows say what each connector has now (iOS ConnectorText). */
    @Test fun connectorText() {
        assertEquals("", ConnectorText.mail(null))
        assertEquals("未添加", ConnectorText.mail(emptyList()))
        val ok = MailAccount(id = "a", email = "me@example.com", status = "ok")
        assertEquals("2 个账户", ConnectorText.mail(listOf(ok, ok.copy(id = "b"))))
        assertEquals("2 个 · 有错误", ConnectorText.mail(listOf(ok, ok.copy(id = "b", status = "error"))))
        assertEquals("", ConnectorText.lark(null))
        assertEquals("未配置", ConnectorText.lark(LarkStatus(configured = false)))
        assertEquals("连不上", ConnectorText.lark(LarkStatus(configured = true, connected = false)))
        assertEquals("已连接", ConnectorText.lark(LarkStatus(configured = true, connected = true)))
        assertEquals("我", ConnectorText.lark(LarkStatus(configured = true, connected = true, name = "我")))
        assertEquals("未打开", ConnectorText.device(0))
        assertEquals("3 项已打开", ConnectorText.device(3))
        val st = JsonCodec.decodeFromString(LarkStatus.serializer(), """{"configured":true,"connected":true,"name":"我","last_event_at":1700000000}""")
        assertEquals(1700000000L, st.lastEventAt)
    }
}
