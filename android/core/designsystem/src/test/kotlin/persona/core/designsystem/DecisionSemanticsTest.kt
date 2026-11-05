package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.Text
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.state.ToggleableState
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertContentDescriptionEquals
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/**
 * A "needs you" card read by TalkBack (#4326): a multi-select option is a checkbox with its tick, a single
 * choice a radio button; each option says which of how many it is; the dock's headline is a heading and its
 * "1 / 2" reads as "request 1 of 2".
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class DecisionSemanticsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun multiSelectOptionIsACheckboxWithItsTick() {
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    DecisionOption("单元测试", {}, Modifier.testTag("on $style"), selected = true, multiSelect = true, position = 1 to 3)
                    DecisionOption("集成测试", {}, Modifier.testTag("off $style"), multiSelect = true, position = 2 to 3)
                }
            }
        }
        for (style in AppStyle.entries) {
            rule.onNodeWithTag("on $style")
                .assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Checkbox))
                .assert(SemanticsMatcher.expectValue(SemanticsProperties.ToggleableState, ToggleableState.On))
                .assert(SemanticsMatcher.keyNotDefined(SemanticsProperties.Selected))
            rule.onNodeWithTag("off $style")
                .assert(SemanticsMatcher.expectValue(SemanticsProperties.ToggleableState, ToggleableState.Off))
        }
    }

    @Test fun singleChoiceSaysWhichOfHowMany() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Column {
                    DecisionOption("1. Swift Testing", {}, Modifier.testTag("first"), detail = "新代码统一用", recommended = true,
                        selected = true, position = 1 to 2)
                    DecisionOption("2. XCTest", {}, Modifier.testTag("second"), position = 2 to 2)
                }
            }
        }
        rule.onNodeWithTag("first")
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.RadioButton)).assertIsSelected()
            .assertContentDescriptionEquals("1. Swift Testing，新代码统一用，建议，第 1 个，共 2 个")
        rule.onNodeWithTag("second").assertContentDescriptionEquals("2. XCTest，第 2 个，共 2 个")
            .assert(SemanticsMatcher.keyNotDefined(SemanticsProperties.ToggleableState))
    }

    @Test fun dockHeadlineIsAHeadingAndItsPositionIsSpoken() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                DecisionDock(DecisionKind.Command, "要运行 npm test", position = 1 to 2, onList = {}) { Text("正文") }
            }
        }
        rule.onNodeWithText("要运行 npm test").assert(SemanticsMatcher.keyIsDefined(SemanticsProperties.Heading))
        rule.onNodeWithText("1 / 2", useUnmergedTree = true).assertExists()
        rule.onNode(SemanticsMatcher.expectValue(SemanticsProperties.ContentDescription, listOf("第 1 件，共 2 件"))).assertExists()
    }
}
