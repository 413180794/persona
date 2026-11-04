@file:OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)

package persona.core.contracts.api

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.json.JsonNames
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonDecoder

/**
 * An extension's declarative UI (docs/extensions.md「声明式 UI」): the extension never runs code in the app; it answers with a
 * View of a few fixed kinds of item that each app draws with its own design system, and its buttons come back to it.
 */
interface ExtensionUi {
    /**
     * `GET /api/v1/extensions/-/ui?placement=…`: what the enabled, running extensions draw at [placement]
     * (`extension_page`, `launcher_section`, `inbox_item`); a launcher's section names the launcher (and the machine it is
     * on). An empty [placement] lists every slot but launcher sections at once (`nav_page`, `overview_card`,
     * `machine_section`, with their badges; docs/extensions.md「UI 插槽」), only on a hub that says `extension_ui_slots`. A hub
     * that does not know a placement lists nothing there.
     */
    suspend fun uiPlaces(placement: String, launcher: String? = null, machine: String? = null): List<ExtensionUiPlace>

    /** `POST /api/v1/extensions/{id}/-/ui/{ui}` without an action: draws the view. */
    suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext? = null, state: String? = null): ExtensionView

    /**
     * The same with [action] (a button was pressed): a new view, a toast, a link to open, or nothing (draw again).
     * [values] are the page's fields when the action submits them.
     */
    suspend fun uiAction(
        extension: String, ui: String, action: String, context: ExtensionUiContext? = null, state: String? = null,
        values: Map<String, String>? = null,
    ): ExtensionUiResult
}

/**
 * Where an extension draws something (`{extension, extension_name, id, title, placement, size, icon?, opens?, badge?, connector?}`).
 * The slots of docs/extensions.md「UI 插槽」: a `nav_page` has an [icon] and maybe a [badge]; an `overview_card` [opens] one
 * of the extension's pages; an `inbox_item` draws the page of its [connector]'s inbox items.
 */
@Serializable
data class ExtensionUiPlace(
    @SerialName("extension") @JsonNames("plugin") val extension: String = "",
    val id: String = "",
    val title: String = "",
    val placement: String = "",
    @SerialName("extension_name") @JsonNames("plugin_name") val extensionName: String? = null,
    /** `full` or `compact` (a card). */
    val size: String? = null,
    /** A `nav_page`'s: `extension`, `chart`, `gauge`, `list`, `bell`, `doc`, `calendar`, `money`; anything else is `extension`. */
    val icon: String? = null,
    /** An `overview_card`'s: the id of the extension's page tapping the card opens; none, the card does not open anything. */
    val opens: String? = null,
    /** A `nav_page`'s badge, set by the extension (and changed by `extension_ui` events). */
    val badge: ExtensionViewBadge? = null,
    /** An `inbox_item`'s: the connector whose inbox items it draws (`lark`), the items' `source`. */
    val connector: String? = null,
)

/**
 * Where a view is drawn and for whom (docs/extensions.md「UI 插槽」「上下文」): a launcher's page names the launcher and its
 * machine, a machine's section the machine, an inbox item's page the [item]; [size] is `compact` for a card. The time
 * zone, language, platform and UI version are the app's: the HTTP client fills in what is left empty.
 */
@Serializable
data class ExtensionUiContext(
    val launcher: String? = null,
    val machine: String? = null,
    /** `inbox_item`: the inbox item's id; the hub hands the extension that row. */
    val item: String? = null,
    val size: String? = null,
    val tz: String? = null,
    val locale: String? = null,
    val platform: String? = null,
    @SerialName("ui_version") val uiVersion: Int? = null,
)

/**
 * An extension's view: an optional title, sections of items, the extension's own state, sent back as it is, and how many
 * seconds after which it wants to be drawn again while on screen ([refreshAfter]; 0 or none: never).
 */
@Serializable
data class ExtensionView(
    val title: String? = null,
    val sections: List<ExtensionViewSection> = emptyList(),
    val state: String? = null,
    @SerialName("refresh_after") val refreshAfter: Int? = null,
)

/** A section of a view. An item this app cannot even read (a newer hub's) is left out, never the whole view. */
@Serializable
data class ExtensionViewSection(
    val title: String? = null,
    val footer: String? = null,
    @Serializable(with = LenientItemsSerializer::class) val items: List<ExtensionViewItem> = emptyList(),
)

/** Reads a section's items one by one: an item that does not decode is skipped and the rest are drawn. */
internal object LenientItemsSerializer : KSerializer<List<ExtensionViewItem>> {
    private val list = ListSerializer(ExtensionViewItem.serializer())
    override val descriptor = list.descriptor

    override fun deserialize(decoder: Decoder): List<ExtensionViewItem> {
        val input = decoder as? JsonDecoder ?: return list.deserialize(decoder)
        val array = input.decodeJsonElement() as? JsonArray ?: throw SerializationException("items: expected an array")
        return array.mapNotNull { element ->
            try { input.json.decodeFromJsonElement(ExtensionViewItem.serializer(), element) }
            catch (_: SerializationException) { null }
            catch (_: IllegalArgumentException) { null }
        }
    }

    override fun serialize(encoder: Encoder, value: List<ExtensionViewItem>) = list.serialize(encoder, value)
}

/**
 * One item of a view, by [type]: `text`, `metric`, `progress`, `row`, `button`, `link`, `field` or `divider`, and
 * since UI version 2 `chart`, `stat_grid` and `segmented`; each uses a few of the fields. `tone` is
 * `neutral | positive | caution | danger`. A type this app does not know is skipped when drawn.
 */
@Serializable
data class ExtensionViewItem(
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
    val badge: ExtensionViewBadge? = null,
    val action: ExtensionViewAction? = null,
    /** button: `normal | destructive`. */
    val role: String? = null,
    /** link: https only. */
    val url: String? = null,
    /** field: the name its value goes back under. */
    val name: String? = null,
    /** field: `text | secret | number | toggle | choice`; chart: `bar | line`. */
    val kind: String? = null,
    /** A choice field's or a segmented's choices. */
    val choices: List<ExtensionViewChoice>? = null,
    val placeholder: String? = null,
    /** chart: 1 to 4 series with the same labels in the same order. */
    val series: List<ExtensionChartSeries>? = null,
    /** chart: the unit of its values, written after a value without a `display`. */
    val unit: String? = null,
    /** chart: bars of the series stacked (else side by side); lines ignore it. */
    val stacked: Boolean? = null,
    /** stat_grid: 1 to 6 numbers side by side. */
    val stats: List<ExtensionStat>? = null,
)

/** One line, or one colour of bars, of a chart. No [tone]: the app's own colours for series, in order. */
@Serializable
data class ExtensionChartSeries(
    val name: String? = null,
    val tone: String? = null,
    val points: List<ExtensionChartPoint> = emptyList(),
)

/** A value at a label (a day, a model); [display] is how to write it (「¥1.20」), else the app writes it with the unit. */
@Serializable
data class ExtensionChartPoint(val label: String = "", val value: Double = 0.0, val display: String? = null)

/** One number of a stat_grid; [tone] colours its detail. */
@Serializable
data class ExtensionStat(val label: String = "", val value: String = "", val detail: String? = null, val tone: String? = null)

@Serializable
data class ExtensionViewBadge(val text: String = "", val tone: String? = null)

/** A button's action: [confirm] asks first, [submit] sends the page's fields with it. */
@Serializable
data class ExtensionViewAction(val id: String = "", val confirm: String? = null, val submit: Boolean = false)

@Serializable
data class ExtensionViewChoice(val value: String = "", val title: String? = null) {
    val label: String get() = title?.takeIf { it.isNotBlank() } ?: value
}

/** What a pressed button brings back; all empty means: draw the view again. */
@Serializable
data class ExtensionUiResult(
    val view: ExtensionView? = null,
    val toast: String? = null,
    @SerialName("open_url") val openUrl: String? = null,
)

/**
 * The extension says the launcher's gateway is not its own (404 `{code: "unsupported"}`, a section without `hosts` is
 * listed on every launcher): the section is hidden, not an error.
 */
class ExtensionUiUnsupported : Exception("unsupported")

/**
 * The hub has no such thing to draw for (404 `not_found`): a machine's section for a machine it does not know (one of
 * another hub), an extension that has gone. A machine's page leaves the section out.
 */
class ExtensionUiNotFound(message: String) : Exception(message)

/**
 * This device may not draw extensions' views (403: a device that can only read and answer). An inbox item's page opens
 * the item's own page instead; elsewhere it reads as any failure, in the hub's words.
 */
class ExtensionUiForbidden(message: String) : Exception(message)
