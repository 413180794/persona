package persona.feature.agents

import android.content.res.Resources
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalResources
import persona.core.designsystem.UiText

/**
 * Text a view model or a long-lived helper hands to the screen: a string resource and its arguments, resolved in the
 * language on screen when it is shown (a view model outlives a language change, so it never keeps a resolved string).
 * An argument that is a [UiText] (a failure's reason) is put into words in the same language first.
 */
internal class AgentText(@StringRes val id: Int, vararg args: Any) {
    val args: List<Any> = args.toList()

    override fun equals(other: Any?) = other is AgentText && other.id == id && other.args == args
    override fun hashCode() = 31 * id + args.hashCode()
    override fun toString() = "AgentText($id, $args)"

    fun resolve(resources: Resources): String =
        resources.getString(id, *args.map { if (it is UiText) it.resolve(resources) else it }.toTypedArray())

    companion object {
        /** Text that is already in the user's words (the hub's reason, a failure message). */
        fun raw(text: String) = AgentText(R.string.agents_text_raw, text)
    }
}

@Composable
internal fun AgentText.resolve(): String = resolve(LocalResources.current)

/** The resources of the language on screen, for helpers called from composition. */
@Composable
internal fun agentResources(): Resources = LocalResources.current
