package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * Controls that turn enabled after being disabled keep what they draw behind their content. They used to lose it:
 * Modifier.alpha(1f) drops its layer, and with the layer gone from behind clip + background Compose stopped drawing
 * the fill (a JButton showed only its label). Rendered with Robolectric's native graphics, one density pixel per dp.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-mdpi")
class EnabledStateTest {
    @get:Rule val rule = createComposeRule()

    @Test fun buttonKeepsItsFillWhenItTurnsEnabled() {
        var enabled by mutableStateOf(false)
        val fills = mutableMapOf<String, Color>()
        rule.setContent {
            Column {
                for (style in AppStyle.entries) for (dark in listOf(false, true)) PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    val tag = "$style dark=$dark"
                    fills[tag] = PersonaTheme.palette.action
                    Box(Modifier.background(PersonaTheme.palette.background)) {
                        JButton("下一步", {}, Modifier.testTag(tag).width(160.dp), enabled = enabled)
                    }
                }
            }
        }
        rule.waitForIdle()
        fills.forEach { (tag, fill) -> assertEquals("disabled $tag", hex(fill), hex(pixel(tag, 10))) }
        enabled = true
        rule.waitForIdle()
        fills.forEach { (tag, fill) -> assertEquals("enabled again $tag", hex(fill), hex(pixel(tag, 10))) }
        enabled = false
        rule.waitForIdle()
        enabled = true
        rule.waitForIdle()
        fills.forEach { (tag, fill) -> assertEquals("enabled a second time $tag", hex(fill), hex(pixel(tag, 10))) }
    }

    /** Fields, switches and decision options draw their frame behind the dimmed content in the same way. */
    @Test fun controlsKeepTheirFrameWhenTheyTurnEnabled() {
        var enabled by mutableStateOf(false)
        // Where each control's frame shows, clear of its content: x in dp from its left edge.
        val probes = mutableMapOf<String, Int>()
        rule.setContent {
            Column(Modifier.background(Backdrop)) {
                for (style in AppStyle.entries) PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    JTextField("hub", {}, Modifier.testTag("field $style").width(200.dp), enabled = enabled, singleLine = true)
                    probes["field $style"] = 8
                    JSwitch(false, {}, Modifier.testTag("switch $style"), enabled = enabled)
                    probes["switch $style"] = 40
                    DecisionOption("允许", {}, Modifier.testTag("option $style").width(200.dp), recommended = true, enabled = enabled)
                    probes["option $style"] = 8
                }
            }
        }
        rule.waitForIdle()
        val before = probes.mapValues { (tag, x) -> pixel(tag, x) }
        before.forEach { (tag, color) -> assertNotEquals("frame drawn while disabled: $tag", hex(Backdrop), hex(color)) }
        enabled = true
        rule.waitForIdle()
        probes.forEach { (tag, x) -> assertEquals("frame kept once enabled: $tag", hex(before.getValue(tag)), hex(pixel(tag, x))) }
    }

    private fun pixel(tag: String, x: Int): Color {
        val bitmap = rule.onNodeWithTag(tag).captureToImage().asAndroidBitmap()
        return Color(bitmap.getPixel(x, bitmap.height / 2))
    }

    private fun hex(color: Color) = String.format("#%08X", color.toArgb())

    private companion object {
        /** Not a colour of any style: a frame that is not drawn shows it. */
        val Backdrop = Color(0xFFFF00FF)
    }
}
