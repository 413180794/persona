package persona.feature.settings

import androidx.activity.ComponentActivity
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.*
import java.io.File

/**
 * Settings › Language in each of the app's languages, every style, light and dark (docs/protocol/i18n.md). Written as PNGs to
 * `build/i18n-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class LanguageShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", title = "语言", system = "跟随系统")

    @Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
    @Test fun english() = shots("en", title = "Language", system = "Match system")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h800dp-xhdpi")
    @Test fun japanese() = shots("ja", title = "言語", system = "システムと同じ")

    private fun shots(language: String, title: String, system: String) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalLanguageHost provides rule.activity) {
                    Box(Modifier.testTag("shot").width(400.dp).height(760.dp).background(PersonaTheme.palette.background)) {
                        LanguagePage(onBack = {}, footer = {})
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            rule.onNodeWithText(title).assertExists()
            rule.onNodeWithText(system).assertExists()
            // Each language is listed in its own name whatever the app's language is.
            listOf("简体中文", "English", "日本語").forEach { rule.onNodeWithText(it).assertExists() }
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val name = "language-$language-${s.name.lowercase()}-${if (d) "dark" else "light"}.png"
            File(out(), name).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
