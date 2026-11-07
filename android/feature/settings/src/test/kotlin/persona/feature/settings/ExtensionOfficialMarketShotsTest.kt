package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.CatalogExtension
import persona.core.contracts.api.ExtensionCatalog
import persona.core.contracts.api.ExtensionListing
import persona.core.contracts.api.ExtensionMarket
import persona.core.contracts.api.HubExtension
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

// #5017: Settings › Extensions puts Extension market above the installed extensions, and the official market reads Official extensions whatever its
// index calls itself.
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class ExtensionOfficialMarketShotsTest {
    @get:Rule val rule = createComposeRule()

    private val official = ExtensionMarket("o1", "github:example/official", "Example 扩展", extensions = 2, official = true)
    private val team = ExtensionMarket("m1", "https://extensions.example.com/team/", "团队扩展", extensions = 1)

    private fun capture(name: String) {
        rule.waitForIdle()
        val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        assertTrue(image.width > 0 && image.height > 0)
        val dir = File("build/extension-official-market-shots").also { it.mkdirs() }
        File(dir, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun top(text: String) = rule.onAllNodesWithText(text)[0].fetchSemanticsNode().boundsInRoot.top

    @Test fun marketAboveTheInstalledExtensions() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        val api = object : ExtensionApiStub() {
            override suspend fun listing() = ExtensionListing(listOf(
                HubExtension("example-notes", "示例笔记", "connectors", "running", source = "installed", enabled = true),
                HubExtension("example-voice", "示例语音", "voice", "disabled", source = "builtin"),
            ), canInstall = true, markets = true)
        }
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) { ExtensionsPage(api, {}, { _, _ -> }, onMarket = {}) }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("示例笔记").fetchSemanticsNodes().isNotEmpty() }
        assertTrue("扩展市场 is above the installed list", top("扩展市场") < top("示例笔记"))
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            capture("list-${s.key}-${if (d) "dark" else "light"}")
        }
    }

    @Test fun officialMarketReadsOfficialExtensions() {
        var dark by mutableStateOf(false)
        val api = object : ExtensionApiStub() {
            override suspend fun catalog(query: String, market: String?, refresh: Boolean) = ExtensionCatalog(listOf(official, team), listOf(
                CatalogExtension("o1", "Example 扩展", "example-notes", "示例笔记", "1.2.0", "把笔记同步进知识库", publisher = "example"),
                CatalogExtension("m1", "团队扩展", "team-digest", "每日摘要", "2.3.0", "每天早上总结收件箱", publisher = "team"),
            ))
        }
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) { ExtensionMarketPage(api, {}, {}, {}) }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("示例笔记").fetchSemanticsNodes().isNotEmpty() }
        assertTrue("the official market is named 官方扩展", rule.onAllNodesWithText("官方扩展", substring = true).fetchSemanticsNodes().isNotEmpty())
        assertTrue("its index's name is not shown", rule.onAllNodesWithText("Example 扩展", substring = true).fetchSemanticsNodes().isEmpty())
        assertTrue("the other market keeps its name", rule.onAllNodesWithText("团队扩展", substring = true).fetchSemanticsNodes().isNotEmpty())
        for (d in listOf(false, true)) {
            dark = d
            capture("catalog-glass-${if (d) "dark" else "light"}")
        }
    }
}
