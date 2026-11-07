package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Session

/** Which mark an agent's row draws and whether it carries the command line's </> badge (#6390), as iOS and the web. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class AgentMarkTest {
    @get:Rule val rule = createComposeRule()

    private fun session(agent: String? = null, surface: String? = null, source: String? = null, assistant: Boolean = false, label: String? = null) =
        Session(sessionId = "s", agent = agent, agentSurface = surface, sourceKind = source, assistant = assistant, agentLabelHint = label)

    @Test fun claudeAndAnOlderHubAreClaude() {
        for (agent in listOf(null, "", "claude")) assertEquals(AgentMarkKind.Claude, session(agent).agentMark?.kind)
    }

    @Test fun codexFromATerminalOrItsAppIsCodex() {
        assertEquals(AgentMarkKind.Codex, session("codex").agentMark?.kind)
        assertEquals(AgentMarkKind.Codex, session("claude", source = "codex_history").agentMark?.kind)
        assertEquals(AgentMarkKind.Codex, session(source = "codex_app_server").agentMark?.kind)
    }

    @Test fun traeHasItsMarkAndOthersTheirLetter() {
        assertEquals(AgentMarkKind.Trae, session("trae").agentMark?.kind)
        assertEquals(AgentMarkKind.Trae, session("trae-cli").agentMark?.kind)
        val traex = session("traex").agentMark!!
        assertNull(traex.kind)
        assertEquals("TRAEX", traex.name)
        assertEquals("DSH", session("dsh", label = "DSH").agentMark?.name)
    }

    @Test fun theClientComesFromAgentSurface() {
        assertEquals(AgentSurface.Cli, session("claude", "cli").agentMark?.surface)
        assertEquals(AgentSurface.Desktop, session("codex", "desktop").agentMark?.surface)
        assertNull(session("codex", "sdk").agentMark?.surface)
        assertNull(session("codex").agentMark?.surface)
    }

    @Test fun personasOwnConversationsHaveNone() {
        assertNull(session(assistant = true).agentMark)
    }

    @Test fun theMarkSaysTheAgentAndItsClient() {
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    JAgentMark(AgentMarkSpec(AgentMarkKind.Claude, "Claude Code", AgentSurface.Cli), Modifier.testTag("cli-$style"))
                    JAgentMark(AgentMarkSpec(AgentMarkKind.Codex, "Codex", AgentSurface.Desktop), Modifier.testTag("desktop-$style"))
                    JAgentMark(AgentMarkSpec(null, "amp", null), Modifier.testTag("letter-$style"))
                }
            }
        }
        // Each style draws one of each: the three styles say the same.
        for ((said, n) in listOf("Claude Code（命令行版）" to 3, "Codex（桌面版）" to 3, "amp" to 3)) {
            rule.onAllNodesWithContentDescription(said, useUnmergedTree = true).assertCountEquals(n)
        }
    }
}
