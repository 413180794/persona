package persona.feature.files

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeLeft
import androidx.compose.ui.test.swipeUp
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * The table viewer drawn on the JVM with Robolectric's native graphics: a CSV and a two-sheet workbook (the second
 * sheet picked, a cell tapped), in every style, light and dark. Each is checked to draw something and written as a PNG
 * to `build/files-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h720dp-xhdpi")
class SheetShotsTest {
    @get:Rule val rule = createComposeRule()

    private val csv = Delimited.sheet("销量.csv", buildString {
        appendLine("地区,产品,数量,单价,备注")
        appendLine("华东,苹果,1200,3.5,\"含,逗号\"")
        appendLine("华南,香蕉,-80,2.25,退货")
        appendLine("华北,一个名字很长很长很长很长很长很长很长的产品,15%,\$4,")
        repeat(40) { appendLine("西部,梨 $it,${it * 37},1.${it % 10},") }
    })

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/files-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, bitmap.height - 2)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun drawsACsvAndAWorkbookInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var workbook by mutableStateOf(false)
        val sheets = Xlsx.read(SheetsTest.workbook())
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").size(400.dp, 720.dp).background(PersonaTheme.palette.background)) {
                    if (workbook) SheetGrid(sheets, truncated = false, PaddingValues(), header = {})
                    else SheetGrid(listOf(csv), truncated = true, PaddingValues(), header = { item { FileMeta(4_200_000, true) } })
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            workbook = false
            capture("sheet-csv-${s.name.lowercase()}-${if (d) "dark" else "light"}")
            workbook = true
            rule.waitForIdle()
            rule.onNodeWithText("名称").performClick()
            capture("sheet-xlsx-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }

    @Test fun aWideSheetScrollsBothWaysWithItsHeaderInPlace() {
        val wide = Delimited.sheet("wide.csv", buildString {
            appendLine((0 until 60).joinToString(",") { "列$it" })
            repeat(200) { r -> appendLine((0 until 60).joinToString(",") { c -> "r${r}c$c" }) }
        })
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                Box(Modifier.testTag("shot").size(400.dp, 720.dp).background(PersonaTheme.palette.background)) {
                    SheetGrid(listOf(wide), truncated = false, PaddingValues(), header = {})
                }
            }
        }
        // Only the columns in view are composed: the far ones aren't there before scrolling sideways.
        rule.onAllNodesWithText("列59").assertCountEquals(0)
        repeat(12) { rule.onNodeWithTag("sheet").performTouchInput { swipeLeft() } }
        repeat(3) { rule.onNodeWithTag("sheet").performTouchInput { swipeUp() } }
        rule.waitForIdle()
        // The file's header row is pinned; the last column came into view; early rows scrolled away.
        rule.onNodeWithText("列59").assertExists()
        rule.onAllNodesWithText("r0c59").assertCountEquals(0)
        capture("sheet-scrolled-glass-light")
    }
}
