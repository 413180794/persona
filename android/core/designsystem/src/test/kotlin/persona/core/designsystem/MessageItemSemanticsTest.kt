package persona.core.designsystem

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.performMouseInput
import androidx.compose.ui.test.rightClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/**
 * The inbox item a detail pane shows is marked selected, and a right click opens its menu, in every style: the glass
 * "needs you" card is a card of its own and once had neither (#5722).
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class MessageItemSemanticsTest {
    @get:Rule val rule = createComposeRule()

    private fun item(tag: String) = rule.onNode(hasClickAction() and hasAnyAncestor(hasTestTag(tag)))

    @Test fun selectedItemSaysItIsSelected() {
        rule.setContent {
            Column {
                for (style in AppStyle.entries) for (status in listOf(Status.NeedsYou, null)) {
                    PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                        Box(Modifier.testTag("$style.$status")) {
                            MessageItem("示例同事", "下周的评审改到周三", onClick = {}, status = status, onLongClick = {}, selected = true)
                        }
                    }
                }
            }
        }
        for (style in AppStyle.entries) for (status in listOf(Status.NeedsYou, null)) {
            item("$style.$status").assert(SemanticsMatcher.expectValue(SemanticsProperties.Selected, true))
        }
    }

    @OptIn(ExperimentalTestApi::class)
    @Test fun rightClickOpensTheMenu() {
        val opened = mutableListOf<AppStyle>()
        rule.setContent {
            Column {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    Box(Modifier.testTag("$style")) {
                        MessageItem("示例同事", "下周的评审改到周三", onClick = {}, status = Status.NeedsYou, onLongClick = { opened += style })
                    }
                }
            }
        }
        for (style in AppStyle.entries) item("$style").performMouseInput { rightClick() }
        assertEquals(AppStyle.entries, opened)
    }
}
