package persona.feature.settings

import persona.core.contracts.api.PluginUi
import persona.core.contracts.api.PluginUiContext
import persona.core.contracts.api.PluginUiNotFound
import persona.core.contracts.api.PluginUiPlace
import persona.core.contracts.api.PluginUiUnsupported
import persona.core.contracts.api.PluginView
import persona.core.contracts.api.PluginViewAction
import persona.core.contracts.api.PluginViewItem
import persona.core.designsystem.ChartMath
import persona.core.designsystem.JChartData
import persona.core.designsystem.JChartKind
import persona.core.designsystem.JChartSeries
import persona.core.designsystem.JStat
import persona.core.designsystem.Tone
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
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

    /** Every field (and segmented choice) of the view by name, as the plugin first sent it. */
    fun fields(view: PluginView?): Map<String, String> = view?.sections.orEmpty().flatMap { it.items }
        .filter { (it.type == "field" || it.type == "segmented") && !it.name.isNullOrEmpty() }.associate { it.name!! to initial(it) }

    /** A progress ratio, clamped to 0…1 (a plugin's 1.2 is full). */
    fun ratio(item: PluginViewItem): Float = (item.ratio ?: 0.0).toFloat().coerceIn(0f, 1f)

    /** Rows, links, switches and choices line up in one group; the rest stand alone. */
    fun grouped(item: PluginViewItem): Boolean = item.type == "row" || item.type == "link" ||
        (item.type == "field" && (item.kind == "toggle" || item.kind == "choice"))

    /**
     * Kinds this app draws (a newer hub's are skipped, not drawn half, and the rest of the view is drawn); a link that
     * is not https is not drawn, nor a chart without points or a segmented without choices to switch between.
     */
    fun drawable(item: PluginViewItem): Boolean = when (item.type) {
        "text", "metric", "progress", "row", "divider" -> true
        "button" -> item.action?.id?.isNotEmpty() == true
        "link" -> isHttps(item.url)
        "field" -> !item.name.isNullOrEmpty() && item.kind in setOf("text", "secret", "number", "toggle", "choice")
        "chart" -> chart(item) != null
        "stat_grid" -> !item.stats.isNullOrEmpty()
        "segmented" -> !item.name.isNullOrEmpty() && item.action?.id?.isNotEmpty() == true && item.choices.orEmpty().size >= 2
        else -> false
    }

    /** Fields aren't drawn on a card (the hub refuses them there too): it opens a page for that. */
    fun drawableOnCard(item: PluginViewItem): Boolean = item.type != "field" && drawable(item)

    /** How a chart's value is written: the plugin's own words, else short with the unit (`1.2M tokens`). */
    fun display(value: Double, display: String?, unit: String?): String =
        display?.takeIf { it.isNotBlank() } ?: (ChartMath.compact(value) + (unit?.takeIf { it.isNotBlank() }?.let { " $it" } ?: ""))

    /**
     * A chart item as the design system's chart: the first series' labels along the bottom, every series cut to as
     * many points (the hub checks they match), a role colour for a series with a tone. None when there is nothing to draw.
     */
    fun chart(item: PluginViewItem): JChartData? {
        val kind = when (item.kind) { "bar" -> JChartKind.Bar; "line" -> JChartKind.Line; else -> return null }
        val series = item.series.orEmpty().take(4)
        val labels = series.firstOrNull()?.points.orEmpty().map { it.label }
        if (labels.isEmpty()) return null
        return JChartData(kind, labels, series.map { s ->
            val points = s.points.take(labels.size)
            JChartSeries(s.name?.takeIf { it.isNotBlank() }, points.map { it.value }, points.map { display(it.value, it.display, item.unit) },
                s.tone?.takeIf { it.isNotBlank() }?.let(::tone))
        }, stacked = item.stacked == true)
    }

    /** A stat_grid's numbers, up to six. */
    fun stats(item: PluginViewItem): List<JStat> = item.stats.orEmpty().take(6).map { JStat(it.label, it.value, it.detail, tone(it.tone)) }

    /** When a view wants drawing again, clamped to the hub's bounds (30 s to a day); null: it does not. */
    fun refreshAt(state: PluginViewState): Long? {
        val seconds = state.view?.refreshAfter?.takeIf { it > 0 } ?: return null
        val drawn = state.drawnAt ?: return null
        return drawn + seconds.coerceIn(30, 24 * 3600) * 1000L
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
    /**
     * The plugin said this launcher's gateway is not its own, or the hub does not know the machine of a machine's
     * section: the section is not drawn at all.
     */
    val unsupported: Boolean = false,
    /** When the view was last drawn (or tried), for `refresh_after`. */
    val drawnAt: Long? = null,
) {
    val busy: Boolean get() = loading || acting != null
}

/** What pressing a button brought back for the page to show: a notice, a link to open, a failure. */
internal data class PluginActionOutcome(val toast: String? = null, val openUrl: String? = null, val error: String? = null)

/**
 * One view a plugin draws: at [place], with [context] (a launcher's page, a machine's, a card's size). [now] is the
 * clock `refresh_after` counts on.
 */
internal class PluginViewModel(
    private val api: PluginUi,
    val plugin: String,
    val ui: String,
    private val context: PluginUiContext? = null,
    private val now: () -> Long = System::currentTimeMillis,
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
        catch (_: PluginUiNotFound) { mutable.update { it.copy(unsupported = true, error = null) } }
        catch (error: Exception) { mutable.update { it.copy(error = "读取失败：${failureReason(error)}") } }
        finally { mutable.update { it.copy(loading = false, drawnAt = now()) } }
    }

    /**
     * Draws it again when its `refresh_after` is up, for as long as it is called (the page runs it while it is
     * resumed): coming back after the time is up draws it at once. A view without `refresh_after` just waits.
     */
    suspend fun keepFresh() {
        state.map(PluginViewText::refreshAt).distinctUntilChanged().collectLatest { at ->
            at ?: return@collectLatest
            delay((at - now()).coerceAtLeast(0))
            load()
        }
    }

    /**
     * A segmented control was switched to [value]: shown at once, and its action goes to the plugin with
     * `{name: value}` (and the page's fields when it submits). If that fails the old choice comes back.
     */
    suspend fun choose(item: PluginViewItem, value: String): PluginActionOutcome {
        val name = item.name?.takeIf { it.isNotEmpty() } ?: return PluginActionOutcome()
        val action = item.action?.takeIf { it.id.isNotEmpty() } ?: return PluginActionOutcome()
        val before = state.value.values[name]
        if (before == value || state.value.busy) return PluginActionOutcome()
        edit(name, value)
        val outcome = press(action, if (action.submit) state.value.values else mapOf(name to value))
        if (outcome.error != null) mutable.update { s -> s.copy(values = s.values + (name to (before ?: PluginViewText.initial(item)))) }
        return outcome
    }

    fun edit(name: String, value: String) = mutable.update { it.copy(values = it.values + (name to value)) }

    /**
     * A button or a row was pressed (after its confirm, which the page asks): sends it, with the fields when it
     * submits. A new view replaces the page; nothing back draws it again.
     */
    suspend fun press(action: PluginViewAction, values: Map<String, String>? = null): PluginActionOutcome {
        if (state.value.busy || action.id.isEmpty()) return PluginActionOutcome()
        mutable.update { it.copy(acting = action.id) }
        return try {
            val current = state.value
            val result = api.uiAction(plugin, ui, action.id, context, current.view?.state,
                values ?: current.values.takeIf { action.submit })
            when {
                result.view != null -> mutable.update { it.copy(view = result.view, values = PluginViewText.fields(result.view), error = null, drawnAt = now()) }
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
