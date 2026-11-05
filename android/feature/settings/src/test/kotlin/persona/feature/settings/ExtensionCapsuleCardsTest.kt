package persona.feature.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.flowOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.ExtensionUiRoute

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1600dp-xhdpi")
class ExtensionCapsuleCardsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun rendersAnOptedInCardAndDispatchesItsActionAndPage() {
        var actions = 0
        var opened: ExtensionUiRoute? = null
        val api = object : ExtensionApiStub() {
            override val slotsAvailable = flowOf(true)
            override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?): List<ExtensionUiPlace> {
                assertEquals("capsule_card", placement)
                return listOf(ExtensionUiPlace("example-capsule", "task", "任务摘要", "capsule_card", opens = "home"),
                    ExtensionUiPlace("example-capsule", "overview", "只在设置显示", "overview_card"))
            }
            override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?): ExtensionView {
                assertEquals("compact", context?.size)
                assertEquals("android", context?.platform)
                return view("正在处理", "state-1")
            }
            override suspend fun uiAction(extension: String, ui: String, action: String, context: ExtensionUiContext?, state: String?,
                values: Map<String, String>?): ExtensionUiResult {
                assertEquals("example-capsule", extension)
                assertEquals("task", ui)
                assertEquals("continue", action)
                assertEquals("state-1", state)
                actions++
                return ExtensionUiResult(view = view("已继续", "state-2"))
            }
        }
        rule.setContent { PersonaTheme(reduceMotion = true) { Column { ExtensionCapsuleCards(api) { opened = it } } } }
        rule.waitForIdle()
        rule.onNodeWithText("正在处理").assertIsDisplayed()
        rule.onNodeWithText("只在设置显示").assertDoesNotExist()
        rule.onNodeWithText("继续任务").performClick()
        rule.waitForIdle()
        assertEquals(1, actions)
        rule.onNodeWithText("已继续").assertIsDisplayed()
        rule.onNodeWithText("打开完整页面").performClick()
        assertEquals("example-capsule", opened?.extensionId)
        assertEquals("home", opened?.uiId)
    }

    @Test fun asksInsideTheCardBeforeSendingAnAction() {
        var actions = 0
        val api = object : ExtensionApiStub() {
            override val slotsAvailable = flowOf(true)
            override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?) =
                listOf(ExtensionUiPlace("example-capsule", "task", "任务", "capsule_card"))
            override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?) =
                ExtensionView(sections = listOf(ExtensionViewSection(items = listOf(
                    ExtensionViewItem("button", title = "处理这件需要确认的任务", action = ExtensionViewAction("confirm", confirm = "确认处理这件事？")),
                ))))
            override suspend fun uiAction(extension: String, ui: String, action: String, context: ExtensionUiContext?, state: String?,
                values: Map<String, String>?): ExtensionUiResult {
                actions++
                return ExtensionUiResult(toast = "处理好了")
            }
        }
        rule.setContent { PersonaTheme(reduceMotion = true) { Column { ExtensionCapsuleCards(api) } } }
        rule.waitForIdle()
        rule.onNodeWithText("处理这件需要确认的任务").performClick()
        assertEquals(0, actions)
        rule.onNodeWithText("确认处理这件事？").assertIsDisplayed()
        rule.onNodeWithText("确定").performClick()
        rule.waitForIdle()
        assertEquals(1, actions)
        rule.onNodeWithText("处理好了").assertIsDisplayed()
    }

    @Test fun refusesOtherPlacementsAndMissingIdentities() {
        val card = ExtensionUiPlace("example-capsule", "task", "任务", "capsule_card")
        assertTrue(ExtensionCapsuleSlot.accepts(card))
        assertFalse(ExtensionCapsuleSlot.accepts(card.copy(placement = "overview_card")))
        assertFalse(ExtensionCapsuleSlot.accepts(card.copy(extension = "")))
        assertFalse(ExtensionCapsuleSlot.accepts(card.copy(id = "")))
    }

    private fun view(text: String, state: String) = ExtensionView(state = state, sections = listOf(ExtensionViewSection(items = listOf(
        ExtensionViewItem("text", text = text),
        ExtensionViewItem("button", title = "继续任务", action = ExtensionViewAction("continue")),
    ))))
}
