package persona.feature.agents

import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.NewTodoRequests

/** The launcher's "New to-do" opens the list's sheet each time it is used, not only the first time (#5718). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class NewTodoSheetTest {
    @get:Rule val rule = createComposeRule()

    private var now = 1_000L
    private val requests = NewTodoRequests { now }
    private val sheets get() = rule.onAllNodes(isDialog()).fetchSemanticsNodes().size

    @Test fun eachRequestOpensTheSheet() {
        requests.request()
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                TodosScreen(emptyList(), emptyList(), null, onBack = {}, onOpenTodo = {}, newTodoRequests = requests)
            }
        }
        rule.waitForIdle()
        assertEquals("first request", 1, sheets)
        assertNull(requests.requests.value)
        rule.onAllNodes(hasSetTextAction() and hasAnyAncestor(isDialog())).onFirst().performTextInput("Example")
        rule.onNodeWithText(RuntimeEnvironment.getApplication().getString(R.string.agents_todo_create)).performClick()
        rule.waitForIdle()
        assertEquals("created", 0, sheets)
        // Used again with the list still open underneath.
        now += 60_000
        requests.request()
        rule.waitForIdle()
        assertEquals("second request", 1, sheets)
    }
}
