package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import coil3.ImageLoader
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.TaskNote
import persona.core.designsystem.*
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ChatRow
import persona.core.domain.lines.TaskIndex
import java.io.File

/**
 * A background task's notice (iOS TaskNoteRow): one line in the tools' look with what it is, the call's description and
 * how it went; a tap shows the command and "Jump to the step that started it", which asks the chat for the call that started it. Every
 * style, light and dark, closed and open. PNGs in `build/task-note-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
class TaskNoteShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/task-note-shots").also { it.mkdirs() }

    private val use = Item("use#0", seq = 2, kind = "tool_use", toolUseId = "toolu_1", toolName = "Bash", ts = "2026-11-05T10:00:00Z",
        toolInput = kotlinx.serialization.json.Json.parseToJsonElement("""{"command":"npm test","description":"跑全部单测"}"""))
    private val result = Item("result#0", seq = 3, kind = "tool_result", toolUseId = "toolu_1",
        text = "Command running in background with ID: b1")
    private val done = Item("note#0", seq = 5, kind = "notice", text = "后台命令结束", ts = "2026-11-05T10:02:05Z",
        task = TaskNote(kind = "shell", status = "completed", title = "npm test", taskId = "b1", detail = "npm test -- --runInBand"))
    private val failed = Item("note#1", seq = 6, kind = "notice", text = "后台命令失败",
        task = TaskNote(kind = "shell", status = "failed", title = "make lint", exitCode = 2, detail = "make lint"))

    @Test fun aNoticeOpensAndJumpsToItsCall() {
        val jumps = mutableListOf<String>()
        val items = listOf(use, result, done, failed).map { it.asLineItem() }
        val index = TaskIndex.build(items)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val context = LocalContext.current
                val loader = remember { ImageLoader.Builder(context).build() }
                CompositionLocalProvider(LocalChatTaskIndex provides index, LocalChatJumpToCall provides { call, _ -> jumps += call }) {
                    Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                        for (item in items.drop(2)) {
                            ChatLineContent(ChatLine.Row(ChatRow("notice", item)), SessionKey("mac", "main"), loader,
                                recordedDuration = null, onToggle = {}, onFold = {}, folding = emptySet(), foldErrors = emptyMap(), onFullText = {})
                        }
                    }
                }
            }
        }
        rule.onNodeWithText("跑全部单测").assertIsDisplayed()
        rule.onNodeWithText("后台命令 · ✓ · 2 分").assertIsDisplayed()
        rule.onNodeWithText("后台命令 · ✗ 退出码 2").assertIsDisplayed()
        rule.onNodeWithText("跳到发起的这一步").assertDoesNotExist()
        shots("closed")
        rule.onNodeWithText("跑全部单测").performClick()
        rule.onNodeWithText("$ npm test -- --runInBand").assertIsDisplayed()
        shots("open")
        rule.onNodeWithText("跳到发起的这一步").performClick()
        assertEquals(listOf("toolu_1"), jumps)
        // The failed one has no call we know of: nothing to jump to.
        rule.onNodeWithText("make lint").performClick()
        rule.onNodeWithText("$ make lint").assertIsDisplayed()
        rule.onAllNodesWithText("跳到发起的这一步").assertCountEquals(1)
    }

    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)

    private fun shots(state: String) {
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "task-note-$state-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
