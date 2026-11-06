package persona.feature.files

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * The Git page's comparing (#2805): a picture's two versions side by side and under the slider, an added picture
 * with no old version, and a document's text diff by page with a changed table row. Every style, light and dark.
 * PNGs in `build/compare-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
class CompareShotsTest {
    @get:Rule val rule = createComposeRule()

    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)
    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/compare-shots").also { it.mkdirs() }

    private fun icon(color: Int, round: Boolean) = Bitmap.createBitmap(240, 240, Bitmap.Config.ARGB_8888).also {
        val canvas = Canvas(it)
        val paint = Paint().apply { this.color = color; isAntiAlias = true }
        if (round) canvas.drawCircle(120f, 120f, 100f, paint) else canvas.drawRect(30f, 30f, 210f, 210f, paint)
    }.asImageBitmap()

    private fun shots(name: String) {
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "compare-$name-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    @Test fun picturesSideBySideAndUnderTheSlider() {
        val old = icon(0xFF3478F6.toInt(), round = false)
        val new = icon(0xFFFF9500.toInt(), round = true)
        var added by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").size(400.dp, 520.dp).background(PersonaTheme.palette.background)) {
                    ImageCompare(if (added) Picture.Missing else Picture.Shown(old), Picture.Shown(new), "暂存区", "工作区")
                }
            }
        }
        rule.onNodeWithText("旧版 · 暂存区").assertIsDisplayed()
        rule.onNodeWithText("新版 · 工作区").assertIsDisplayed()
        shots("image-side")
        rule.onNodeWithText("滑动").performClick()
        rule.onNodeWithText("旧版").assertIsDisplayed()
        shots("image-slider")
        added = true
        rule.waitForIdle()
        // Only one version: no slider to offer, the missing side says so.
        rule.onNodeWithText("滑动").assertDoesNotExist()
        rule.onNodeWithText("这个版本里没有这个文件").assertIsDisplayed()
        shots("image-added")
    }

    @Test fun aDocumentsTextByPage() {
        val before = listOf(DocUnit("第 1 页", listOf("发布说明", "主屏幕小组件支持按小时刷新", "降雨概率显示为小数", "版本 | 日期 | 负责人", "2.3 | 11-01 | 小林")),
            DocUnit("第 2 页", listOf("已知问题", "小组件在锁屏上不刷新", "手表版暂缓", "下一版计划", "夜间模式下图标看不清")))
        val after = listOf(DocUnit("第 1 页", listOf("发布说明", "主屏幕小组件支持按小时刷新", "降雨概率改成百分比", "版本 | 日期 | 负责人", "2.4 | 11-05 | 小林")),
            DocUnit("第 2 页", listOf("已知问题", "小组件在锁屏上不刷新", "手表版暂缓", "下一版计划")))
        val outcome = compareUnits(before, after)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").size(400.dp, 520.dp).background(PersonaTheme.palette.background)) {
                    DocCompare(outcome, PaddingValues())
                }
            }
        }
        rule.onNodeWithText("+2 −3 行 · 2 处改动（按文字比较）").assertIsDisplayed()
        rule.onNodeWithText("第 2 页").assertIsDisplayed()
        shots("doc")
    }

    @Test fun aDocumentWithTheSameText() {
        val units = listOf(DocUnit("第 1 页", listOf("一样的文字")))
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").size(400.dp, 400.dp).background(PersonaTheme.palette.background)) {
                    DocCompare(compareUnits(units, units), PaddingValues())
                }
            }
        }
        rule.onNodeWithText("文字没有变化").assertIsDisplayed()
    }
}
