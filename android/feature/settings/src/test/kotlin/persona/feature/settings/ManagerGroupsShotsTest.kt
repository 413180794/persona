package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isSelected
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.onNodeWithTag
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.ExtensionListing
import persona.core.contracts.api.HubExtension
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.ManagerRoster
import persona.core.designsystem.AppStyle
import persona.core.designsystem.LocalManagerRoster
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.ExtensionRoute
import persona.core.navigation.LocalDetailKey
import java.io.File
import java.time.Instant

/**
 * 设置 › 扩展 with several managers (docs/assistant/managers.md step 4): each manager's extensions under its name, the default's
 * first (the unstamped ones are its), the offline one greyed with 最后同步于; with one manager the list is as before.
 * Written to `build/managers-shots/plugins-*.png`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class ManagerGroupsShotsTest {
    @get:Rule val rule = createComposeRule()

    private val roster = ManagerRoster(ManagersSample.map {
        // 最后同步于 counts from now: the offline one went away three hours ago.
        if (it.id == "m-8b04d2") it.copy(lastSeen = Instant.now().minusSeconds(3 * 3600).toString()) else it
    }, "assistant")

    private val api = object : ExtensionApiStub() {
        override suspend fun listing() = ExtensionListing(listOf(
            HubExtension("example-notes", "示例笔记", "connectors", "running", source = "installed", enabled = true),
            HubExtension("example-voice", "示例语音", "voice", "disabled", source = "builtin", manager = "assistant"),
            HubExtension("example-mail", "工作邮箱", "connectors", "running", source = "installed", enabled = true, manager = "m-3fa21c"),
            HubExtension("example-nas", "家里的相册", "connectors", "running", source = "installed", enabled = true, manager = "m-8b04d2"),
        ), canInstall = true)
    }

    private fun capture(name: String) {
        rule.waitForIdle()
        val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        assertTrue(image.width > 0 && image.height > 0)
        val dir = File(System.getProperty("persona.shots") ?: "build/managers-shots").also { it.mkdirs() }
        File(dir, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun top(text: String) = rule.onAllNodesWithText(text)[0].fetchSemanticsNode().boundsInRoot.top

    @Test fun extensionsGroupByManager() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var shown by mutableStateOf<ManagerRoster?>(roster)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                CompositionLocalProvider(LocalManagerRoster provides shown) {
                    Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) { ExtensionsPage(api, {}, { _, _ -> }) }
                }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("示例笔记").fetchSemanticsNodes().isNotEmpty() }
        // The default first (both of its), then the roster's order; 测试 is still installing and has none.
        assertTrue(top("助手") < top("示例笔记") && top("示例语音") < top("工作"))
        assertTrue(top("工作") < top("工作邮箱") && top("工作邮箱") < top("家里"))
        assertTrue(rule.onAllNodesWithText("最后同步于 3 小时前").fetchSemanticsNodes().isNotEmpty())
        assertEquals(0, rule.onAllNodesWithText("测试").fetchSemanticsNodes().size)
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            capture("plugins-grouped-${s.key}-${if (d) "dark" else "light"}")
        }
        // One manager: the one list as before.
        shown = ManagerRoster(listOf(HubManager("assistant", "助手", online = true, isDefault = true)), "assistant")
        rule.waitForIdle()
        assertEquals(0, rule.onAllNodesWithText("助手").fetchSemanticsNodes().size)
        assertTrue(rule.onAllNodesWithText("这台 hub 的扩展").fetchSemanticsNodes().isNotEmpty())
    }

    /** The merged list as the hub sends it with several managers: a built-in once per manager. */
    private val twice = object : ExtensionApiStub() {
        override suspend fun listing() = ExtensionListing(listOf(
            HubExtension("example-voice", "示例语音", "voice", "disabled", source = "builtin", manager = "assistant"),
            HubExtension("example-voice", "示例语音", "voice", "disabled", source = "builtin", manager = "m-3fa21c"),
        ), canInstall = true)
    }

    /** Before the roster is read (or with a hub that sends none) the merged list is one list: the same id twice still draws. */
    @Test fun theSameIdTwiceBeforeTheRoster() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalManagerRoster provides null) {
                    Box(Modifier.fillMaxSize()) { ExtensionsPage(twice, {}, { _, _ -> }) }
                }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("示例语音").fetchSemanticsNodes().size == 2 }
    }

    /** A built-in is listed under every manager: each row opens its own manager's, and the open one is selected alone. */
    @Test fun aBuiltInUnderEachManagerIsItsOwn() {
        val opened = mutableListOf<Pair<String, String?>>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalManagerRoster provides roster, LocalDetailKey provides ExtensionRoute("example-voice", manager = "m-3fa21c")) {
                    Box(Modifier.fillMaxSize()) { ExtensionsPage(twice, {}, { id, manager -> opened += id to manager }) }
                }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("示例语音").fetchSemanticsNodes().size == 2 }
        val selected = rule.onAllNodes(hasText("示例语音") and isSelected()).fetchSemanticsNodes()
        assertEquals(1, selected.size)
        assertTrue(selected[0].boundsInRoot.top > top("工作"))
        rule.onAllNodesWithText("示例语音")[0].performClick()
        rule.onAllNodesWithText("示例语音")[1].performClick()
        assertEquals(listOf<Pair<String, String?>>("example-voice" to null, "example-voice" to "m-3fa21c"), opened)
    }
}
