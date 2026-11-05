package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.*

class ConnectorsContractTest {
    @Test
    fun roundTripMailAccount() {
        val original = MailAccount(
            id = "acc1",
            name = "Test",
            email = "user@example.com",
            provider = "gmail",
            auth = "oauth2",
            username = "user",
            imapHost = "imap.gmail.com",
            imapPort = 993,
            status = "ok",
        )
        val json = JsonCodec.encodeToString(MailAccount.serializer(), original)
        val decoded = JsonCodec.decodeFromString(MailAccount.serializer(), json)
        assertEquals(original, decoded)
        assertFalse(decoded.isError)
        assertEquals("Gmail · Google 账号登录", (decoded.detail).zhAny())
    }

    @Test
    fun roundTripAddMailAccountRequest() {
        val req = AddMailAccountRequest(
            name = "name@example.com",
            email = "name@example.com",
            provider = "imap",
            auth = "password",
            password = "secret",
            imapHost = "imap.example.com",
            imapPort = 993,
            smtpHost = "smtp.example.com",
            smtpPort = 587,
            smtpSecurity = "starttls",
        )
        val json = JsonCodec.encodeToString(AddMailAccountRequest.serializer(), req)
        val decoded = JsonCodec.decodeFromString(AddMailAccountRequest.serializer(), json)
        assertEquals(req, decoded)
    }

    @Test
    fun roundTripLarkStatusAndAccounts() {
        val status = LarkStatus(configured = true, connected = true, name = "张三", lastEventAt = 1700000000L)
        val json = JsonCodec.encodeToString(LarkStatus.serializer(), status)
        val decodedStatus = JsonCodec.decodeFromString(LarkStatus.serializer(), json)
        assertEquals(status, decodedStatus)

        val accounts = LarkAccounts(
            accounts = listOf(
                LarkAccount(id = "u1", title = "张三", tenant = "公司A", unread = 3),
                LarkAccount(id = "u2", title = "李四", tenant = "公司B", unread = 0),
            ),
            default = "u1",
        )
        val accJson = JsonCodec.encodeToString(LarkAccounts.serializer(), accounts)
        val decodedAccs = JsonCodec.decodeFromString(LarkAccounts.serializer(), accJson)
        assertEquals(accounts, decodedAccs)
    }

    @Test
    fun roundTripHubConnector() {
        val connector = HubConnector(
            id = "github",
            name = "GitHub",
            status = "ok",
            account = "octocat",
            via = "macbook",
            enabled = true,
            archive = true,
            inbox = false,
            capabilities = listOf("login", "archive", "inbox"),
        )
        val json = JsonCodec.encodeToString(HubConnector.serializer(), connector)
        val decoded = JsonCodec.decodeFromString(HubConnector.serializer(), json)
        assertEquals(connector, decoded)
    }
}
