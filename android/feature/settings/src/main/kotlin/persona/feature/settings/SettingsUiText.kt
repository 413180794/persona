package persona.feature.settings

import android.content.res.Resources
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalResources
import persona.core.designsystem.UiText

/**
 * Text a state holder hands to the page (docs/protocol/i18n.md): a string resource with its arguments, resolved in the page's
 * language when shown, or text that came as it is (a hub's own message). An argument that is a [UiText] is put into
 * words in the same language first.
 */
sealed interface SettingsUiText {
    data class Res(@StringRes val id: Int, val args: List<Any> = emptyList()) : SettingsUiText
    data class Raw(val text: String) : SettingsUiText

    fun resolve(resources: Resources): String = when (this) {
        is Res -> resources.getString(id, *args.map { if (it is UiText) it.resolve(resources) else it }.toTypedArray())
        is Raw -> text
    }
}

@Composable
internal fun SettingsUiText.text(): String = resolve(LocalResources.current)
