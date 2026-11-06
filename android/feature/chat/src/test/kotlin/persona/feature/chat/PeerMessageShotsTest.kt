package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.designsystem.*
import java.io.File

/**
 * A message another Claude session sent ("From <name>") draws its words as Markdown: a teammate's status note (which the
 * hub turns from JSON into Markdown) and a plain report. Drawn on the JVM with Robolectric's native graphics in every
 * style, light and dark. PNGs in `build/peer-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
class PeerMessageShotsTest {
    @get:Rule val rule = createComposeRule()

    private val note = "**[to reviewer] web branch rebased onto master; mock file matches**\n\n" +
        "The web branch is now rebased onto master, the same base the iOS branch used. The mock file matches the iOS " +
        "copy byte for byte again.\n\n- **Why the rebase:** master changed the mock file after the earlier base."
    private val plain = "Report is in /tmp/report.md\nAll checks pass; nothing else changed."

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/peer-shots").also { it.mkdirs() }

    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)

    @Test fun peerMessagesDrawTheirWordsAsMarkdown() {
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    MergedUserBubble(listOf(Item("p1", role = "user", kind = "text", text = note, from = "worker-1", ts = "2026-11-04T16:10:18Z")))
                    MergedUserBubble(listOf(Item("p2", role = "user", kind = "text", text = plain, from = "docs", ts = "2026-11-04T16:12:00Z")))
                }
            }
        }
        rule.onNodeWithText("来自 worker-1").assertExists()
        // The Markdown is parsed off the main thread; until then the raw text shows.
        rule.waitUntil(10_000) { rule.onAllNodesWithText("**", substring = true).fetchSemanticsNodes().isEmpty() }
        rule.onNodeWithText("Why the rebase:", substring = true).assertExists()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "peer-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
