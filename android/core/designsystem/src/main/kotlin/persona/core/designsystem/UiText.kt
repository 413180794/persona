package persona.core.designsystem

import android.content.Context
import androidx.annotation.PluralsRes
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext

/**
 * Text that code outside composition decides on (a view model's status line, a failure's message) and the page
 * shows in its own language (docs/i18n.md): a string resource with its arguments, or words that are already final
 * (the hub's own reason, a name). Arguments that are themselves [UiText] are resolved first.
 */
sealed interface UiText {
    data class Res(@StringRes val id: Int, val args: List<Any> = emptyList()) : UiText
    data class Plural(@PluralsRes val id: Int, val count: Int, val args: List<Any> = listOf(count)) : UiText
    data class Plain(val text: String) : UiText

    /** The text in [context]'s language, for notifications and other code outside composition. */
    fun resolve(context: Context): String = when (this) {
        is Res -> if (args.isEmpty()) context.getString(id) else context.getString(id, *resolved(context))
        is Plural -> context.resources.getQuantityString(id, count, *resolved(context))
        is Plain -> text
    }

    private fun resolved(context: Context): Array<Any> = when (this) {
        is Res -> args
        is Plural -> args
        is Plain -> emptyList()
    }.map { if (it is UiText) it.resolve(context) else it }.toTypedArray()

    companion object {
        fun of(@StringRes id: Int, vararg args: Any): UiText = Res(id, args.toList())
    }
}

/** [UiText.resolve] in the page's language. */
@Composable
fun UiText.text(): String = resolve(LocalContext.current)
