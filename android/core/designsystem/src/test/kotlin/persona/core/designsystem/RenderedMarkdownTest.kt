package persona.core.designsystem

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.MarkdownRenderer.Kind
import java.io.File

/**
 * Fences drawn as pictures and their fallbacks, in light and dark (written to `build/render-shots/`). Robolectric's
 * WebView draws nothing, so the pictures are put in the renderer's cache as if drawn: this checks what the app does
 * with a picture (a formula tinted in the text's colour) and with KaTeX's or Mermaid's error (the code, then why).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class RenderedMarkdownTest {
    @get:Rule val rule = createComposeRule()

    /** A stand-in for KaTeX's picture: black text on transparent, as the page draws a formula. */
    private fun formula(): Bitmap = Bitmap.createBitmap(220, 60, Bitmap.Config.ARGB_8888).also {
        Canvas(it).drawText("x² + y² = r²", 4f, 44f, Paint(Paint.ANTI_ALIAS_FLAG).apply { textSize = 40f })
    }

    @Composable
    private fun Fences(dark: Boolean) {
        val density = LocalDensity.current
        val body = PersonaTheme.type.body
        val caption = PersonaTheme.type.caption
        // Seed the renderer's cache in an effect, not during composition, and draw the fences once it is seeded.
        var seeded by remember { mutableStateOf(false) }
        LaunchedEffect(Unit) {
            val points = with(density) { body.fontSize.toPx() / this.density }
            val small = with(density) { caption.fontSize.toPx() / this.density }
            MarkdownRenderer.put(Kind.DisplayMath, "x^2 + y^2 = r^2", dark, points, density.density, formula())
            MarkdownRenderer.put(Kind.DisplayMath, "\\frac{1}{", dark, points, density.density, null, "Expected '}', got 'EOF' at end of input")
            MarkdownRenderer.put(Kind.Mermaid, "flowchart TD\n  A -->", dark, small, density.density, null, "Parse error on line 2")
            seeded = true
        }
        if (!seeded) return
        Column(Modifier.testTag("fences dark=$dark").width(360.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l),
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            MarkdownFence("```math\nx^2 + y^2 = r^2\n```") { JCodeBlock(it.code, small = true) }
            MarkdownFence("```math\n\\frac{1}{\n```") { JCodeBlock(it.code, small = true) }
            MarkdownFence("```mermaid\nflowchart TD\n  A -->\n```") { JCodeBlock(it.code, small = true) }
            MarkdownFence("```swift\nlet x = 1\n```") { JCodeBlock(it.code, small = true) }
        }
    }

    @Test fun picturesAndFallbacks() {
        rule.setContent {
            Column {
                for (dark in listOf(false, true)) PersonaTheme(darkTheme = dark, reduceMotion = true) { Fences(dark) }
            }
        }
        rule.waitForIdle()
        rule.onAllNodesWithText("没能画出来：Expected '}', got 'EOF' at end of input", useUnmergedTree = true).assertCountEquals(2)
        rule.onAllNodesWithText("没能画出来：Parse error on line 2", useUnmergedTree = true).assertCountEquals(2)
        rule.onAllNodesWithText("let x = 1", useUnmergedTree = true).assertCountEquals(2)
        val out = File("build/render-shots").apply { mkdirs() }
        for (dark in listOf(false, true)) {
            val shot = rule.onNodeWithTag("fences dark=$dark").captureToImage().asAndroidBitmap()
            out.resolve("fences-${if (dark) "dark" else "light"}.png").outputStream().use { shot.compress(Bitmap.CompressFormat.PNG, 100, it) }
            // The formula takes the text's colour: dark strokes on the light page, light ones on the dark page.
            val formula = rule.onAllNodesWithTag("rendered-math", useUnmergedTree = true)[if (dark) 1 else 0].captureToImage().asAndroidBitmap()
            val lum = (0 until formula.width).flatMap { x -> (0 until formula.height).map { y -> formula.getPixel(x, y) } }
                .map { androidx.compose.ui.graphics.Color(it).luminance() }
            if (dark) assertTrue("light strokes on the dark page: ${lum.max()}", lum.max() > 0.6f)
            else assertTrue("dark strokes on the light page: ${lum.min()}", lum.min() < 0.3f)
        }
    }
}
