package persona.feature.settings

import android.content.Context
import android.content.res.Resources
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.*

// Robolectric: the words come from the app's resources, in Simplified Chinese (robolectric.properties).
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class SettingsConnectorsTest {
    private val res: Resources get() = ApplicationProvider.getApplicationContext<Context>().resources

    @Test
    fun stubConnectorsReturnsData() = runBlocking {
        val stub = object : ConnectorsApiStub() {
            override suspend fun connectors(all: Boolean): List<HubConnector> = listOf(
                HubConnector(id = "github", name = "GitHub", status = "ok", account = "testuser"),
            )
            override suspend fun mailAccounts(): List<MailAccount> = listOf(
                MailAccount(id = "1", email = "test@example.com", status = "ok"),
            )
            override suspend fun larkStatus(): LarkStatus = LarkStatus(configured = true, connected = true, name = "测试用户")
            override suspend fun larkAccounts(): LarkAccounts = LarkAccounts(
                accounts = listOf(LarkAccount(id = "1", title = "测试用户", tenant = "测试企业")),
                default = "1",
            )
        }

        val connectors = stub.connectors(all = true)
        assertEquals(1, connectors.size)
        assertEquals("GitHub", connectors[0].name)
        assertEquals("已连接", ConnectorText.state(res, connectors[0]))
        assertEquals("testuser", ConnectorText.summary(res, connectors[0]))

        val accounts = stub.mailAccounts()
        assertEquals(1, accounts.size)
        assertEquals("1 个账户", ConnectorText.mail(res, accounts))

        val lark = stub.larkStatus()
        assertTrue(lark.connected)
        assertEquals("测试用户", ConnectorText.lark(res, lark))

        val larkAccounts = stub.larkAccounts()
        assertNotNull(larkAccounts)
        assertEquals(1, larkAccounts.accounts.size)
    }

    @Test
    fun testMailAccountFormValidation() {
        val gmailForm = MailAccountForm(kind = MailAccountForm.Kind.GMAIL, email = "me@example.com", password = "app-password")
        assertTrue(gmailForm.canConnect(gmailPasswordAllowed = true))
        val req = gmailForm.request()
        assertEquals("gmail", req.provider)
        assertEquals("me@example.com", req.email)
        assertEquals("app-password", req.password)

        val feishuForm = MailAccountForm(kind = MailAccountForm.Kind.FEISHU, email = "work@example.com", password = "larkpassword")
        assertTrue(feishuForm.canConnect(gmailPasswordAllowed = false))
        val feishuReq = feishuForm.request()
        assertEquals("feishu", feishuReq.provider)
        assertEquals("work@example.com", feishuReq.email)
        assertEquals("larkpassword", feishuReq.password)
    }

    @Test
    fun feishuRowOnlyWhileTheHubHasFeishu() {
        val hub = listOf(
            HubConnector(id = "mail", name = "邮箱"),
            HubConnector(id = "lark", name = "飞书"),
            HubConnector(id = "github", name = "GitHub"),
        )
        val github = ConnectorRow.Hub(hub[2])
        // With the Lark extension (or a hub from before it): Lark between mail and the hub's own connectors.
        assertEquals(listOf(ConnectorRow.Phone, ConnectorRow.Mail, ConnectorRow.Lark, github),
            connectorRows(hub, lark = persona.core.contracts.LarkFeatures.of(setOf("lark", "lark_moments")).chats))
        assertEquals(listOf(ConnectorRow.Phone, ConnectorRow.Mail, ConnectorRow.Lark, github),
            connectorRows(hub, lark = persona.core.contracts.LarkFeatures.of(setOf("lark_attachments")).chats))
        // Without it there is no Lark row (its page reads /api/v1/lark/...), even if the hub still lists one.
        assertEquals(listOf(ConnectorRow.Phone, ConnectorRow.Mail, github),
            connectorRows(hub, lark = persona.core.contracts.LarkFeatures.of(setOf("client_ops")).chats))
    }
}
