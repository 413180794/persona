package persona.android.ui

import android.app.Application
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import android.content.ComponentName
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import org.robolectric.Shadows
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import org.junit.Assert.assertTrue
import androidx.compose.ui.unit.dp
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.android.R
import persona.core.designsystem.AppNavigation
import persona.core.designsystem.AppNavigationRail
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File

/**
 * The shell's tabs (bottom dock and rail) and a page the demo cannot show, in each language and style, light and dark
 * (docs/protocol/i18n.md). Persona and Agents read the same in every language. PNGs go to `build/i18n-shots/` (or
 * `-Dpersona.shots=<dir>`). The plain Application keeps Hilt's PersonaApp out of the test.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(application = Application::class)
class ShellShotsTest {
    @get:Rule val rule = createEmptyComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", "收件箱", "设置", "体验模式没有终端数据")

    @Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
    @Test fun english() = shots("en", "Inbox", "Settings", "The demo has no terminal data")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h800dp-xhdpi")
    @Test fun japanese() = shots("ja", "受信トレイ", "設定", "体験モードにはターミナルのデータがありません")

    private fun shots(language: String, inbox: String, settings: String, unavailable: String) {
        val navigator = object : Navigator {
            override fun navigate(key: NavKey) = Unit
            override fun back() = Unit
            override fun selectTab(tab: AppTab) = Unit
        }
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        // The app's manifest has no plain activity to host the content: register one for this test.
        val app = ApplicationProvider.getApplicationContext<Application>()
        Shadows.shadowOf(app.packageManager).addActivityIfNotPresent(ComponentName(app, ComponentActivity::class.java))
        val scenario = ActivityScenario.launch(ComponentActivity::class.java)
        scenario.onActivity { activity -> activity.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val items = navigationItems(mapOf(AppTab.Agents to 2, AppTab.Inbox to 5), stringResource(R.string.app_demo_badge))
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background),
                    verticalArrangement = Arrangement.spacedBy(16.dp)) {
                    Row(Modifier.height(360.dp)) {
                        AppNavigationRail(items, AppTab.Inbox.name, onSelect = {})
                        Box(Modifier.width(300.dp)) { UnavailablePage(unavailable, navigator) }
                    }
                    AppNavigation(items, AppTab.Agents.name, onSelect = {})
                }
            }
        } }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            listOf("Persona", "Agents", inbox, settings).forEach {
                assertTrue(it, rule.onAllNodesWithText(it, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
            }
            rule.onNodeWithText(unavailable).assertExists()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "shell-$language-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        scenario.close()
    }
}
