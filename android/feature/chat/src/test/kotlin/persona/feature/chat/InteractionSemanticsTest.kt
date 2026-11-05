package persona.feature.chat

import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.state.ToggleableState
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Interaction
import persona.core.contracts.Machine
import persona.core.contracts.Question
import persona.core.contracts.QuestionOption
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.domain.answer.AnswerDraft

/** The conversation's question card read by TalkBack (#4326): multi-select ticks, "n of total", the page, the status. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class InteractionSemanticsTest {
    @get:Rule val rule = createComposeRule()

    private val machine = Machine("box", name = "box")
    private val request = Interaction("q", machineId = "box", sessionId = "s1", kind = "question", questions = listOf(
        Question("要跑哪些测试？", multiSelect = true, options = listOf(QuestionOption("单元测试"), QuestionOption("集成测试"))),
        Question("用哪个框架？", options = listOf(QuestionOption("JUnit"), QuestionOption("Kotest"), QuestionOption("Spek"))),
    ))

    private fun show(busy: Boolean = false) = rule.setContent {
        PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
            var draft by remember { mutableStateOf(AnswerDraft(picks = mapOf("要跑哪些测试？" to setOf("单元测试")))) }
            Column {
                InteractionForm(request, machine, draft, { draft = it }, busy = busy, uncertain = false, full = false,
                    onAnswer = { _, _, _ -> }, onTerminal = null, onExpand = {})
            }
        }
    }

    private fun description(text: String) = SemanticsMatcher.expectValue(SemanticsProperties.ContentDescription, listOf(text))

    @Test fun multiSelectOptionsAreTickedCheckboxesWithTheirPosition() {
        show()
        rule.onNode(description("1. 单元测试，第 1 个，共 2 个"))
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Checkbox))
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.ToggleableState, ToggleableState.On))
        rule.onNode(description("2. 集成测试，第 2 个，共 2 个"))
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.ToggleableState, ToggleableState.Off))
        rule.onNode(description("第 1 题，共 2 题"), useUnmergedTree = true).assertExists()
    }

    @Test fun statusLineIsALiveRegion() {
        show(busy = true)
        rule.onNodeWithTag("request-status")
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.LiveRegion, LiveRegionMode.Polite))
    }
}
