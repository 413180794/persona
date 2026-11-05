package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.state.ToggleableState
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/**
 * TalkBack reads a switch's on / off and a checkbox's ticked / not ticked from its toggleable state, not from
 * "selected"; a switch row is read as a switch, not a button (#4121).
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class SwitchSemanticsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun switchRowSaysOnOrOffAndIsASwitch() {
        var on by mutableStateOf(false)
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    JSwitchRow("自动运行", on, { on = it }, Modifier.testTag("$style"))
                }
            }
        }
        for (style in AppStyle.entries) rule.onNodeWithTag("$style")
            .assertIsOff().assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Switch))
        rule.onNodeWithTag("${AppStyle.Glass}").performClick()
        for (style in AppStyle.entries) rule.onNodeWithTag("$style").assertIsOn()
    }

    @Test fun switchAndCheckboxHaveAToggleableState() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Column {
                    JSwitch(true, {}, Modifier.testTag("switch"))
                    JCheckbox(false, {}, Modifier.testTag("checkbox"))
                }
            }
        }
        rule.onNodeWithTag("switch").assertIsOn()
            .assert(SemanticsMatcher.keyNotDefined(SemanticsProperties.Selected))
        rule.onNodeWithTag("checkbox")
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.ToggleableState, ToggleableState.Off))
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Checkbox))
            .assert(SemanticsMatcher.keyNotDefined(SemanticsProperties.Selected))
    }

    /** A content row that toggles a switch (Claude Code's bool settings) passes its role and reads the switch's state. */
    @Test fun contentRowWithASwitchIsASwitch() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                JRow(Modifier.testTag("row"), trailing = { JSwitch(true, null) }, onClick = {}, chevron = false, role = Role.Switch) {
                    androidx.compose.material3.Text("自动运行")
                }
            }
        }
        rule.onNodeWithTag("row").assertIsOn().assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Switch))
    }
}
