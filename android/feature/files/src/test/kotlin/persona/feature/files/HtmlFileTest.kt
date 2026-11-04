package persona.feature.files

import android.view.View
import android.view.ViewGroup
import android.webkit.WebView
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import persona.core.contracts.api.SessionFileContent
import persona.core.designsystem.PersonaTheme
import persona.feature.files.HtmlPreview.Policy

/**
 * The HTML preview's WebView, as Robolectric records it: the settings that keep the page away from the app, files
 * and the network, the policy in front of the page, no address; turning scripts and the network on rebuilds it.
 * (Robolectric's WebView doesn't draw, so there is no screenshot here.)
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class HtmlFileTest {
    @get:Rule val rule = createComposeRule()

    private fun webViews(root: View): List<WebView> = when (root) {
        is WebView -> listOf(root)
        is ViewGroup -> (0 until root.childCount).flatMap { webViews(root.getChildAt(it)) }
        else -> emptyList()
    }

    @Test fun isolatedByDefaultAndRebuiltWhenAllowed() {
        val file = SessionFileContent("/w/page.html", "page.html", text = "<!doctype html><p>hi<img src=\"a.png\">")
        var policy by mutableStateOf(Policy())
        lateinit var root: View
        rule.setContent {
            root = LocalView.current
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                Box(Modifier.size(400.dp, 600.dp)) { HtmlFile(file, policy) }
            }
        }
        rule.waitForIdle()
        val web = webViews(root).single()
        with(web.settings) {
            assertFalse(javaScriptEnabled)
            assertFalse(allowFileAccess)
            assertFalse(allowContentAccess)
            assertFalse(domStorageEnabled)
            assertTrue(blockNetworkLoads)
        }
        val load = shadowOf(web).lastLoadDataWithBaseURL
        assertEquals(null, load.baseUrl)
        assertTrue(load.data, load.data.startsWith("<!doctype html><meta http-equiv=\"Content-Security-Policy\""))
        assertTrue(load.data.contains("script-src 'none'"))
        rule.onNodeWithText("预览：脚本和联网已关闭，可在 ⋯ 里打开 · 页面引用的 1 个相邻文件不会加载").assertExists()

        policy = Policy(scripts = true, network = true)
        rule.waitForIdle()
        val again = webViews(root).single()
        assertTrue(again.settings.javaScriptEnabled)
        assertFalse(again.settings.blockNetworkLoads)
        assertFalse(again.settings.allowFileAccess)
        assertEquals("<!doctype html><p>hi<img src=\"a.png\">", shadowOf(again).lastLoadDataWithBaseURL.data)

        // With the network on the page is the same either way; turning scripts off still rebuilds it without them.
        policy = Policy(scripts = false, network = true)
        rule.waitForIdle()
        assertFalse(webViews(root).single().settings.javaScriptEnabled)
        rule.onNodeWithText("已允许联网，脚本已关闭 · 页面引用的 1 个相邻文件不会加载").assertExists()
    }
}
