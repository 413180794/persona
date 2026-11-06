package persona.android.ui

import android.content.res.Configuration
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalConfiguration

/** Settings › Appearance › Light/Dark, or a debug capture, pins light or dark; nested PersonaTheme calls read the same configuration. */
@Composable
internal fun AppearanceOverride(dark: Boolean?, content: @Composable () -> Unit) {
    if (dark == null) return content()
    val base = LocalConfiguration.current
    val configuration = remember(base, dark) {
        Configuration(base).apply {
            uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
        }
    }
    CompositionLocalProvider(LocalConfiguration provides configuration, content = content)
}
