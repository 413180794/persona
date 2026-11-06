package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
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
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
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
 * UI version 3's components (#1851) drawn on the JVM with Robolectric's native graphics: a list page with its search
 * box, filter, rows with an icon, an avatar and a thumbnail, "Load more", markdown, a table, an empty state and a loading
 * one; and an assistant's ```persona-card / ```persona-view in a reply. Every style, light and dark, written to
 * `build/extension-v3-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h2400dp-xhdpi")
class ExtensionComponentsShotsTest {
    @get:Rule val rule = createComposeRule()

    private val page = ExtensionView(title = "笔记", state = "page=1",
        search = ExtensionViewSearch("搜笔记", action = ExtensionViewAction("search")),
        filter = ExtensionViewFilter("folder", "文件夹", listOf(ExtensionViewChoice("all", "全部"), ExtensionViewChoice("work", "工作")), "work",
            ExtensionViewAction("folder")),
        sections = listOf(
            ExtensionViewSection(title = "最近", more = ExtensionViewMore(action = ExtensionViewAction("more")), items = listOf(
                ExtensionViewItem("row", title = "周报", subtitle = "今天 09:30", icon = "doc", key = "n1",
                    action = ExtensionViewAction("open", push = ExtensionViewPush("note=n1"))),
                ExtensionViewItem("row", title = "示例联系人", subtitle = "提到了你", avatar = ExtensionViewAvatar(text = "示"), key = "n2",
                    badge = ExtensionViewBadge("新", "positive")),
                ExtensionViewItem("row", title = "设计稿", subtitle = "3 张图", thumbnail = ExtensionViewThumbnail("not-https", "wide"), key = "n3"),
            )),
            ExtensionViewSection(title = "说明", items = listOf(ExtensionViewItem("markdown",
                text = "## 本周要点\n\n- **发布**：周四上线\n- 评审改到 `周三`\n\n> 有问题在群里说。\n\n详见 [文档](https://example.com/notes)。"))),
            ExtensionViewSection(title = "附件", items = listOf(ExtensionViewItem("table",
                columns = listOf(ExtensionTableColumn("名称"), ExtensionTableColumn("类型"), ExtensionTableColumn("大小", "end")),
                rows = listOf(listOf("周报.md", "文档", "4 KB"), listOf("发布计划-很长很长很长的名字.xlsx", "表格", "128 KB"), listOf("截图.png", "图片", "1.2 MB")),
                caption = "共 3 个"))),
            ExtensionViewSection(items = listOf(ExtensionViewItem("empty", title = "这个文件夹还没有笔记", detail = "在电脑上新建一篇，这里会自动出现。", icon = "folder",
                button = ExtensionEmptyButton("新建笔记", ExtensionViewAction("new"))))),
            ExtensionViewSection(items = listOf(ExtensionViewItem("loading", title = "正在读取更早的笔记…"))),
        ))

    private val card = PersonaViewBlocks.parse("persona-card", """{"type":"stacked_bar","title":"本月预算","subtitle":"截至今天","unit":"元",
        "segments":[{"label":"Claude","value":30},{"label":"GPT","value":10,"note":"含图片"}],"rest":{"label":"未分配","value":60}}""")!!
    private val trend = PersonaViewBlocks.parse("persona-card", """{"type":"line","title":"近 7 天的请求","unit":"次",
        "points":[{"label":"10-29","value":31},{"label":"10-30","value":45},{"label":"10-31","value":28},{"label":"11-01","value":52},
        {"label":"11-02","value":61},{"label":"11-03","value":40},{"label":"11-04","value":57}]}""")!!
    private val reply = PersonaViewBlocks.parse("persona-view", """{"sections":[{"items":[
        {"type":"stat_grid","stats":[{"label":"会议","value":"12"},{"label":"待办","value":"5","detail":"2 件今天到期","tone":"caution"}]},
        {"type":"row","title":"周会","subtitle":"周四 10:00","icon":"calendar"},{"type":"row","title":"评审","subtitle":"周三 15:00","icon":"calendar"}]}]}""")!!

    private val handle = ExtensionViewHandle({ _, _ -> }, { _, _ -> }, {}, {})

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/extension-v3-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun drawsTheNewComponentsAndRepliesInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var replies by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background)) {
                    if (!replies) LazyColumn(Modifier.fillMaxWidth(), contentPadding = PaddingValues(horizontal = Tokens.Space.l, vertical = Tokens.Space.l)) {
                        extensionViewItems("page", ExtensionViewState(page, values = ExtensionViewText.fields(page), loadingMore = null), handle)
                    } else Column(Modifier.fillMaxWidth().padding(Tokens.Space.l)) {
                        PersonaViewBlock(card)
                        PersonaViewBlock(trend, Modifier.padding(top = Tokens.Space.l))
                        PersonaViewBlock(reply, Modifier.padding(top = Tokens.Space.l))
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            replies = false
            capture("extension-v3-page-${s.name.lowercase()}-${if (d) "dark" else "light"}")
            replies = true
            capture("extension-v3-reply-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }
}
