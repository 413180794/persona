package persona.feature.terminal

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

/**
 * The terminal's chrome (title, subtitle, read-only chip, key bar, the notice above it, "Display" sheet) in each of the
 * app's languages, light and dark (docs/protocol/i18n.md). Written as PNGs to `build/i18n-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class TerminalShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", polling = "每秒读一次屏幕", offline = "连不上 hub，显示的是上次画面", settings = "显示设置", wrap = "自动换行")

    @Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
    @Test fun english() = shots("en", polling = "Updates every second", offline = "Can't reach the hub; showing the last screen",
        settings = "Display settings", wrap = "Wrap lines")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h800dp-xhdpi")
    @Test fun japanese() = shots("ja", polling = "画面を毎秒読み込み中", offline = "ハブに接続できません。前回の画面を表示しています",
        settings = "表示設定", wrap = "自動折り返し")

    /** A node that only has the screen read (`GET …/screen`): watched, read once a second. */
    private class Screen : TerminalTransport {
        override val connection = MutableStateFlow(ConnectionState.Online)
        override val features = MutableStateFlow(emptySet<String>())
        override val events = MutableSharedFlow<ServerEvent>()
        override fun subscribe(key: SessionKey, epoch: String?, after: Int, size: TerminalSize?, takeover: Boolean) = false
        override fun unsubscribe(key: SessionKey) = Unit
        override fun input(key: SessionKey, text: String, size: TerminalSize?, scroll: Int, at: TerminalCellPosition?, click: TerminalCellPosition?) = false
        override suspend fun screen(key: SessionKey) = TerminalScreen("$ npm test\n\n> app@1.0.0 test\n> vitest run\n\n ✓ src/app.test.ts (12 tests) 48ms\n\n Test Files  1 passed (1)\n      Tests  12 passed (12)\n$ ")
        override suspend fun keys(key: SessionKey, keys: List<String>, text: String) = Unit
    }

    private fun shots(language: String, polling: String, offline: String, settings: String, wrap: String) {
        val transport = Screen()
        val model = TerminalViewModel(SessionKey("lab", "s1"), transport)
        var dark by mutableStateOf(false)
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.testTag("shot").width(400.dp).height(800.dp).background(PersonaTheme.palette.background)) {
                    TerminalScreen(model, onBack = {}, sessionTitle = "Fix the login flow")
                }
            }
        }
        fun settle() { repeat(8) { rule.mainClock.advanceTimeBy(250); Thread.sleep(20) }; rule.mainClock.advanceTimeByFrame() }
        fun shot(name: String) {
            val bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
            File(out(), "terminal-$name-$language-glass-${if (dark) "dark" else "light"}.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        // The first opening of the "Display" sheet stays below the window under Robolectric: a warm-up round, not saved.
        for ((round, d) in listOf(false, false, true).withIndex()) {
            dark = d
            transport.connection.value = ConnectionState.Online
            settle()
            rule.onNodeWithText(polling).assertExists()
            if (round > 0) shot("screen")
            transport.connection.value = ConnectionState.Connecting
            settle()
            rule.onNodeWithText(offline).assertExists()
            if (round > 0) shot("offline")
            transport.connection.value = ConnectionState.Online
            settle()
            rule.onNodeWithContentDescription(settings).performClick()
            settle()
            rule.onNodeWithText(wrap).assertExists()
            if (round > 0) shot("display")
            rule.activity.onBackPressedDispatcher.onBackPressed()
            settle()
        }
    }
}
