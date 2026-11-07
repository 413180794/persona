package persona.feature.settings

import android.content.res.Resources
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalResources
import persona.core.contracts.api.ExtensionUi
import persona.core.contracts.api.ExtensionUiContext
import persona.core.contracts.api.ExtensionUiNotFound
import persona.core.contracts.api.ExtensionUiPlace
import persona.core.contracts.api.ExtensionUiResult
import persona.core.contracts.api.ExtensionUiUnsupported
import persona.core.contracts.api.ExtensionView
import persona.core.contracts.api.ExtensionViewAction
import persona.core.contracts.api.ExtensionViewItem
import persona.core.contracts.api.ExtensionViewPush
import persona.core.contracts.api.ExtensionViewSection
import persona.core.designsystem.ChartMath
import persona.core.designsystem.JChartData
import persona.core.designsystem.JChartKind
import persona.core.designsystem.JChartSeries
import persona.core.designsystem.JStat
import persona.core.designsystem.Tone
import persona.core.designsystem.UiText
import persona.core.designsystem.failureReasonText
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update

// An extension's declarative UI (docs/extensions/extensions.md "Declarative UI"): drawing a view, its fields' values, its buttons. The
// renderer is in ExtensionViewContent.kt; the pages that hold one are in ExtensionUiScreen.kt.

internal object ExtensionViewText {
    /** The four tones an extension may give, as the app's role colours; anything else is neutral. */
    fun tone(tone: String?): Tone = when (tone) {
        "positive" -> Tone.Positive
        "caution" -> Tone.Caution
        "danger" -> Tone.Danger
        else -> Tone.Neutral
    }

    /** Links and `open_url` are https only (the hub checks it too). */
    fun isHttps(url: String?): Boolean = url != null && url.startsWith("https://") && url.length > "https://".length

    /**
     * The inbox item an inbox item's page asks to see at its source (`open_url: persona://inbox/<id>/source`, the one
     * app link the hub lets that slot open, docs/extensions/extensions.md "UI slots"): the item's id, else null.
     */
    fun inboxSource(url: String?): String? {
        val uri = runCatching { java.net.URI(url ?: return null) }.getOrNull() ?: return null
        if (uri.scheme?.lowercase() !in persona.core.contracts.ProtocolCompatibility.appSchemes || uri.rawAuthority != "inbox" || uri.rawQuery != null || uri.rawFragment != null) return null
        val segments = uri.rawPath?.removePrefix("/")?.split("/") ?: return null
        if (segments.size != 2 || segments[1] != "source") return null
        return runCatching { java.net.URLDecoder.decode(segments[0].replace("+", "%2B"), "UTF-8") }.getOrNull()?.takeIf { it.isNotBlank() }
    }

    /** A field's value before the user touches it: the extension's, never for a secret; a switch is off. */
    fun initial(item: ExtensionViewItem): String = when (item.kind) {
        "secret" -> ""
        "toggle" -> if (item.value == "true") "true" else "false"
        else -> item.value.orEmpty()
    }

    /** Every field (and segmented choice) of the view by name, as the extension first sent it. */
    fun fields(view: ExtensionView?): Map<String, String> = view?.sections.orEmpty().flatMap { it.items }
        .filter { (it.type == "field" || it.type == "segmented" || it.type == "composer") && !it.name.isNullOrEmpty() }.associate { it.name!! to initial(it) } +
        listOfNotNull(view?.tabs?.takeIf { it.name.isNotEmpty() }?.let { it.name to it.value })

    /** A progress ratio, clamped to 0…1 (an extension's 1.2 is full). */
    fun ratio(item: ExtensionViewItem): Float = (item.ratio ?: 0.0).toFloat().coerceIn(0f, 1f)

    /** Rows, links, switches and choices line up in one group; the rest stand alone. */
    fun grouped(item: ExtensionViewItem): Boolean = item.type == "row" || item.type == "link" ||
        (item.type == "field" && (item.kind == "toggle" || item.kind == "choice"))

    /**
     * Kinds this app draws (a newer hub's are skipped, not drawn half, and the rest of the view is drawn); a link that
     * is not https is not drawn, nor a chart without points or a segmented without choices to switch between.
     */
    fun drawable(item: ExtensionViewItem): Boolean = when (item.type) {
        "text", "metric", "progress", "row", "divider", "loading" -> true
        "markdown" -> !item.text.isNullOrBlank()
        "table" -> !item.columns.isNullOrEmpty()
        "empty" -> !item.title.isNullOrBlank()
        "button" -> item.action?.id?.isNotEmpty() == true
        "link" -> isHttps(item.url)
        "field" -> !item.name.isNullOrEmpty() && item.kind in setOf("text", "secret", "number", "toggle", "choice")
        "chart" -> chart(item) != null
        "stat_grid" -> !item.stats.isNullOrEmpty()
        "segmented" -> !item.name.isNullOrEmpty() && item.action?.id?.isNotEmpty() == true && item.choices.orEmpty().size >= 2
        "html" -> !item.html.isNullOrBlank()
        // UI version 4 (#2951)
        "header" -> !item.title.isNullOrBlank()
        "breadcrumb" -> !item.crumbs.isNullOrEmpty()
        "file_tree" -> item.entries != null
        "code" -> item.text != null
        "diff" -> !item.path.isNullOrBlank()
        "comment_thread" -> item.comments != null
        "composer" -> !item.name.isNullOrEmpty() && item.buttons.orEmpty().any { it.action.id.isNotEmpty() }
        else -> false
    }

    /** The values an action sends: the fields' when it submits, then the component's own, then those it carries. */
    fun sent(action: ExtensionViewAction, fields: Map<String, String>, own: Map<String, String>?): Map<String, String>? {
        val out = (if (action.submit) fields else emptyMap()) + own.orEmpty() + action.values.orEmpty()
        return out.takeIf { action.submit || own != null || !action.values.isNullOrEmpty() }
    }

    /**
     * A row's picture: an avatar or a thumbnail, https only (anything else is not loaded: the letters or the icon stand
     * in); the app loads it itself, with no credentials.
     */
    fun picture(url: String?): String? = url?.takeIf(::isHttps)

    /** An avatar's letters: the extension's one or two, else the first of the row's title. */
    fun initials(item: ExtensionViewItem): String =
        item.avatar?.text?.trim()?.takeIf { it.isNotEmpty() }?.take(2) ?: item.title.orEmpty().trim().take(1)

    /**
     * A table's rows as many cells as it has columns (a short row is filled, a long one cut), at most 100 rows of 8
     * columns: the hub refuses more, an older one may not.
     */
    fun tableRows(item: ExtensionViewItem): List<List<String>> {
        val width = item.columns.orEmpty().take(8).size
        return item.rows.orEmpty().take(100).map { row -> List(width) { row.getOrNull(it).orEmpty() } }
    }

    /**
     * Markdown an extension may write (docs/extensions/extensions.md "Declarative UI"): headings, paragraphs, lists, emphasis, code,
     * quotes and https links. Pictures become their words, other links their words; HTML is drawn as the text it is
     * (the renderer has no HTML).
     */
    fun safeMarkdown(text: String): String = text.take(20_000)
        .replace(Regex("!\\[([^\\]]*)]\\([^)]*\\)"), "$1")
        .replace(Regex("\\[([^\\]]*)]\\((?!https://)[^)]*\\)"), "$1")

    /** Lazy-list keys for a section's items: the extension's [ExtensionViewItem.key] when it gives one (and unique). */
    fun itemKeys(items: List<ExtensionViewItem>): List<String?> {
        val seen = HashSet<String>()
        return items.map { item -> item.key?.takeIf { it.isNotBlank() && seen.add(it) } }
    }

    /** Fields aren't drawn on a card (the hub refuses them there too): it opens a page for that. */
    fun drawableOnCard(item: ExtensionViewItem): Boolean = item.type != "field" && item.type != "table" && item.type != "html" && drawable(item)

    /** How deep pages pushed on each other may go (docs/extensions/extensions.md "Page navigation"). */
    const val MaxDepth = 8

    /**
     * The page a push opens on top of [uiId]'s ([depth] pages up): the push's UI of the same extension (this one when
     * empty), its state and title; none past [MaxDepth].
     */
    fun pushed(extensionId: String, uiId: String, title: String, depth: Int, push: ExtensionViewPush, manager: String? = null): persona.core.navigation.ExtensionUiRoute? {
        if (depth + 1 >= MaxDepth) return null
        // the page it opens is the same manager's extension (several managers: the same id is under each)
        return persona.core.navigation.ExtensionUiRoute(extensionId, push.ui?.takeIf { it.isNotBlank() } ?: uiId,
            push.title?.takeIf { it.isNotBlank() } ?: title, push.state, depth + 1, manager)
    }

    /**
     * The whole page a push on a card, a machine's section or a launcher's section opens (it has no stack of its own):
     * the push's UI, else the place's own.
     */
    fun opened(place: ExtensionUiPlace, push: ExtensionViewPush): persona.core.navigation.ExtensionUiRoute =
        persona.core.navigation.ExtensionUiRoute(place.extension, push.ui?.takeIf { it.isNotBlank() } ?: place.id,
            push.title?.takeIf { it.isNotBlank() } ?: place.title.ifBlank { place.id }, push.state,
            manager = place.manager?.takeIf { it.isNotBlank() && it != persona.core.contracts.api.AssistantManagerChoice.current.value.defaultId })

    /** Whether an `extension_ui` event asks to draw [extension]'s UI [ui] again (`ui.invalidate`, all of it when no id). */
    fun invalidates(event: persona.core.contracts.ExtensionUiEvent, extension: String, ui: String): Boolean =
        event.invalidate && event.extension == extension && (event.id.isNullOrEmpty() || event.id == ui)

    /** How long the search box waits after typing stops before asking the extension. */
    const val SearchPauseMillis = 300L

    /** How a chart's value is written: the extension's own words, else short with the unit (`1.2M tokens`). */
    fun display(value: Double, display: String?, unit: String?): String =
        display?.takeIf { it.isNotBlank() } ?: (ChartMath.compact(value) + (unit?.takeIf { it.isNotBlank() }?.let { " $it" } ?: ""))

    /**
     * A chart item as the design system's chart: the first series' labels along the bottom, every series cut to as
     * many points (the hub checks they match), a role colour for a series with a tone. None when there is nothing to draw.
     */
    fun chart(item: ExtensionViewItem): JChartData? {
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
    fun stats(item: ExtensionViewItem): List<JStat> = item.stats.orEmpty().take(6).map { JStat(it.label, it.value, it.detail, tone(it.tone)) }

    /** When a view wants drawing again, clamped to the hub's bounds (30 s to a day); null: it does not. */
    fun refreshAt(state: ExtensionViewState): Long? {
        val seconds = state.view?.refreshAfter?.takeIf { it > 0 } ?: return null
        val drawn = state.drawnAt ?: return null
        return drawn + seconds.coerceIn(30, 24 * 3600) * 1000L
    }

    fun percent(item: ExtensionViewItem): String = "${Math.round(ratio(item) * 100)}%"

    /** A confirm dialog's title: what is about to happen. */
    fun confirmTitle(res: Resources, item: ExtensionViewItem): String = item.title?.takeIf { it.isNotBlank() } ?: res.getString(R.string.settings_ext_confirm_title)

    /** Its button: the pressed button's own words, so "Disable" reads "Disable". */
    fun confirmLabel(res: Resources, item: ExtensionViewItem): String = item.title?.takeIf { it.isNotBlank() && it.length <= 8 } ?: res.getString(R.string.settings_ext_confirm_ok)

    /** A gateway's host for a line of text (`https://gw.example.com/v1` → `gw.example.com`). */
    fun host(url: String?): String? = url?.takeIf { it.isNotBlank() }?.let {
        runCatching { java.net.URI(it).host }.getOrNull()?.takeIf { h -> h.isNotBlank() } ?: it
    }

    /** A number field typed into: digits, one point, a leading minus. */
    fun isNumber(text: String): Boolean = text.isEmpty() || text.matches(Regex("-?\\d*(\\.\\d*)?"))
}

internal data class ExtensionViewState(
    val view: ExtensionView? = null,
    /** The fields as they are now, by name: what a submitting button sends. */
    val values: Map<String, String> = emptyMap(),
    val loading: Boolean = false,
    /** The action on its way (its id): its button spins, the others wait. */
    val acting: String? = null,
    /** Reading the view failed: the page shows it with "Retry". */
    val error: ExtensionFailure? = null,
    /** Why reading it last failed (an inbox item's page tells a refusal from a breakdown); none once drawn. */
    val failure: Throwable? = null,
    /**
     * The extension said this launcher's gateway is not its own, or the hub does not know the machine of a machine's
     * section: the section is not drawn at all.
     */
    val unsupported: Boolean = false,
    /** When the view was last drawn (or tried), for `refresh_after`. */
    val drawnAt: Long? = null,
    /** What the search box holds now (UI version 3); null: as the view says. */
    val query: String? = null,
    /** A section's "Load more" on its way (the section's index): its row spins, the page stays usable. */
    val loadingMore: Int? = null,
) {
    val busy: Boolean get() = loading || acting != null
}

/**
 * What pressing a button brought back for the page to show: a notice, a link to open, a failure, and since UI version 3
 * a page to draw on top ([push]) or going back ([pop]).
 */
internal data class ExtensionActionOutcome(
    val toast: String? = null,
    val openUrl: String? = null,
    val error: ExtensionFailure? = null,
    val push: ExtensionViewPush? = null,
    val pop: Boolean = false,
)

/**
 * One view an extension draws: at [place], with [context] (a launcher's page, a machine's, a card's size). [now] is the
 * clock `refresh_after` counts on; [opens] is which `open_url` a button may bring back (https, and an inbox item's
 * page also its source link).
 */
internal class ExtensionViewModel(
    private val api: ExtensionUi,
    val extension: String,
    val ui: String,
    private val context: ExtensionUiContext? = null,
    private val now: () -> Long = System::currentTimeMillis,
    private val opens: (String) -> Boolean = ExtensionViewText::isHttps,
    /** A pushed page's first state (the push's); later drawings send the view's own. */
    private val initialState: String? = null,
) {
    private val mutable = MutableStateFlow(ExtensionViewState())
    val state = mutable.asStateFlow()

    /** The view is on screen ([shown]); an `ui.invalidate` while it is not marks it [stale], drawn when it comes back. */
    private var shown = false
    private var stale = false
    /** Answers to the search box come back in order; an older one that arrives late is dropped. */
    private var asked = 0

    constructor(api: ExtensionUi, place: ExtensionUiPlace, context: ExtensionUiContext? = null) : this(api, place.extension, place.id, context)

    private fun currentState(): String? = state.value.view?.state ?: initialState

    /** Draws it (again), with the extension's state of the last view. */
    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val view = api.renderUi(extension, ui, context, currentState())
            // drawn again for the same page: the diffs loaded since stay (UI version 5)
            mutable.update { it.copy(view = ExtensionCodeReview.keepLoaded(it.view, view), values = ExtensionViewText.fields(view), error = null, failure = null, unsupported = false) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: ExtensionUiUnsupported) { mutable.update { it.copy(unsupported = true, error = null) } }
        catch (_: ExtensionUiNotFound) { mutable.update { it.copy(unsupported = true, error = null) } }
        catch (error: Exception) { mutable.update { it.copy(error = ExtensionFailure(R.string.settings_ext_load_failed, failureReasonText(error)), failure = error) } }
        finally { mutable.update { it.copy(loading = false, drawnAt = now()) } }
    }

    /**
     * The extension's data changed (`ui.invalidate`, docs/extensions/extensions.md "Declarative UI"): drawn again if on screen, else when it
     * comes back.
     */
    suspend fun invalidate() {
        if (shown) load() else stale = true
    }

    /**
     * The view came on screen or left it (the page resumed or paused): one that was invalidated meanwhile, or that a
     * page was pushed on ([covered]: the user comes back from it, maybe after saving there), is drawn now.
     */
    suspend fun shown(on: Boolean) {
        shown = on
        if (on && stale && state.value.view != null) {
            stale = false
            load()
        }
    }

    /** A page of the extension was pushed on this one: it is drawn again when it shows (docs/extensions/extensions.md "Page navigation"). */
    fun covered() { stale = true }

    /**
     * Draws it again when its `refresh_after` is up, for as long as it is called (the page runs it while it is
     * resumed): coming back after the time is up draws it at once. A view without `refresh_after` just waits.
     */
    suspend fun keepFresh() {
        state.map(ExtensionViewText::refreshAt).distinctUntilChanged().collectLatest { at ->
            at ?: return@collectLatest
            delay((at - now()).coerceAtLeast(0))
            load()
        }
    }

    /**
     * A segmented control was switched to [value]: shown at once, and its action goes to the extension with
     * `{name: value}` (and the page's fields when it submits). If that fails the old choice comes back.
     */
    suspend fun choose(item: ExtensionViewItem, value: String): ExtensionActionOutcome {
        val name = item.name?.takeIf { it.isNotEmpty() } ?: return ExtensionActionOutcome()
        val action = item.action?.takeIf { it.id.isNotEmpty() } ?: return ExtensionActionOutcome()
        val before = state.value.values[name]
        if (before == value || state.value.busy) return ExtensionActionOutcome()
        edit(name, value)
        val outcome = press(action, mapOf(name to value))
        if (outcome.error != null) mutable.update { s -> s.copy(values = s.values + (name to (before ?: ExtensionViewText.initial(item)))) }
        return outcome
    }

    fun edit(name: String, value: String) = mutable.update { it.copy(values = it.values + (name to value)) }

    /**
     * An action that does not hold the page (search, filter, "Load more"): sent with [values]; a new view replaces this one,
     * keeping what is being typed into its fields; [before] marks the state while it goes, [loadingMore] the section
     * spinning. An answer that a later one has overtaken is dropped.
     */
    internal suspend fun quietly(id: String, values: Map<String, String>, loadingMore: Int? = null,
                                 before: (ExtensionViewState) -> ExtensionViewState): ExtensionActionOutcome {
        val ask = ++asked
        mutable.update { before(it).copy(loadingMore = loadingMore ?: it.loadingMore) }
        return try {
            val result = api.uiAction(extension, ui, id, context, currentState(), values)
            if (ask == asked) result.view?.let { view ->
                mutable.update { s ->
                    // What was typed stays; the view's new fields come in with its values.
                    val fresh = ExtensionViewText.fields(view)
                    s.copy(view = view, values = fresh + s.values.filterKeys { it in fresh }, error = null, drawnAt = now())
                }
            }
            ExtensionActionOutcome(result.toast?.takeIf { it.isNotBlank() }, result.openUrl?.takeIf(opens), push = result.push, pop = result.pop)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { ExtensionActionOutcome(error = ExtensionFailure(R.string.settings_ext_action_failed, failureReasonText(error))) }
        finally { if (loadingMore != null) mutable.update { it.copy(loadingMore = null) } }
    }

    /**
     * An action that does not hold the page (UI version 5: a diff's hidden lines, a viewed mark, a diff loaded as it
     * comes near): sent with the action's own [values]; its components go in place, a new view replaces the page keeping
     * what is typed. Says what was answered; null when it failed.
     */
    suspend fun fetch(action: ExtensionViewAction, values: Map<String, String>): ExtensionUiResult? = try {
        val result = api.uiAction(extension, ui, action.id, context, currentState(), values + action.values.orEmpty())
        val view = result.view
        when {
            view != null -> mutable.update { s ->
                val fresh = ExtensionViewText.fields(view)
                s.copy(view = ExtensionCodeReview.keepLoaded(s.view, view), values = fresh + s.values.filterKeys { it in fresh }, drawnAt = now())
            }
            result.items.isNotEmpty() -> mutable.update { s -> s.copy(view = s.view?.let { ExtensionCodeReview.patchView(it, result.items) }) }
        }
        result
    } catch (cancelled: CancellationException) { throw cancelled }
    catch (_: Exception) { null }

    /**
     * A button or a row was pressed (after its confirm, which the page asks): sends it, with the fields when it
     * submits. A new view replaces the page; nothing back draws it again.
     */
    suspend fun press(action: ExtensionViewAction, values: Map<String, String>? = null): ExtensionActionOutcome {
        // A push is not sent: the page draws the extension's page on top (UI version 3).
        action.push?.let { return ExtensionActionOutcome(push = it) }
        if (state.value.busy || action.id.isEmpty()) return ExtensionActionOutcome()
        mutable.update { it.copy(acting = action.id) }
        return try {
            val current = state.value
            val result = api.uiAction(extension, ui, action.id, context, current.view?.state ?: initialState,
                ExtensionViewText.sent(action, current.values, values))
            when {
                result.view != null -> mutable.update { it.copy(view = result.view, values = ExtensionViewText.fields(result.view), error = null, drawnAt = now()) }
                // UI version 5: components in place, or lines for a diff, say enough
                result.items.isNotEmpty() -> mutable.update { s -> s.copy(view = s.view?.let { ExtensionCodeReview.patchView(it, result.items) }) }
                result.lines != null -> Unit
                result.toast == null && result.openUrl == null && result.push == null && !result.pop -> { mutable.update { it.copy(acting = null) }; load() }
            }
            ExtensionActionOutcome(result.toast?.takeIf { it.isNotBlank() }, result.openUrl?.takeIf(opens), push = result.push, pop = result.pop)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: ExtensionUiUnsupported) {
            mutable.update { it.copy(unsupported = true) }
            ExtensionActionOutcome()
        } catch (error: Exception) {
            ExtensionActionOutcome(error = ExtensionFailure(R.string.settings_ext_action_failed, failureReasonText(error)))
        } finally { mutable.update { it.copy(acting = null) } }
    }
}

/**
 * The search box was typed into (the page waits for a pause first): the view's search action with `{q}`, and the
 * filter's choice beside it. The page stays usable meanwhile; an answer overtaken by a later one is dropped.
 */
internal suspend fun ExtensionViewModel.search(query: String): ExtensionActionOutcome {
    val view = state.value.view ?: return ExtensionActionOutcome()
    val search = view.search?.action?.takeIf { it.id.isNotEmpty() } ?: return ExtensionActionOutcome()
    val values = buildMap {
        put("q", query)
        view.filter?.takeIf { it.name.isNotEmpty() }?.let { f -> (state.value.values[f.name] ?: f.value)?.let { put(f.name, it) } }
    }
    return quietly(search.id, values) { it.copy(query = query) }
}

/** The filter beside the search box was set to [value]: its action with `{name: value}` and the search's `q`. */
internal suspend fun ExtensionViewModel.filter(value: String): ExtensionActionOutcome {
    val view = state.value.view ?: return ExtensionActionOutcome()
    val filter = view.filter?.takeIf { it.name.isNotEmpty() && it.action.id.isNotEmpty() } ?: return ExtensionActionOutcome()
    val q = state.value.query ?: view.search?.value
    val values = buildMap {
        put(filter.name, value)
        q?.let { put("q", it) }
    }
    edit(filter.name, value)
    return quietly(filter.action.id, values) { it }
}

/** The end of section [index] came on screen, or its "Load more" was tapped: its action, once until the answer is back. */
internal suspend fun ExtensionViewModel.more(index: Int, section: ExtensionViewSection): ExtensionActionOutcome {
    val action = section.more?.action?.takeIf { it.id.isNotEmpty() } ?: return ExtensionActionOutcome()
    if (state.value.loadingMore != null || state.value.loading) return ExtensionActionOutcome()
    return quietly(action.id, emptyMap(), loadingMore = index) { it }
}

internal data class ExtensionPlacesState(
    val places: List<ExtensionUiPlace>? = null,
    val loading: Boolean = false,
    val error: ExtensionFailure? = null,
)

/** Where extensions draw at one placement: a launcher's sections. */
internal class ExtensionPlacesModel(private val api: ExtensionUi, private val placement: String, private val context: ExtensionUiContext? = null) {
    private val mutable = MutableStateFlow(ExtensionPlacesState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val places = api.uiPlaces(placement, context?.launcher, context?.machine).filter { it.placement.isEmpty() || it.placement == placement }
            mutable.update { it.copy(places = places, error = null) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = ExtensionFailure(R.string.settings_ext_content_failed, failureReasonText(error))) } }
        finally { mutable.update { it.copy(loading = false) } }
    }
}

/**
 * Why a view could not be read or a button did not work: the sentence (a string with one `%1$s`) and the reason in it,
 * put into words where it is shown, in the language shown then.
 */
data class ExtensionFailure(@param:StringRes val sentence: Int, val reason: UiText) {
    constructor(@StringRes sentence: Int, reason: String) : this(sentence, UiText.Plain(reason))

    fun text(res: Resources): String = res.getString(sentence, reason.resolve(res))
}

@Composable
internal fun ExtensionFailure.text(): String = text(LocalResources.current)

/** A tab was chosen (UI version 4): its action with `{name: value}` and the search's `q`; shown at once. */
internal suspend fun ExtensionViewModel.tab(value: String): ExtensionActionOutcome {
    val view = state.value.view ?: return ExtensionActionOutcome()
    val tabs = view.tabs?.takeIf { it.name.isNotEmpty() && it.action.id.isNotEmpty() } ?: return ExtensionActionOutcome()
    if ((state.value.values[tabs.name] ?: tabs.value) == value) return ExtensionActionOutcome()
    val q = state.value.query ?: view.search?.value
    val values = buildMap {
        put(tabs.name, value)
        q?.let { put("q", it) }
        view.filter?.takeIf { it.name.isNotEmpty() }?.let { f -> (state.value.values[f.name] ?: f.value)?.let { put(f.name, it) } }
    }
    edit(tabs.name, value)
    return quietly(tabs.action.id, values) { it }
}

/**
 * A code hosting component's action (UI version 4) with its own values (a path, a line, the text typed): sent like a
 * press, the action's own values on top.
 */
internal suspend fun ExtensionViewModel.act(action: ExtensionViewAction, values: Map<String, String>): ExtensionActionOutcome = press(action, values)
