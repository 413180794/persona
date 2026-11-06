package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.hasSetTextAction
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File
import java.time.Instant

/**
 * Settings › Lark bot (#2806) drawn with Robolectric's native graphics, in every style, light and dark: a bot that is
 * connected and paired (with the agents chatted with in Feishu), and one with no credentials yet, where Verify and save
 * waits for the secret. PNGs in `build/feishu-bot-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1600dp-xhdpi")
class FeishuBotShotsTest {
    @get:Rule val rule = createComposeRule()

    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)
    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/feishu-bot-shots").also { it.mkdirs() }

    private class Bot(val status: FeishuBotStatus) : FeishuBotSettings {
        override suspend fun status() = status
        override suspend fun update(update: FeishuBotUpdate) = status
    }

    private val paired = FeishuBotStatus(enabled = true, appId = "cli_a1b2c3", domain = "https://open.feishu.cn", hasSecret = true, connected = true,
        lastEventAt = Instant.now().minusSeconds(300).epochSecond, botName = "Persona 助手",
        owner = FeishuBotOwner("示例用户", 1), target = "主对话",
        agents = listOf(FeishuBotAgent("修登录页的跳转", "laptop", "s1"), FeishuBotAgent("整理发布说明", "macmini", "s2"),
            FeishuBotAgent("同一会话的另一个话题", "laptop", "s1")))

    private fun shots(name: String) {
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "feishu-bot-$name-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    private fun show(api: FeishuBotSettings) = rule.setContent {
        PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
            key(style, dark) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) { FeishuBotPage(api, onBack = {}) }
            }
        }
    }

    @Test fun aPairedBot() {
        show(Bot(paired))
        rule.waitUntil(5_000) { rule.onAllNodes(androidx.compose.ui.test.hasText("已连接")).fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("Persona 助手").assertIsDisplayed()
        rule.onNodeWithText("修登录页的跳转").assertIsDisplayed()
        rule.onNodeWithText("验证并保存").assertIsNotEnabled()
        rule.onAllNodes(androidx.compose.ui.test.hasText("https://open.feishu.cn")).assertCountEquals(0) // Feishu, not the URL
        shots("paired")
    }

    @Test fun noCredentialsYet() {
        show(Bot(FeishuBotStatus()))
        rule.waitUntil(5_000) { rule.onAllNodes(androidx.compose.ui.test.hasText("未打开")).fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("怎么建机器人").assertIsDisplayed()
        shots("empty")
        rule.onAllNodes(hasSetTextAction())[0].performTextInput("cli_new")
        rule.onNodeWithText("验证并保存").assertIsNotEnabled() // the secret too, the first time
        rule.onAllNodes(hasSetTextAction())[1].performTextInput("s3cret")
        rule.onNodeWithText("验证并保存").assertIsEnabled()
    }
}
