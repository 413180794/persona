package persona.platform.capsule

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.heightIn
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.assertHeightIsEqualTo
import androidx.compose.ui.test.assertWidthIsEqualTo
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.PersonaTheme

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w411dp-h900dp-xhdpi")
class CapsuleSurfaceTest {
    @get:Rule val rule = createComposeRule()
    private val sections = listOf(CapsuleSection("unread", "未读", 3),
        CapsuleSection("main", "主对话", list = false), CapsuleSection("inbox", "收件箱", 6))
    private val unread = CapsuleEntry("agent", "A very long task title", "A long preview that belongs in the list", CapsuleState.Done)
    private val main = CapsuleEntry("main-chat", "Persona", "", CapsuleState.Idle)
    private val inbox = CapsuleEntry("inbox-item", "A request in the inbox", "Open its original detail", CapsuleState.NeedsYou)

    @Test fun collapsedCapsuleKeepsItsSizeAndHidesAllTaskTextAndNumbers() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CapsuleSurface(CapsuleContent(entries = listOf(unread), selectedId = unread.id,
                    sections = sections, sectionId = "unread", badgeCount = 15, hasActivity = true), false,
                    {}, {}, {}, {}, {}, { _, _ -> }, { _, _ -> },
                    extraContent = { Text("Extension content") })
            }
        }
        rule.onNodeWithTag("capsule.orb").assertWidthIsEqualTo(64.dp).assertHeightIsEqualTo(40.dp)
        rule.onNodeWithTag("capsule.badge", useUnmergedTree = true).assertExists()
        rule.onNodeWithText(unread.title).assertDoesNotExist()
        rule.onNodeWithText(unread.summary).assertDoesNotExist()
        rule.onNodeWithText("15").assertDoesNotExist()
        rule.onNodeWithText("3/15").assertDoesNotExist()
        rule.onNodeWithText("Extension content").assertDoesNotExist()
    }

    @Test fun staleInboxSelectionShowsTheProviderErrorInsideTheList() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CapsuleSurface(CapsuleContent(entries = listOf(inbox), sections = sections, sectionId = "inbox"), true,
                    {}, {}, { throw IllegalStateException("事项已不在当前分组") }, {}, {}, { _, _ -> }, { _, _ -> })
            }
        }
        rule.onNodeWithTag("capsule.entry.inbox-item").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("事项已不在当前分组").assertExists()
        rule.onNodeWithTag("capsule.list").assertExists()
    }

    @Test fun fixedSectionsSwitchBetweenAListAndChatWithoutLeakingComposerOrExtensionContent() {
        var content by mutableStateOf(CapsuleContent(entries = listOf(unread), sections = sections, sectionId = "unread"))
        var opened: String? = null
        var sent: Pair<String?, String>? = null
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Box(Modifier.heightIn(max = 420.dp)) {
                    CapsuleSurface(content, true, {}, {}, onSelect = { id ->
                        if (content.sectionId == "inbox") opened = id
                        else content = content.copy(selectedId = id, messages = if (id != null)
                            listOf(CapsuleMessage("reply", "The actual reply", false)) else emptyList())
                    }, onSelectSection = { id ->
                        content = content.copy(sectionId = id, selectedId = if (id == "main") main.id else null,
                            entries = when (id) { "main" -> listOf(main); "inbox" -> listOf(inbox); else -> listOf(unread) }, messages = emptyList())
                    }, onOpen = { opened = it }, onSend = { id, text -> sent = id to text }, onAction = { _, _ -> },
                        extraContent = { Text("Extension content", Modifier.testTag("extension")) })
                }
            }
        }
        rule.onNodeWithTag("capsule.card").assertWidthIsEqualTo(300.dp)
        rule.onNodeWithTag("capsule.section.unread").assertExists()
        rule.onNodeWithText("未读 3").assertExists()
        rule.onNodeWithText("主对话").assertExists()
        rule.onNodeWithText("收件箱 6").assertExists()
        rule.onNodeWithTag("capsule.input").assertDoesNotExist()
        rule.onNodeWithTag("extension").assertDoesNotExist()
        rule.onNodeWithTag("capsule.entry.agent").performClick()
        rule.onNodeWithText("The actual reply").assertExists()
        rule.onNodeWithTag("capsule.back").assertExists().performClick()
        rule.onNodeWithTag("capsule.list").assertExists()
        rule.onNodeWithTag("capsule.input").assertDoesNotExist()
        rule.onNodeWithTag("capsule.section.main").performClick()
        rule.onNodeWithTag("capsule.back").assertDoesNotExist()
        rule.onNodeWithTag("extension").assertExists()
        rule.onNode(hasSetTextAction()).performTextInput("Continue the main conversation")
        rule.onNodeWithTag("capsule.send").performClick()
        rule.waitForIdle()
        assertEquals(main.id to "Continue the main conversation", sent)
        rule.onNodeWithTag("capsule.section.inbox").performClick()
        rule.onNodeWithTag("capsule.input").assertDoesNotExist()
        rule.onNodeWithTag("extension").assertDoesNotExist()
        rule.onNodeWithTag("capsule.entry.inbox-item").performClick()
        assertEquals(inbox.id, opened)
        assertTrue(content.listing)
    }
}
