package persona.feature.settings

import persona.core.contracts.api.PluginUi
import persona.core.contracts.api.PluginUiContext
import persona.core.contracts.api.PluginUiPlace
import persona.core.contracts.api.PluginUiUnsupported
import persona.core.contracts.api.PluginView
import persona.core.contracts.api.PluginViewAction
import persona.core.contracts.api.PluginViewItem
import persona.core.designsystem.Tone
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

// A plugin's declarative UI (docs/plugins.md「声明式 UI」): drawing a view, its fields' values, its buttons. The
// renderer is in PluginViewContent.kt; the pages that hold one are in PluginUiScreen.kt.

internal object PluginViewText {
    /** The four tones a plugin may give, as the app's role colours; anything else is neutral. */
    fun tone(tone: String?): Tone = when (tone) {
        "positive" -> Tone.Positive
        "caution" -> Tone.Caution
        "danger" -> Tone.Danger
        else -> Tone.Neutral
    }

    /** Links and `open_url` are https only (the hub checks it too). */
    fun isHttps(url: String?): Boolean = url != null && url.startsWith("https://") && url.length > "https://".length

    /** A field's value before the user touches it: the plugin's, never for a secret; a switch is off. */
    fun initial(item: PluginViewItem): String = when (item.kind) {
        "secret" -> ""
        "toggle" -> if (item.value == "true") "true" else "false"
        else -> item.value.orEmpty()
    }

    /** Every field of the view by name, as the plugin first sent it. */
    fun fields(view: PluginView?): Map<String, String> = view?.sections.orEmpty().flatMap { it.items }
        .filter { it.type == "field" && !it.name.isNullOrEmpty() }.associate { it.name!! to initial(it) }

    /** A progress ratio, clamped to 0…1 (a plugin's 1.2 is full). */
    fun ratio(item: PluginViewItem): Float = (item.ratio ?: 0.0).toFloat().coerceIn(0f, 1f)

    /** Rows, links, switches and choices line up in one group; the rest stand alone. */
    fun grouped(item: PluginViewItem): Boolean = item.type == "row" || item.type == "link" ||
        (item.type == "field" && (item.kind == "toggle" || item.kind == "choice"))

    /** Kinds this app draws (a newer hub's are skipped, not drawn half); a link that is not https is not drawn. */
    fun drawable(item: PluginViewItem): Boolean = when (item.type) {
        "text", "metric", "progress", "row", "divider" -> true
        "button" -> item.action?.id?.isNotEmpty() == true
        "link" -> isHttps(item.url)
        "field" -> !item.name.isNullOrEmpty() && item.kind in setOf("text", "secret", "number", "toggle", "choice")
        else -> false
    }

    fun percent(item: PluginViewItem): String = "${Math.round(ratio(item) * 100)}%"

    /** A confirm dialog's title: what is about to happen. */
    fun confirmTitle(item: PluginViewItem): String = item.title?.takeIf { it.isNotBlank() } ?: "确认"

    /** Its button: the pressed button's own words, so 停用 reads 停用. */
    fun confirmLabel(item: PluginViewItem): String = item.title?.takeIf { it.isNotBlank() && it.length <= 8 } ?: "确定"

    /** A gateway's host for a line of text (`https://gw.example.com/v1` → `gw.example.com`). */
    fun host(url: String?): String? = url?.takeIf { it.isNotBlank() }?.let {
        runCatching { java.net.URI(it).host }.getOrNull()?.takeIf { h -> h.isNotBlank() } ?: it
    }

    /** A number field typed into: digits, one point, a leading minus. */
    fun isNumber(text: String): Boolean = text.isEmpty() || text.matches(Regex("-?\\d*(\\.\\d*)?"))
}

internal data class PluginViewState(
    val view: PluginView? = null,
    /** The fields as they are now, by name: what a submitting button sends. */
    val values: Map<String, String> = emptyMap(),
    val loading: Boolean = false,
    /** The action on its way (its id): its button spins, the others wait. */
    val acting: String? = null,
    /** Reading the view failed: the page shows it with 重试. */
    val error: String? = null,
    /** The plugin said this launcher's gateway is not its own: the section is not drawn at all. */
    val unsupported: Boolean = false,
) {
    val busy: Boolean get() = loading || acting != null
}

/** What pressing a button brought back for the page to show: a notice, a link to open, a failure. */
internal data class PluginActionOutcome(val toast: String? = null, val openUrl: String? = null, val error: String? = null)

/** One view a plugin draws: at [place], on a launcher's page with [context]. */
internal class PluginViewModel(
    private val api: PluginUi,
    val plugin: String,
    val ui: String,
    private val context: PluginUiContext? = null,
) {
    private val mutable = MutableStateFlow(PluginViewState())
    val state = mutable.asStateFlow()

    constructor(api: PluginUi, place: PluginUiPlace, context: PluginUiContext? = null) : this(api, place.plugin, place.id, context)

    /** Draws it (again), with the plugin's state of the last view. */
    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val view = api.renderUi(plugin, ui, context, state.value.view?.state)
            mutable.update { it.copy(view = view, values = PluginViewText.fields(view), error = null, unsupported = false) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: PluginUiUnsupported) { mutable.update { it.copy(unsupported = true, error = null) } }
        catch (error: Exception) { mutable.update { it.copy(error = "读取失败：${failureReason(error)}") } }
        finally { mutable.update { it.copy(loading = false) } }
    }

    fun edit(name: String, value: String) = mutable.update { it.copy(values = it.values + (name to value)) }

    /**
     * A button or a row was pressed (after its confirm, which the page asks): sends it, with the fields when it
     * submits. A new view replaces the page; nothing back draws it again.
     */
    suspend fun press(action: PluginViewAction): PluginActionOutcome {
        if (state.value.busy || action.id.isEmpty()) return PluginActionOutcome()
        mutable.update { it.copy(acting = action.id) }
        return try {
            val current = state.value
            val result = api.uiAction(plugin, ui, action.id, context, current.view?.state,
                current.values.takeIf { action.submit })
            when {
                result.view != null -> mutable.update { it.copy(view = result.view, values = PluginViewText.fields(result.view), error = null) }
                result.toast == null && result.openUrl == null -> { mutable.update { it.copy(acting = null) }; load() }
            }
            PluginActionOutcome(result.toast?.takeIf { it.isNotBlank() }, result.openUrl?.takeIf(PluginViewText::isHttps))
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: PluginUiUnsupported) {
            mutable.update { it.copy(unsupported = true) }
            PluginActionOutcome()
        } catch (error: Exception) {
            PluginActionOutcome(error = "没有做成：${failureReason(error)}")
        } finally { mutable.update { it.copy(acting = null) } }
    }
}

internal data class PluginPlacesState(
    val places: List<PluginUiPlace>? = null,
    val loading: Boolean = false,
    val error: String? = null,
)

/** Where plugins draw at one placement: a launcher's sections. */
internal class PluginPlacesModel(private val api: PluginUi, private val placement: String, private val context: PluginUiContext? = null) {
    private val mutable = MutableStateFlow(PluginPlacesState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val places = api.uiPlaces(placement, context?.launcher, context?.machine).filter { it.placement.isEmpty() || it.placement == placement }
            mutable.update { it.copy(places = places, error = null) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = "插件的内容读不到：${failureReason(error)}") } }
        finally { mutable.update { it.copy(loading = false) } }
    }
}
