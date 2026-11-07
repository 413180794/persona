package persona.feature.settings

import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import persona.core.contracts.AppLanguage
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/** A language's own name, the same in every language so it can be found from any of them. */
@Composable
internal fun AppLanguage.ownName(): String =
    if (this == AppLanguage.System) stringResource(R.string.settings_language_system) else ownName

/** Settings › Appearance › Language: the row, with the choice as its value. */
@Composable
internal fun LanguageRow(selected: Boolean, onClick: () -> Unit) {
    val choice by AppLanguages.choice.collectAsState()
    JRow(stringResource(R.string.settings_language_title), selected = selected, leadingIcon = PhIcons.GlobeSimple,
        value = choice.ownName(), onClick = onClick)
}

/**
 * Settings › Appearance › Language (docs/protocol/i18n.md): follow the phone, or one of the app's catalog languages. Choosing one restarts
 * the screens in it at once; on Android 13 and later the phone's App language page shows and changes the same choice.
 */
@Composable
internal fun LanguagePage(onBack: () -> Unit, footer: @Composable () -> Unit) {
    val host = LocalLanguageHost.current
    val context = LocalContext.current
    val choice by AppLanguages.choice.collectAsState()
    JListPage(title = stringResource(R.string.settings_language_title), onBack = onBack) {
        item { JSectionHeader(stringResource(R.string.settings_language_header)) }
        AppLanguage.entries.forEachIndexed { index, language ->
            item {
                JGroupItem(GroupPosition.of(index, AppLanguage.entries.size)) {
                    JRow(
                        language.ownName(),
                        subtitle = if (language == AppLanguage.System) {
                            stringResource(R.string.settings_language_system_now, AppLanguages.phoneLanguage(context).ownName())
                        } else null,
                        trailing = { JRadio(language == choice, null) },
                        onClick = { if (language != choice) host?.let { AppLanguages.choose(it, language) } },
                        enabled = host != null,
                    )
                }
            }
        }
        item {
            SettingsCaption(stringResource(R.string.settings_language_caption), Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
        }
        item { footer() }
    }
}
