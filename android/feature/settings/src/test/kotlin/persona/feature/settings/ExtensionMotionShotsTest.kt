package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

/**
 * An extension's page drawn again with new values (#2811): the numbers count to the new ones, a badge goes over to its
 * new tone, a row whose key is new comes in and one that went goes, the bars grow to their new heights. Every style,
 * light and dark, before, halfway and after, written to `build/extension-motion-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
class ExtensionMotionShotsTest {
    @get:Rule val rule = createComposeRule()

    private val days = listOf("周一", "周二", "周三", "周四", "周五")
    private fun chart(values: List<Double>) = ExtensionViewItem("chart", key = "chart", kind = "bar", unit = "M",
        series = listOf(ExtensionChartSeries("tokens", null, days.zip(values) { d, v -> ExtensionChartPoint(d, v, "%.1fM".format(v)) })))

    private fun view(after: Boolean) = ExtensionView(title = "用量", sections = listOf(
        ExtensionViewSection(items = listOf(
            ExtensionViewItem("metric", key = "today", label = "今天", value = if (after) "8.4M" else "1.2M", detail = "tokens"),
            ExtensionViewItem("stat_grid", key = "stats", stats = listOf(ExtensionStat("任务", if (after) "6/8" else "2/8"),
                ExtensionStat("费用", if (after) "¥36.20" else "¥1.20"), ExtensionStat("请求", if (after) "1,480" else "312"))),
            chart(if (after) listOf(3.0, 4.5, 2.0, 5.5, 4.0) else listOf(1.0, 1.5, 3.0, 0.5, 2.0)),
        )),
        ExtensionViewSection(title = "任务", items = listOfNotNull(
            ExtensionViewItem("row", key = "a", title = "迁移数据库", value = if (after) "3/4" else "1/4",
                badge = if (after) ExtensionViewBadge("等你", "caution") else ExtensionViewBadge("在做", "positive")),
            ExtensionViewItem("row", key = "b", title = "写周报", value = "已完成", badge = ExtensionViewBadge("完成", "neutral")),
            if (after) null else ExtensionViewItem("row", key = "c", title = "清理旧分支", value = "0/2"),
            if (after) ExtensionViewItem("row", key = "d", title = "评审 PR", value = "1/3", badge = ExtensionViewBadge("新", "positive")) else null,
        )),
    ))

    private val handle = ExtensionViewHandle({ _, _ -> }, { _, _ -> }, {}, {})

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/extension-motion-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out(), "$name.png").outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
    }

    @Test fun drawsTheChangeInEveryStyleAndAppearance() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var after by mutableStateOf(false)
        var page by mutableStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = false, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background)) {
                    // a new page for each style, so each starts from its own first draw
                    androidx.compose.runtime.key(page) {
                        LazyColumn(Modifier.fillMaxWidth(), contentPadding = PaddingValues(Tokens.Space.l)) {
                            extensionViewItems("page", ExtensionViewState(view(after)), handle)
                        }
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            val tag = "${s.name.lowercase()}-${if (d) "dark" else "light"}"
            rule.mainClock.autoAdvance = true
            rule.runOnIdle { style = s; dark = d; after = false; page++ }
            rule.waitForIdle()
            capture("extension-motion-$tag-1-before")
            rule.mainClock.autoAdvance = false
            rule.runOnIdle { after = true }
            // about halfway through the count: the numbers are on their way, each written as the value writes it
            val today = (0 until 16).map {
                rule.mainClock.advanceTimeByFrame()
                rule.onAllNodes(hasText("M", substring = true)).fetchSemanticsNodes().single().config[SemanticsProperties.Text].joinToString()
            }
            assertTrue("$tag: $today", today.all { Regex("""\d\.\dM""").matches(it) })
            assertTrue("$tag: $today", today.last().dropLast(1).toDouble() in 1.25..8.35)
            capture("extension-motion-$tag-2-halfway")
            rule.mainClock.autoAdvance = true
            rule.waitForIdle()
            capture("extension-motion-$tag-3-after")
            // at rest, exactly what the extension sent
            for (text in listOf("8.4M", "6/8", "¥36.20", "1,480", "3/4", "等你", "评审 PR")) {
                assertEquals("$tag: $text", 1, rule.onAllNodesWithText(text).fetchSemanticsNodes().size)
            }
            assertEquals("$tag: the row that went is gone", 0, rule.onAllNodesWithText("清理旧分支").fetchSemanticsNodes().size)
        }
    }
}
