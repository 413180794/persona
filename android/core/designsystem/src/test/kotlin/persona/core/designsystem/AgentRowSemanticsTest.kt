package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/**
 * A compact session row has no state line and its mark is colour only, so TalkBack hears the state from the row's
 * state description (#3902); a detailed row already says it in its text and does not say it twice.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class AgentRowSemanticsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun compactRowSaysItsState() {
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    AgentRow("补齐网络层的单元测试", Status.NeedsYou, onClick = {}, Modifier.testTag("$style"), time = "2 分钟前",
                        statusText = "需要你", compact = true)
                }
            }
        }
        for (style in AppStyle.entries) rule.onNodeWithTag("$style")
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "需要你"))
    }

    @Test fun detailedRowSaysItInItsText() {
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    AgentRow("补齐网络层的单元测试", Status.NeedsYou, onClick = {}, Modifier.testTag("$style"), statusText = "需要你")
                }
            }
        }
        for (style in AppStyle.entries) rule.onNodeWithTag("$style")
            .assert(SemanticsMatcher.keyNotDefined(SemanticsProperties.StateDescription))
    }

    /** A compact tab row says its lead agent's state; its fold toggle keeps only its own. */
    @Test fun compactTabRowSaysItsLeadsState() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                AgentTabRow("#2", "补齐网络层的单元测试", Status.NeedsYou, agents = 3, folded = true, onToggle = {}, onClick = {},
                    Modifier.testTag("tab"), statusText = "需要你", compact = true)
            }
        }
        rule.onNodeWithTag("tab").assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "需要你"))
    }
}
