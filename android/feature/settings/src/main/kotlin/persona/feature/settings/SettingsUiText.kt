package persona.feature.settings

import android.content.res.Resources
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource

/**
 * Text a state holder hands to the page (docs/i18n.md): a string resource with its arguments, resolved in the page's
 * language when shown, or text that came as it is (a hub's own message).
 */
sealed interface SettingsUiText {
    data class Res(@StringRes val id: Int, val args: List<Any> = emptyList()) : SettingsUiText
    data class Raw(val text: String) : SettingsUiText

    fun resolve(resources: Resources): String = when (this) {
        is Res -> resources.getString(id, *args.toTypedArray())
        is Raw -> text
    }
}

@Composable
internal fun SettingsUiText.text(): String = when (this) {
    is SettingsUiText.Res -> stringResource(id, *args.toTypedArray())
    is SettingsUiText.Raw -> text
}
