package persona.feature.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import persona.core.contracts.api.ExtensionTheme
import persona.core.designsystem.*

@Composable
internal fun ExtensionThemeOptions() {
    val themes = LocalExtensionThemeOptions.current
    val store = LocalAppStyleStore.current ?: return
    if (themes.isEmpty()) return
    val selected by store.themeKey.collectAsState()
    val scope = LocalExtensionThemeScope.current
    ExtensionThemeChoices(themes, ExtensionThemeColors.selected(scope, selected, themes),
        onChoose = { store.setTheme(it?.let { theme -> ExtensionThemeColors.selection(scope, theme) }) })
}

@Composable
internal fun ExtensionThemeChoices(themes: List<ExtensionTheme>, selected: ExtensionTheme?, onChoose: (ExtensionTheme?) -> Unit) {
    JSectionHeader(stringResource(R.string.settings_ext_themes_extension))
    Column {
        JGroupItem(GroupPosition.of(0, themes.size + 1)) {
            JRow(stringResource(R.string.settings_ext_theme_default), onClick = { onChoose(null) }, trailing = { JRadio(selected == null, null) })
        }
        themes.forEachIndexed { index, theme ->
            JGroupItem(GroupPosition.of(index + 1, themes.size + 1)) {
                JRow(theme.name.ifBlank { theme.id }, subtitle = theme.extensionName.ifBlank { theme.extension },
                    onClick = { onChoose(theme) }, trailing = { JRadio(selected?.key == theme.key, null) })
            }
        }
    }
    SettingsCaption(stringResource(R.string.settings_ext_theme_note),

        Modifier.padding(vertical = Tokens.Space.s))
}
