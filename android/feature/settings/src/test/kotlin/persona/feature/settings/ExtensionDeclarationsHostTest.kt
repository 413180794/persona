package persona.feature.settings

import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.ExtensionTheme
import persona.core.designsystem.*

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ExtensionDeclarationsHostTest {
    @get:Rule val rule = createComposeRule()
    @Test fun disabledContributionDropsSelectedThemeAndReturnsToDefault() {
        val events = MutableSharedFlow<Unit>(extraBufferCapacity = 1)
        val theme = ExtensionTheme("example", "示例", "ocean", "海蓝", mapOf("accent" to "#0059A6"), mapOf("accent" to "#8BC4FF"))
        var enabled = true
        val api = object : ExtensionApiStub() {
            override val changes = events
            override suspend fun themes() = if (enabled) listOf(theme) else emptyList()
        }
        var store: AppStyleStore? = null
        var choices = emptyList<ExtensionTheme>()
        var active: ExtensionTheme? = null
        rule.setContent {
            val context = LocalContext.current
            val appearance = remember { AppStyleStore(context).also { store = it; it.setTheme(null) } }
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                CompositionLocalProvider(LocalAppStyleStore provides appearance) {
                    ExtensionDeclarationsHost(api, "test") {
                        choices = LocalExtensionThemeOptions.current
                        active = LocalExtensionRoleTheme.current
                        Text("主题色")
                    }
                }
            }
        }
        rule.waitUntil { choices.size == 1 }
        assertNull(active)
        rule.runOnIdle { store!!.setTheme(ExtensionThemeColors.selection("test", theme)) }
        rule.waitUntil { active != null }
        assertEquals(theme, active)
        rule.runOnIdle { enabled = false; events.tryEmit(Unit) }
        rule.waitUntil { choices.isEmpty() && store!!.themeKey.value == null }
        assertNull(active)
    }
}
