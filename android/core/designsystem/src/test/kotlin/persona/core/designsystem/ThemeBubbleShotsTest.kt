package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/**
 * The user's own bubble under each theme color (#3048 review: the presets' bubbles as iOS has them), in every style, light
 * and dark. Written to `build/theme-bubble-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
class ThemeBubbleShotsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun drawsEveryPresetsBubble() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            Column(Modifier.testTag("shot").width(400.dp)) {
                (listOf<ThemeColor?>(null) + ThemeColor.presets).forEach { color ->
                    CompositionLocalProvider(LocalThemeAccent provides color?.preset) {
                        PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                            Column(Modifier.background(PersonaTheme.palette.background).padding(horizontal = Tokens.Space.l)) {
                                Text(color?.name ?: "Style", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                                UserBubble("帮我看一下 CI 为什么挂了", time = "10:32")
                            }
                        }
                    }
                }
            }
        }
        val out = File(System.getProperty("persona.shots") ?: "build/theme-bubble-shots").apply { mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out, "bubbles-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
