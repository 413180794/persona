package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File
import java.time.Instant

/**
 * Settings › Connectors › Messages (its steps on the Mac, "Hand to Persona", "Conversations" and a conversation), Settings › Lark › Message display and "Add mail account" ›
 * Outlook and Gmail (with and without the hub's Google sign-in, #2442), in the three styles, light and dark (#3045). Written as PNGs to `build/parity-shots/` to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ParityInboxShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/parity-shots").also { it.mkdirs() }

    private enum class Page { MessagesGuide, MessagesOk, MessagesChats, MessagesThread, Display, OutlookRegister, OutlookSignIn, GmailSignIn, GmailNoGoogle }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h1100dp-xhdpi")
    @Test fun pages() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var page by mutableStateOf(Page.MessagesGuide)
        var run by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                key(run) {
                    Box(Modifier.testTag("shot").width(400.dp).height(1100.dp).background(PersonaTheme.palette.background)) {
                        when (page) {
                            Page.MessagesGuide -> MessagesConnectorPage(Api(ok = false), onBack = {})
                            Page.MessagesOk, Page.MessagesChats, Page.MessagesThread -> MessagesConnectorPage(Api(ok = true), onBack = {})
                            Page.Display -> LarkMessageDisplayPage(Api(ok = true), onBack = {})
                            Page.OutlookRegister -> AddMailAccountPage(Api(ok = true, outlook = false), onBack = {}, onAdded = {},
                                start = MailAccountForm(MailAccountForm.Kind.OUTLOOK))
                            Page.OutlookSignIn -> AddMailAccountPage(Api(ok = true, outlook = true), onBack = {}, onAdded = {},
                                start = MailAccountForm(MailAccountForm.Kind.OUTLOOK))
                            Page.GmailSignIn -> AddMailAccountPage(Api(ok = true, google = true), onBack = {}, onAdded = {})
                            Page.GmailNoGoogle -> AddMailAccountPage(Api(ok = true, google = false), onBack = {}, onAdded = {})
                        }
                    }
                }
            }
        }
        val looks = listOf(AppStyle.Glass to false, AppStyle.Glass to true, AppStyle.Brief to false, AppStyle.Brief to true,
            AppStyle.Board to false, AppStyle.Board to true)
        for ((s, d) in looks) {
            style = s; dark = d
            for (next in Page.entries) {
                page = next; run++
                rule.waitForIdle()
                when (next) {
                    Page.MessagesGuide -> rule.assertText("重新检查")
                    Page.MessagesOk -> { rule.assertText("交给 Persona"); rule.assertText("删除 hub 上已存的信息") }
                    Page.MessagesChats -> { rule.onAllNodesWithText("会话")[0].performClick(); rule.waitForIdle(); rule.assertText("Alex") }
                    Page.MessagesThread -> {
                        rule.onAllNodesWithText("会话")[0].performClick(); rule.waitForIdle()
                        rule.onAllNodesWithText("Alex")[0].performClick(); rule.waitForIdle()
                        rule.assertText("周五见")
                    }
                    Page.Display -> { rule.assertText("防撤回"); rule.assertText("编辑历史") }
                    Page.OutlookRegister -> rule.assertText("保存 client ID")
                    Page.OutlookSignIn -> rule.assertText("使用 Microsoft 账号登录")
                    Page.GmailSignIn -> rule.assertText("使用 Google 账号登录")
                    Page.GmailNoGoogle -> {
                        rule.assertText("hub 还没有配置 Google 登录"); rule.assertText("邮箱地址")
                        assertTrue(rule.onAllNodesWithText("使用 Google 账号登录").fetchSemanticsNodes().isEmpty())
                    }
                }
                capture("parity-${next.name.lowercase()}-${s.name.lowercase()}-${if (d) "dark" else "light"}")
            }
        }
    }

    private fun androidx.compose.ui.test.junit4.ComposeContentTestRule.assertText(text: String) =
        assertTrue("missing $text", onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty())

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    // ---- made-up data

    private class Api(val ok: Boolean, val outlook: Boolean = false, val google: Boolean? = null) : ConnectorsApiStub() {
        private val now = Instant.now()
        override suspend fun messagesStatus() = if (ok) MessagesStatus("Studio", "ok", ai = true)
            else MessagesStatus("Studio", "no_permission", error = "authorization denied",
                guide = listOf("打开「系统设置 › 隐私与安全性 › 完全磁盘访问权限」", "把 Persona Link 加进去并打开开关", "回到这里点「重新检查」"), ai = false)
        override suspend fun messagesChats(before: String?) = MessagesChatsPage(listOf(
            MessagesChat("c1", "Alex", members = listOf(MessagesPerson("+1 555 0100", "Alex")), services = listOf("iMessage"),
                last = now.minusSeconds(300).toString(), preview = "周五见", unread = 2),
            MessagesChat("c2", "周末徒步", group = true, members = listOf(MessagesPerson("a@example.com", "Sam"), MessagesPerson("b@example.com", "Kim")),
                services = listOf("iMessage"), last = now.minusSeconds(86_400 * 3).toString(), preview = "好的", fromMe = true),
        ))
        override suspend fun messagesThread(chatId: String, before: String?) = MessagesPage(
            MessagesChat("c1", "Alex", services = listOf("iMessage", "SMS")), listOf(
                MessagesMessage("m1", now.minusSeconds(7_200).toString(), sender = MessagesPerson("+1 555 0100", "Alex"), text = "明天有空吗？", service = "iMessage"),
                MessagesMessage("m2", now.minusSeconds(7_000).toString(), fromMe = true, text = "下午可以", service = "iMessage", edited = true),
                MessagesMessage("m3", now.minusSeconds(600).toString(), sender = MessagesPerson("+1 555 0100", "Alex"), unsent = true, service = "SMS"),
                MessagesMessage("m4", now.minusSeconds(300).toString(), sender = MessagesPerson("+1 555 0100", "Alex"), text = "周五见", service = "SMS",
                    attachments = listOf(MessagesAttachment("image", "photo.jpg"))),
            ), more = true, before = "m1")
        override suspend fun larkMessageDisplay() = LarkMessageDisplay("acct", antiRecall = true, showEditHistory = false)
        override suspend fun microsoftConfig() = MicrosoftConfig(configured = outlook, clientId = if (outlook) "00000000-0000-0000-0000-000000000000" else null,
            redirectUri = "persona-mail-oauth://outlook")
        override suspend fun googleConfig() = google?.let(::GoogleConfig)
    }
}
