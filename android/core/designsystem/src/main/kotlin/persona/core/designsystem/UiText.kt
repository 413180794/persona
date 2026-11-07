package persona.core.designsystem

import android.content.Context
import android.content.res.Resources
import androidx.annotation.PluralsRes
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalResources

/**
 * Text that code outside composition decides on (a view model's status line, a failure's message) and the page
 * shows in its own language (docs/protocol/i18n.md): a string resource with its arguments, or words that are already final
 * (the hub's own reason, a name). Arguments that are themselves [UiText] are resolved first.
 */
sealed interface UiText {
    data class Res(@StringRes val id: Int, val args: List<Any> = emptyList()) : UiText
    data class Plural(@PluralsRes val id: Int, val count: Int, val args: List<Any> = listOf(count)) : UiText
    data class Plain(val text: String) : UiText

    /** The text in [context]'s language, for notifications and other code outside composition. */
    fun resolve(context: Context): String = resolve(context.resources)

    /** The text in [resources]' language, for models that hold `Resources` rather than a `Context`. */
    fun resolve(resources: Resources): String = when (this) {
        is Res -> if (args.isEmpty()) resources.getString(id) else resources.getString(id, *resolved(resources))
        is Plural -> resources.getQuantityString(id, count, *resolved(resources))
        is Plain -> text
    }

    private fun resolved(resources: Resources): Array<Any> = when (this) {
        is Res -> args
        is Plural -> args
        is Plain -> emptyList()
    }.map { if (it is UiText) it.resolve(resources) else it }.toTypedArray()

    companion object {
        fun of(@StringRes id: Int, vararg args: Any): UiText = Res(id, args.toList())
    }
}

/** [UiText.resolve] in the page's language. */
@Composable
fun UiText.text(): String = resolve(LocalResources.current)
