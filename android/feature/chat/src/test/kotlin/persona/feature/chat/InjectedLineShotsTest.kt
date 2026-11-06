package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import coil3.ImageLoader
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Injected
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.designsystem.*
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ChatRow
import java.io.File

/**
 * Persona's messages for the model (docs/protocol.md "Messages Persona injects") as centered lines, never their words: a
 * task's notice from an older hub (read off the text) and a scheduled run the hub marked, in every style, light and
 * dark. PNGs in `build/injected-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
class InjectedLineShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/injected-shots").also { it.mkdirs() }

    private val notice = Item("n#0", seq = 1, role = "user", kind = "text",
        text = "🔔 任务「整理发布说明」做完了（MacBook）\n（这是 Persona 的任务通知，不是用户发的消息。这个任务是从主对话派出去的……）\n\n" +
            "Agent 最后说的：\n整理好了。\n\n你来决定要不要告诉用户、怎么说……")
    private val run = Item("r#0", seq = 3, role = "user", kind = "text", text = "⏰ 午间整理（定时任务）\n\n看看上午的邮件。",
        injected = Injected("followup", "daily", "午间整理", line = "定时任务「午间整理」开始运行", detail = "看看上午的邮件。"))
    private val cut = Item("c#0", seq = 5, role = "user", kind = "text", text = "🔔 任务「修登录页」被模型服务出错打断了\n（这是 Persona 的任务通知，……）")
    private val answer = Item("a#0", seq = 2, role = "assistant", kind = "text", text = "发布说明整理好了，在 docs/release-notes.md，等你过目。")

    @Test fun linesNotBubbles() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val context = LocalContext.current
                val loader = remember { ImageLoader.Builder(context).build() }
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    for ((type, item) in listOf("user" to notice, "reply" to answer, "user" to run, "user" to cut)) {
                        ChatLineContent(ChatLine.Row(ChatRow(type, item.asLineItem())), SessionKey("mac", "main"), loader,
                            recordedDuration = null, onToggle = {}, onFold = {}, folding = emptySet(), foldErrors = emptyMap(), onFullText = {})
                    }
                }
            }
        }
        rule.onNodeWithText("任务「整理发布说明」做完了").assertIsDisplayed()
        rule.onNodeWithText("定时任务「午间整理」开始运行").assertIsDisplayed()
        rule.onNodeWithText("任务「修登录页」被打断了").assertIsDisplayed()
        rule.onNodeWithText("这是 Persona 的任务通知", substring = true).assertDoesNotExist()
        rule.onNodeWithText("你来决定", substring = true).assertDoesNotExist()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "injected-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
