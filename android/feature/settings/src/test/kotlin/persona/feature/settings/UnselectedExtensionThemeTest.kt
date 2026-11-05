package persona.feature.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.material3.Text
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertArrayEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.ExtensionTheme
import persona.core.designsystem.*

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
class UnselectedExtensionThemeTest {
    @get:Rule val rule = createComposeRule()

    @Test fun installedButUnselectedThemeDoesNotChangeAnyStylesPixels() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var hosted by mutableStateOf(false)
        val api = object : ExtensionApiStub() {
            override suspend fun themes() = listOf(ExtensionTheme("example", "示例", "ocean", "海蓝",
                mapOf("accent" to "#0059A6"), mapOf("accent" to "#8BC4FF")))
        }
        rule.setContent {
            val context = LocalContext.current
            val store = remember { AppStyleStore(context).apply { setTheme(null) } }
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                CompositionLocalProvider(LocalAppStyleStore provides store) {
                    val content: @Composable () -> Unit = {
                        Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                            JGroupItem(GroupPosition.Single) { JRow("默认界面", subtitle = "扩展主题已安装，但还没有选择") }
                            JCheckbox(true, {})
                            JRadio(true, {})
                            JButton("继续", {})
                            JInlineStatus("还有一件事", tone = InlineTone.Caution)
                        }
                    }
                    Column {
                        if (hosted) ExtensionDeclarationsHost(api, "test") {
                            content()
                            Text("loaded=${LocalExtensionThemeOptions.current.size}")
                        } else content()
                    }
                }
            }
        }
        fun pixels(): IntArray {
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            return IntArray(bitmap.width * bitmap.height).also {
                bitmap.getPixels(it, 0, bitmap.width, 0, 0, bitmap.width, bitmap.height)
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle { style = s; dark = d; hosted = false }
            val before = pixels()
            rule.runOnIdle { hosted = true }
            rule.waitUntil(timeoutMillis = 5_000) { rule.onAllNodesWithText("loaded=1").fetchSemanticsNodes().isNotEmpty() }
            val after = pixels()
            assertArrayEquals("${s.key} ${if (d) "dark" else "light"}", before, after)
        }
    }
}
