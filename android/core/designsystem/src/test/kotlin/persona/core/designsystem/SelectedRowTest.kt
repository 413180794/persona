package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
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
 * The row a detail pane shows stays readable: its plate lies behind what the row draws, its edge on top. The plate
 * is an opaque surface since #6224, so drawn over the row it hid the title and avatar. Native rendering, 1 px per dp.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-mdpi")
class SelectedRowTest {
    @get:Rule val rule = createComposeRule()

    private val looks = AppStyle.entries.flatMap { style -> listOf(false, true).map { dark -> style to dark } }

    @Test fun aSelectedRowsContentShowsAboveItsPlateAndItsEdgeAboveBoth() {
        rule.setContent {
            Column {
                for ((style, dark) in looks) PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Box(Modifier.testTag("$style dark=$dark").size(120.dp, 40.dp).selectedRow(true)) {
                        Box(Modifier.fillMaxSize().background(Content))
                    }
                }
            }
        }
        rule.waitForIdle()
        for ((style, dark) in looks) {
            val tag = "$style dark=$dark"
            val bitmap = rule.onNodeWithTag(tag).captureToImage().asAndroidBitmap()
            assertEquals("$tag: the row's content shows", Content.toArgb(), bitmap.getPixel(60, 20))
            // The edge: 3 dp at the start (Brief's lies beside the row, outside this node), from 20 % to 80 % of its height.
            if (style != AppStyle.Brief) assertNotEquals("$tag: the edge is drawn over the content", Content.toArgb(), bitmap.getPixel(1, 20))
        }
    }

    private companion object {
        /** Not a colour of any style: a plate drawn over the row hides it. */
        val Content = Color(0xFFFF00FF)
    }
}
