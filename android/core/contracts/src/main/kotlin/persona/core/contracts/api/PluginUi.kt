package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * A plugin's declarative UI (docs/plugins.md「声明式 UI」): the plugin never runs code in the app; it answers with a
 * View of a few fixed kinds of item that each app draws with its own design system, and its buttons come back to it.
 */
interface PluginUi {
    /**
     * `GET /api/v1/plugins/-/ui?placement=…`: what the enabled, running plugins draw at [placement]
     * (`plugin_page`, `launcher_section`); a launcher's section names the launcher (and the machine it is on).
     */
    suspend fun uiPlaces(placement: String, launcher: String? = null, machine: String? = null): List<PluginUiPlace>

    /** `POST /api/v1/plugins/{id}/-/ui/{ui}` without an action: draws the view. */
    suspend fun renderUi(plugin: String, ui: String, context: PluginUiContext? = null, state: String? = null): PluginView

    /**
     * The same with [action] (a button was pressed): a new view, a toast, a link to open, or nothing (draw again).
     * [values] are the page's fields when the action submits them.
     */
    suspend fun uiAction(
        plugin: String, ui: String, action: String, context: PluginUiContext? = null, state: String? = null,
        values: Map<String, String>? = null,
    ): PluginUiResult
}

/** Where a plugin draws something (`{plugin, id, title, placement}`). */
@Serializable
data class PluginUiPlace(
    val plugin: String = "",
    val id: String = "",
    val title: String = "",
    val placement: String = "",
)

/** Where a view is drawn: a launcher's page names the launcher and its machine. */
@Serializable
data class PluginUiContext(val launcher: String? = null, val machine: String? = null)

/** A plugin's view: an optional title, sections of items, and the plugin's own state, sent back as it is. */
@Serializable
data class PluginView(
    val title: String? = null,
    val sections: List<PluginViewSection> = emptyList(),
    val state: String? = null,
)

@Serializable
data class PluginViewSection(
    val title: String? = null,
    val footer: String? = null,
    val items: List<PluginViewItem> = emptyList(),
)

/**
 * One item of a view, by [type]: `text`, `metric`, `progress`, `row`, `button`, `link`, `field` or `divider`; each
 * uses a few of the fields. `tone` is `neutral | positive | caution | danger`.
 */
@Serializable
data class PluginViewItem(
    val type: String = "",
    val text: String? = null,
    /** text: `body | secondary | caption | code`. */
    val style: String? = null,
    val label: String? = null,
    val value: String? = null,
    val detail: String? = null,
    val tone: String? = null,
    /** progress: 0…1. */
    val ratio: Double? = null,
    val title: String? = null,
    val subtitle: String? = null,
    val badge: PluginViewBadge? = null,
    val action: PluginViewAction? = null,
    /** button: `normal | destructive`. */
    val role: String? = null,
    /** link: https only. */
    val url: String? = null,
    /** field: the name its value goes back under. */
    val name: String? = null,
    /** field: `text | secret | number | toggle | choice`. */
    val kind: String? = null,
    val choices: List<PluginViewChoice>? = null,
    val placeholder: String? = null,
)

@Serializable
data class PluginViewBadge(val text: String = "", val tone: String? = null)

/** A button's action: [confirm] asks first, [submit] sends the page's fields with it. */
@Serializable
data class PluginViewAction(val id: String = "", val confirm: String? = null, val submit: Boolean = false)

@Serializable
data class PluginViewChoice(val value: String = "", val title: String? = null) {
    val label: String get() = title?.takeIf { it.isNotBlank() } ?: value
}

/** What a pressed button brings back; all empty means: draw the view again. */
@Serializable
data class PluginUiResult(
    val view: PluginView? = null,
    val toast: String? = null,
    @SerialName("open_url") val openUrl: String? = null,
)

/**
 * The plugin says the launcher's gateway is not its own (404 `{code: "unsupported"}`, a section without `hosts` is
 * listed on every launcher): the section is hidden, not an error.
 */
class PluginUiUnsupported : Exception("unsupported")
