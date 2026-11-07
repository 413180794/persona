package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.pagePadding
import java.io.File

/**
 * Settings › Translation drawn on the JVM with Robolectric's native graphics (no emulator), in every style, light and dark: off,
 * on with two chats picked, and the chat picker. Each is checked to draw something and written as a PNG to
 * `build/translation-shots/` (or `-Dpersona.shots=<dir>`) to look at (docs/android/android.md "Translation"). The interaction tests
 * drive the page as a user would and check what is kept.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
class TranslationSettingsShotsTest {
    @get:Rule val rule = createComposeRule()

    private class FakeTranslation(start: TranslationPrefs) : TranslationSettings {
        val state = MutableStateFlow(start)
        override val prefs: StateFlow<TranslationPrefs> = state
        override fun update(transform: (TranslationPrefs) -> TranslationPrefs) { state.value = transform(state.value) }
    }

    private val picked = listOf(TranslateChat("f2", "设计评审"), TranslateChat("f9", "Global Partners"))

    private val choices = listOf(
        LarkChatChoice("f2", "设计评审"), LarkChatChoice("f3", "产品周会"),
        LarkChatChoice("f4", "示例用户", isGroup = false), LarkChatChoice("f5", "Tokyo Office"),
    )

    private val connectors = object : ConnectorsApiStub() {
        override suspend fun larkChatChoices() = choices
    }

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/translation-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private enum class Shot { Off, On, Picker }

    @Test fun drawsTheTranslationPageInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var shot by mutableStateOf(Shot.Off)
        var run by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                key(run) {
                    val translation = FakeTranslation(
                        if (shot == Shot.Off) TranslationPrefs() else TranslationPrefs(enabled = true, target = "zh-Hans", chats = picked),
                    )
                    Box(Modifier.testTag("shot").width(400.dp).height(1400.dp).background(PersonaTheme.palette.background)) {
                        // The picker is a bottom sheet, a window Robolectric does not capture: its content is drawn on
                        // the page's background as it is laid out in the sheet.
                        if (shot == Shot.Picker) Box(Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.l)) {
                            TranslationChatPickerContent(picked, choices, error = null, query = "", onQuery = {},
                                picked = picked.take(1)) {}
                        } else TranslationSettingsPage(translation, models = null, connectors = connectors, onBack = {}, onModel = {})
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            val suffix = "${s.name.lowercase()}-${if (d) "dark" else "light"}"
            for (next in Shot.entries) {
                shot = next; run++
                capture("translate-${next.name.lowercase()}-$suffix")
            }
        }
    }

    @Test fun turningItOnShowsTheScopeAndChoosingALanguageKeepsIt() {
        val translation = FakeTranslation(TranslationPrefs())
        rule.setContent { PersonaTheme(darkTheme = false, reduceMotion = true) {
            TranslationSettingsPage(translation, models = null, connectors = connectors, onBack = {}, onModel = {})
        } }
        assertEquals(0, rule.onAllNodesWithText("翻译范围").fetchSemanticsNodes().size)
        rule.onNodeWithTag("translate-enabled").performClick()
        assertTrue(translation.state.value.enabled)
        rule.onNodeWithText("翻译范围").assertIsDisplayed()
        rule.onNodeWithText("不翻译").assertIsDisplayed() // no chats by default
        rule.onNode(hasText("目标语言") and hasClickAction()).performClick()
        rule.onNodeWithText("English").performClick()
        assertEquals("en", translation.state.value.target)
        rule.onNodeWithTag("translate-mail").performClick()
        assertFalse(translation.state.value.mail)
    }

    @Test fun pickingChatsKeepsTheirNamesAndRemovingOneDropsIt() {
        val translation = FakeTranslation(TranslationPrefs(enabled = true, chats = listOf(TranslateChat("old", "已经不在最近里的会话"))))
        rule.setContent { PersonaTheme(darkTheme = false, reduceMotion = true) {
            TranslationSettingsPage(translation, models = null, connectors = connectors, onBack = {}, onModel = {})
        } }
        rule.onNode(hasText("飞书会话") and hasClickAction()).performClick()
        rule.waitForIdle()
        // The picked chat is offered first even though the hub no longer lists it, so it can still be unpicked.
        rule.onAllNodesWithText("已经不在最近里的会话")[1].assertIsDisplayed()
        rule.onNodeWithText("产品周会").performClick()
        rule.onNodeWithTag("translate-chats-done").performClick()
        assertEquals(listOf("old", "f3"), translation.state.value.chats.map { it.id })
        assertEquals("产品周会", translation.state.value.chats[1].name)
        rule.onNodeWithText("2 个会话").assertIsDisplayed()
        rule.onNode(hasContentDescription("不再翻译「已经不在最近里的会话」")).performClick()
        assertEquals(listOf("f3"), translation.state.value.chats.map { it.id })
    }

    @Test fun pickerRowsPutThePickedFirstAndFilterByName() {
        val rows = TranslationText.pickerRows(
            listOf(TranslateChat("x", "旧会话")),
            listOf(LarkChatChoice("a", "Tokyo Office"), LarkChatChoice("x", "改名后的会话"), LarkChatChoice("b", "产品周会")),
            "",
        )
        assertEquals(listOf("x", "a", "b"), rows.map { it.id })
        assertEquals("旧会话", rows[0].name)
        assertEquals(listOf("a"), TranslationText.pickerRows(emptyList(), listOf(LarkChatChoice("a", "Tokyo Office"), LarkChatChoice("b", "产品周会")), " tokyo ").map { it.id })
        assertEquals("不翻译", TranslationText.chats(androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>().resources, 0))
        assertEquals("3 个会话", TranslationText.chats(androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>().resources, 3))
    }
}
