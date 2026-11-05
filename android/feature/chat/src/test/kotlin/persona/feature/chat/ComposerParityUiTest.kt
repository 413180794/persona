package persona.feature.chat

import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Session
import persona.core.designsystem.PersonaTheme

/** The parity pieces' behaviour in an English app (#2976): /model's one-off open, and your own quote's heading. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class ComposerParityUiTest {
    @get:Rule val rule = createComposeRule()

    private val session = Session(machineId = "box", sessionId = "s1", agent = "claude", status = "idle")

    @Test fun modelOpensTheSheetOnceNotWhenTheBarComesBack() {
        var requested by mutableStateOf(false)
        var visible by mutableStateOf(true)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                val scope = rememberCoroutineScope()
                val resources = LocalResources.current
                val chrome = remember { ChatChromeState(resources, null, scope, {}, {}) }
                // A question card hides the composer, and with it the bar.
                if (visible) ChatModeBar(session, chrome, online = true, onPickModel = {},
                    openRequested = requested, onOpenHandled = { requested = false })
            }
        }
        fun sheets() = rule.onAllNodesWithText("Chat mode").fetchSemanticsNodes().size
        assertEquals(0, sheets())
        requested = true
        rule.waitForIdle()
        assertEquals(1, sheets())
        assertEquals(false, requested)
        visible = false
        rule.waitForIdle()
        visible = true
        rule.waitForIdle()
        assertEquals(0, sheets())
    }

    @Test fun yourOwnQuoteReadsAsYoursInEnglish() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                Column {
                    ChatContextCard(messageQuote("look here", user = true, agent = "Claude", msg = "m1"))
                    ChatContextCard(messageQuote("done", user = false, agent = "Claude", msg = "m2"))
                }
            }
        }
        rule.onNodeWithText("Quoting your message").assertExists()
    }
}
