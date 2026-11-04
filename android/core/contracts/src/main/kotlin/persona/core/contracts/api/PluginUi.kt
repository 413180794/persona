package persona.core.contracts.api

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonDecoder

/**
 * A plugin's declarative UI (docs/plugins.md「声明式 UI」): the plugin never runs code in the app; it answers with a
 * View of a few fixed kinds of item that each app draws with its own design system, and its buttons come back to it.
 */
interface PluginUi {
    /**
     * `GET /api/v1/plugins/-/ui?placement=…`: what the enabled, running plugins draw at [placement]
     * (`plugin_page`, `launcher_section`); a launcher's section names the launcher (and the machine it is on). An empty
     * [placement] lists every slot but launcher sections at once (`nav_page`, `overview_card`, `machine_section`, with
     * their badges; docs/plugins.md「UI 插槽」), only on a hub that says `plugin_ui_slots`.
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

/**
 * Where a plugin draws something (`{plugin, plugin_name, id, title, placement, size, icon?, opens?, badge?}`). The
 * slots of docs/plugins.md「UI 插槽」: a `nav_page` has an [icon] and maybe a [badge]; an `overview_card` [opens] one of
 * the plugin's pages.
 */
@Serializable
data class PluginUiPlace(
    val plugin: String = "",
    val id: String = "",
    val title: String = "",
    val placement: String = "",
    @SerialName("plugin_name") val pluginName: String? = null,
    /** `full` or `compact` (a card). */
    val size: String? = null,
    /** A `nav_page`'s: `plugin`, `chart`, `gauge`, `list`, `bell`, `doc`, `calendar`, `money`; anything else is `plugin`. */
    val icon: String? = null,
    /** An `overview_card`'s: the id of the plugin's page tapping the card opens; none, the card does not open anything. */
    val opens: String? = null,
    /** A `nav_page`'s badge, set by the plugin (and changed by `plugin_ui` events). */
    val badge: PluginViewBadge? = null,
)

/**
 * Where a view is drawn and for whom (docs/plugins.md「UI 插槽」「上下文」): a launcher's page names the launcher and its
 * machine, a machine's section the machine; [size] is `compact` for a card. The time zone, language, platform and UI
 * version are the app's: the HTTP client fills in what is left empty.
 */
@Serializable
data class PluginUiContext(
    val launcher: String? = null,
    val machine: String? = null,
    val size: String? = null,
    val tz: String? = null,
    val locale: String? = null,
    val platform: String? = null,
    @SerialName("ui_version") val uiVersion: Int? = null,
)

/**
 * A plugin's view: an optional title, sections of items, the plugin's own state, sent back as it is, and how many
 * seconds after which it wants to be drawn again while on screen ([refreshAfter]; 0 or none: never).
 */
@Serializable
data class PluginView(
    val title: String? = null,
    val sections: List<PluginViewSection> = emptyList(),
    val state: String? = null,
    @SerialName("refresh_after") val refreshAfter: Int? = null,
)

/** A section of a view. An item this app cannot even read (a newer hub's) is left out, never the whole view. */
@Serializable
data class PluginViewSection(
    val title: String? = null,
    val footer: String? = null,
    @Serializable(with = LenientItemsSerializer::class) val items: List<PluginViewItem> = emptyList(),
)

/** Reads a section's items one by one: an item that does not decode is skipped and the rest are drawn. */
internal object LenientItemsSerializer : KSerializer<List<PluginViewItem>> {
    private val list = ListSerializer(PluginViewItem.serializer())
    override val descriptor = list.descriptor

    override fun deserialize(decoder: Decoder): List<PluginViewItem> {
        val input = decoder as? JsonDecoder ?: return list.deserialize(decoder)
        val array = input.decodeJsonElement() as? JsonArray ?: throw SerializationException("items: expected an array")
        return array.mapNotNull { element ->
            try { input.json.decodeFromJsonElement(PluginViewItem.serializer(), element) }
            catch (_: SerializationException) { null }
            catch (_: IllegalArgumentException) { null }
        }
    }

    override fun serialize(encoder: Encoder, value: List<PluginViewItem>) = list.serialize(encoder, value)
}

/**
 * One item of a view, by [type]: `text`, `metric`, `progress`, `row`, `button`, `link`, `field` or `divider`, and
 * since UI version 2 `chart`, `stat_grid` and `segmented`; each uses a few of the fields. `tone` is
 * `neutral | positive | caution | danger`. A type this app does not know is skipped when drawn.
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
    /** field: `text | secret | number | toggle | choice`; chart: `bar | line`. */
    val kind: String? = null,
    /** A choice field's or a segmented's choices. */
    val choices: List<PluginViewChoice>? = null,
    val placeholder: String? = null,
    /** chart: 1 to 4 series with the same labels in the same order. */
    val series: List<PluginChartSeries>? = null,
    /** chart: the unit of its values, written after a value without a `display`. */
    val unit: String? = null,
    /** chart: bars of the series stacked (else side by side); lines ignore it. */
    val stacked: Boolean? = null,
    /** stat_grid: 1 to 6 numbers side by side. */
    val stats: List<PluginStat>? = null,
)

/** One line, or one colour of bars, of a chart. No [tone]: the app's own colours for series, in order. */
@Serializable
data class PluginChartSeries(
    val name: String? = null,
    val tone: String? = null,
    val points: List<PluginChartPoint> = emptyList(),
)

/** A value at a label (a day, a model); [display] is how to write it (「¥1.20」), else the app writes it with the unit. */
@Serializable
data class PluginChartPoint(val label: String = "", val value: Double = 0.0, val display: String? = null)

/** One number of a stat_grid; [tone] colours its detail. */
@Serializable
data class PluginStat(val label: String = "", val value: String = "", val detail: String? = null, val tone: String? = null)

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

/**
 * The hub has no such thing to draw for (404 `not_found`): a machine's section for a machine it does not know (one of
 * another hub), a plugin that has gone. A machine's page leaves the section out.
 */
class PluginUiNotFound(message: String) : Exception(message)
