package persona.feature.chat

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Item
import persona.core.designsystem.PersonaTheme

/**
 * Your bubble reads only its own entry of the chat's text updates: a reply streaming below it changes the map with every
 * chunk, and must not make every visible bubble parse its text again.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class UserBubbleUpdatesTest {
    @get:Rule val rule = createComposeRule()

    private val mine = Item("u1#0", 1, role = "user", kind = "text", text = "Example question")

    @Test fun anotherItemsUpdateLeavesTheBubblesTextsAlone() {
        val before = bubbleTexts(listOf(mine), emptyMap())
        val streaming = bubbleTexts(listOf(mine), mapOf("a1#0" to ChatTextUpdate("Example reply", streaming = true)))
        assertEquals(before, streaming)
        val own = bubbleTexts(listOf(mine), mapOf(mine.id to ChatTextUpdate("Example question, in full")))
        assertNotEquals(before, own)
        assertEquals(listOf("Example question, in full"), own)
    }

    @Test fun theBubbleFollowsItsOwnUpdateOnly() {
        var updates by mutableStateOf(emptyMap<String, ChatTextUpdate>())
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) { MergedUserBubble(listOf(mine), updates, menu = false) }
        }
        rule.onNodeWithText("Example question").assertExists()
        updates = mapOf("a1#0" to ChatTextUpdate("Example reply", streaming = true))
        rule.waitForIdle()
        rule.onNodeWithText("Example question").assertExists()
        updates = updates + (mine.id to ChatTextUpdate("Example question, in full"))
        rule.waitForIdle()
        rule.onNodeWithText("Example question, in full").assertExists()
    }
}
