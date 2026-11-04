package persona.feature.assistant

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.designsystem.*
import java.io.File
import java.time.Instant

/**
 * The Persona tab's history drawer drawn on the JVM with Robolectric's native graphics (no emulator), in every style,
 * light and dark: the main conversation first, as 「Persona」 with Persona's face, and 更多 without 资料页 (it is
 * 设置 › Persona). Each is checked to draw something and written as a PNG to `build/drawer-shots/` (or
 * `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w400dp-h2100dp-xhdpi")
class AssistantDrawerShotsTest {
    @get:Rule val rule = createComposeRule()

    private val minutesAgo = { m: Long -> Instant.now().minusSeconds(m * 60).toString() }
    private val main = Session(machineId = "hub", sessionId = "main", title = "主对话", status = "idle",
        updatedAt = minutesAgo(2), lastText = "今天的日程已经整理好了。")
    private val pinned = Session(machineId = "hub", sessionId = "p1", title = "周报草稿", status = "done",
        updatedAt = minutesAgo(30), lastText = "第二版写好了。")
    private val recent = listOf(
        Session(machineId = "hub", sessionId = "r1", title = "整理收件箱", status = "working", updatedAt = minutesAgo(5)),
        Session(machineId = "hub", sessionId = "r2", title = "周末去哪儿", status = "idle", updatedAt = minutesAgo(90)),
    )

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/drawer-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun drawsTheDrawerInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var groups by mutableStateOf(ConversationGroups(main, listOf(pinned), recent))
        var opened: SessionKey? = null
        var mainTapped = false
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(1100.dp).background(PersonaTheme.palette.background)) {
                    AssistantDrawer(PersonaState.Idle, "", {}, groups, active = null, hasMain = groups.main != null,
                        onEnd = null, onMain = { mainTapped = true }, onOpen = { opened = it }, onInfo = {},
                        onNew = {}, onHome = {}, onSettings = {}, onTodos = {}, todoCount = 2)
                }
            }
        }
        // The main conversation reads 「Persona」 (the session keeps its own title), and 资料页 is gone.
        rule.onAllNodesWithText("主对话").assertCountEquals(0)
        rule.onAllNodesWithText("资料页").assertCountEquals(0)
        rule.onAllNodesWithText("Persona")[1].performClick()
        assertEquals(main.key, opened)
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            capture("drawer-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
        // No main conversation yet: the same row starts one.
        style = AppStyle.Glass; dark = false
        groups = ConversationGroups(null, emptyList(), recent)
        rule.onNodeWithText("开始主对话").performClick()
        assertTrue(mainTapped)
        capture("drawer-no-main-glass-light")
    }
}
