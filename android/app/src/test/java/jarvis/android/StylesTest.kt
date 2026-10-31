package jarvis.android

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.graphics.luminance
import jarvis.android.ui.AppStyle
import jarvis.android.ui.paletteOf
import jarvis.android.ui.schemeOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The five 界面风格 keep docs/ios.md「可读性底线」 in light and dark (iOS StyleSpecTests). */
class StylesTest {
    private fun ratio(a: Color, b: Color): Double {
        val x = a.luminance() + 0.05
        val y = b.luminance() + 0.05
        return maxOf(x, y) / minOf(x, y).toDouble()
    }

    private fun each(check: (AppStyle, Boolean) -> Unit) {
        for (s in AppStyle.entries) for (dark in listOf(false, true)) check(s, dark)
    }

    @Test fun whiteOnSolidAccent() = each { s, dark ->
        val p = paletteOf(s, dark)
        val r = ratio(Color.White, p.accent)
        // Below the floor on purpose, as on iOS (StyleSpecTests.known): 飞书's own dark blue #4C88FF is 3.35:1.
        val floor = if (s == AppStyle.LARK && dark) 3.3 else 3.5
        assertTrue("$s dark=$dark white on accent $r", r >= floor)
        assertEquals(Color.White, schemeOf(p).onPrimary)
    }

    @Test fun ownBubbleText() = each { s, dark ->
        val p = paletteOf(s, dark)
        val r = ratio(p.userBubbleText, p.userBubble)
        assertTrue("$s dark=$dark bubble text $r", r >= 4.5)
    }

    @Test fun accentAsLinkOnPage() = each { s, dark ->
        val p = paletteOf(s, dark)
        val r = ratio(p.accent, p.page)
        assertTrue("$s dark=$dark link on page $r", r >= 3.0)
        val rc = ratio(p.accent, p.card)
        assertTrue("$s dark=$dark link on card $rc", rc >= 3.0)
    }

    @Test fun textOnEverySurface() = each { s, dark ->
        val c = schemeOf(paletteOf(s, dark))
        for ((name, bg, fg) in listOf(
            Triple("background", c.background, c.onBackground),
            Triple("card", c.surfaceContainerHighest, c.onSurface),
            Triple("secondaryContainer", c.secondaryContainer, c.onSecondaryContainer),
            Triple("errorContainer", c.errorContainer, c.onErrorContainer),
        )) {
            val r = ratio(fg, bg)
            assertTrue("$s dark=$dark $name $r", r >= 4.5)
        }
        for (bg in listOf(c.background, c.surfaceContainerHighest, c.secondaryContainer, c.surfaceVariant)) {
            val sub = ratio(c.onSurfaceVariant, bg)
            assertTrue("$s dark=$dark secondary text on $bg $sub", sub >= 4.5)
        }
        // Your own bubble's labels (「工作中输入」) are its text color at 80%.
        val q = ratio(c.onPrimaryContainer.copy(alpha = 0.8f).compositeOver(c.primaryContainer), c.primaryContainer)
        assertTrue("$s dark=$dark bubble label $q", q >= 3.0)
    }

    @Test fun unknownIdIsGlass() {
        assertEquals(AppStyle.GLASS, AppStyle.of(null))
        assertEquals(AppStyle.GLASS, AppStyle.of("nope"))
        assertEquals(AppStyle.LARK, AppStyle.of("lark"))
    }
}
