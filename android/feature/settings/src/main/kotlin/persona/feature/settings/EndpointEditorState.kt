package persona.feature.settings

import persona.core.designsystem.resolve
import android.content.res.Resources
import androidx.annotation.StringRes
import persona.core.contracts.api.*
import persona.core.designsystem.Status
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import java.time.Instant
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId

/** The add / edit sheet. [editing] is the manual candidate being replaced; null adds a new one. */
internal data class EndpointSheet(
    val editing: String? = null,
    val address: String = "",
    /** [ConnectionEndpoints.check] of the trimmed [address]; null while the field is blank. */
    val check: EndpointCheck? = null,
    val saving: Boolean = false,
    /** Why the last save was refused, in the connection's words. */
    val error: SettingsUiText? = null,
) {
    /** Editing without a change: saving would only reconnect for nothing. */
    val unchanged: Boolean get() = editing != null && (check as? EndpointCheck.Valid)?.url == editing
    val canSave: Boolean get() = check is EndpointCheck.Valid && !unchanged && !saving

    /** The line under the field: what is wrong, or the url that will be saved. */
    fun hint(resources: Resources): String? = when (val result = check) {
        null -> null
        is EndpointCheck.Invalid -> result.message.resolve(resources)
        is EndpointCheck.Valid -> if (unchanged) resources.getString(R.string.settings_endpoint_hint_unchanged)
            else resources.getString(R.string.settings_endpoint_hint_will_save, result.url, routeKind(result.kind, resources))
    }
}

/** A drag in progress: [url] is the row under the finger, [urls] the order shown while it moves. */
internal data class EndpointDrag(val url: String, val urls: List<String>)

/**
 * An order arranged here and handed to the connection ([ConnectionEndpoints.reorder]). The list shows it until the
 * connection reports it, so a drop does not flick back to the old order meanwhile; [id] tells it from a newer one.
 */
internal data class PendingOrder(val urls: List<String>, val id: Int)

internal data class EndpointEditorUi(
    val sheet: EndpointSheet? = null,
    /** The candidate whose actions are open, by url. */
    val actions: String? = null,
    /** The manual candidate whose deletion waits for confirmation. */
    val deleting: String? = null,
    /** Tests started here that have not returned yet; the connection may report them a moment later. */
    val testing: Set<String> = emptySet(),
    /** A change (add, edit, delete, disable, reorder) is running; the next one waits for it. */
    val busy: Boolean = false,
    /** Why the last change or test did not go through; the next action replaces it. */
    val message: SettingsUiText? = null,
    /** The row being dragged and the order shown meanwhile. */
    val drag: EndpointDrag? = null,
    /** An order handed to the connection that it has not reported yet. */
    val pending: PendingOrder? = null,
    /** "Reset order" waits for confirmation. */
    val resetting: Boolean = false,
)

/**
 * Edits to the candidate list of Settings › Connection diagnostics (docs/android/android.md "Connection addresses"). Calls come from the UI
 * dispatcher; changes run one at a time in the order asked, tests run alongside them. The list's order is the
 * order the connection tries: dragging a row or moving it up and down stores the order shown.
 */
internal class EndpointEditorState(
    private val endpoints: ConnectionEndpoints,
    /** How long a stored order may take to show up in the connection's state before the list follows the connection again. */
    private val reportTimeoutMillis: Long = 3_000,
) {
    private val mutable = MutableStateFlow(EndpointEditorUi())
    private val changes = Mutex()
    private var orders = 0
    val ui: StateFlow<EndpointEditorUi> = mutable.asStateFlow()
    val routes: StateFlow<ConnectionEndpointsState> get() = endpoints.state

    /** The entries in the order shown right now. */
    fun shown(): List<EndpointEntry> = shownEntries(endpoints.state.value.entries, mutable.value)

    fun startAdding() = mutable.update { it.copy(sheet = EndpointSheet(), actions = null) }

    /** Only manual candidates can be edited; the others can only be disabled. */
    fun startEditing(url: String) {
        if (entry(url)?.source != EndpointSource.Manual) return
        val check = endpoints.check(url, replacing = url)
        mutable.update { it.copy(sheet = EndpointSheet(editing = url, address = url, check = check), actions = null) }
    }

    fun updateAddress(address: String) {
        val sheet = mutable.value.sheet?.takeUnless { it.saving } ?: return
        val trimmed = address.trim()
        val check = if (trimmed.isEmpty()) null else endpoints.check(trimmed, replacing = sheet.editing)
        mutable.update { it.copy(sheet = sheet.copy(address = address, check = check, error = null)) }
    }

    fun dismissSheet() = mutable.update { if (it.sheet?.saving == true) it else it.copy(sheet = null) }

    /** Saves the sheet's address. On success the sheet closes and the saved url is tested right away. */
    suspend fun save() {
        val sheet = mutable.value.sheet ?: return
        if (!sheet.canSave) return
        mutable.update { it.copy(sheet = sheet.copy(saving = true, error = null), message = null) }
        var saved: String? = null
        var refusal: SettingsUiText? = null
        try {
            changes.withLock {
                mutable.update { it.copy(busy = true) }
                try {
                    val address = sheet.address.trim()
                    saved = sheet.editing?.let { endpoints.replace(it, address) } ?: endpoints.add(address)
                } finally {
                    mutable.update { it.copy(busy = false) }
                }
            }
        } catch (error: CancellationException) {
            throw error
        } catch (error: EndpointEditException) {
            refusal = error.message?.let(SettingsUiText::Raw)
        } catch (_: Exception) {
            refusal = SettingsUiText.Res(R.string.settings_endpoint_save_failed)
        } finally {
            val done = saved != null
            mutable.update { it.copy(sheet = if (done) null else it.sheet?.copy(saving = false, error = refusal)) }
        }
        saved?.let { test(it) }
    }

    fun openActions(url: String) = mutable.update { it.copy(actions = url) }
    fun closeActions() = mutable.update { it.copy(actions = null) }

    /** Deleting cannot be undone, so it waits for [confirmDelete]. Only manual candidates can be deleted. */
    fun askDelete(url: String) {
        if (entry(url)?.source != EndpointSource.Manual) return
        mutable.update { it.copy(deleting = url, actions = null) }
    }

    fun cancelDelete() = mutable.update { it.copy(deleting = null) }

    suspend fun confirmDelete() {
        val url = mutable.value.deleting ?: return
        mutable.update { it.copy(deleting = null) }
        change { endpoints.remove(url) }
    }

    suspend fun setEnabled(url: String, enabled: Boolean) {
        if (entry(url)?.enabled == enabled) return
        change { endpoints.setEnabled(url, enabled) }
    }

    /** One pinned health request; its result lands in [ConnectionEndpointsState.tests]. */
    suspend fun test(url: String) {
        if (url in mutable.value.testing || url in endpoints.state.value.testing) return
        mutable.update { it.copy(testing = it.testing + url, message = null) }
        try {
            endpoints.test(url)
        } catch (error: CancellationException) {
            throw error
        } catch (error: EndpointEditException) {
            mutable.update { it.copy(message = error.message?.let(SettingsUiText::Raw)) }
        } catch (_: Exception) {
            mutable.update { it.copy(message = SettingsUiText.Res(R.string.settings_endpoint_test_failed)) }
        } finally {
            mutable.update { it.copy(testing = it.testing - url) }
        }
    }

    /** Picks up [url]: the list shows [EndpointDrag.urls] until [endDrag] or [cancelDrag]. Disabled rows move too. */
    fun startDrag(url: String) {
        if (mutable.value.drag != null) return
        val urls = shown().map { it.url }
        if (url !in urls) return
        mutable.update { it.copy(drag = EndpointDrag(url, urls), actions = null) }
    }

    /** Moves the dragged row to [index] of the order shown; the rows in between shift by one. */
    fun dragTo(index: Int) = mutable.update { ui ->
        val drag = ui.drag ?: return@update ui
        val from = drag.urls.indexOf(drag.url)
        val to = index.coerceIn(0, drag.urls.lastIndex)
        if (from < 0 || from == to) ui else ui.copy(drag = drag.copy(urls = drag.urls.moved(from, to)))
    }

    /** Puts the dragged row back where it was. */
    fun cancelDrag() = mutable.update { it.copy(drag = null) }

    /**
     * Drops the dragged row. The order is stored only when it differs from the one shown before the drag; either way
     * the row stays where it was dropped (the drag's order becomes the pending one in the same update).
     */
    suspend fun endDrag() {
        val ui = mutable.value
        val drag = ui.drag ?: return
        val entries = endpoints.state.value.entries
        val before = shownEntries(entries, ui.copy(drag = null)).map { it.url }
        val after = arranged(entries, drag.urls).map { it.url }
        if (after == before) mutable.update { it.copy(drag = null) } else commit(after)
    }

    /** "Move up" / "Move down" in the row's actions, for people who cannot or would rather not drag. */
    suspend fun moveUp(url: String) = move(url, -1)
    suspend fun moveDown(url: String) = move(url, 1)

    private suspend fun move(url: String, by: Int) {
        if (mutable.value.drag != null) return
        val urls = shown().map { it.url }
        val from = urls.indexOf(url)
        if (from < 0 || from + by !in urls.indices) return
        commit(urls.moved(from, from + by))
    }

    /** "Reset order" waits for [confirmResetOrder]; there is nothing to reset while the default rule applies. */
    fun askResetOrder() {
        if (!endpoints.state.value.customOrder) return
        mutable.update { it.copy(resetting = true, actions = null) }
    }

    fun cancelResetOrder() = mutable.update { it.copy(resetting = false) }

    suspend fun confirmResetOrder() {
        if (!mutable.value.resetting) return
        // The default order replaces one still on its way: the list follows the connection again.
        mutable.update { it.copy(resetting = false, pending = null) }
        change { endpoints.resetOrder() }
    }

    /**
     * Stores [urls] (top first) as the order to try. The list keeps showing it until the connection reports it,
     * normally a moment after [ConnectionEndpoints.reorder] returns; a refused or failed commit shows why and the
     * list falls back to the connection's order.
     */
    private suspend fun commit(urls: List<String>) {
        val id = ++orders
        mutable.update { it.copy(pending = PendingOrder(urls, id), drag = null) }
        try {
            if (change(R.string.settings_endpoint_order_failed) { endpoints.reorder(urls) }) withTimeoutOrNull(reportTimeoutMillis) {
                combine(endpoints.state, mutable) { routes, ui -> ui.pending?.id != id || reports(routes.entries, urls) }.first { it }
            }
        } finally {
            mutable.update { if (it.pending?.id == id) it.copy(pending = null) else it }
        }
    }

    private fun entry(url: String) = endpoints.state.value.entries.firstOrNull { it.url == url }

    /** Runs [block] after the changes asked before it; false when it did not go through (the message says why). */
    private suspend fun change(@StringRes failed: Int = R.string.settings_endpoint_change_failed, block: suspend () -> Unit): Boolean {
        mutable.update { it.copy(message = null) }
        return changes.withLock {
            mutable.update { it.copy(busy = true) }
            try {
                block()
                true
            } catch (error: CancellationException) {
                throw error
            } catch (error: EndpointEditException) {
                mutable.update { it.copy(message = error.message?.let(SettingsUiText::Raw)) }
                false
            } catch (_: Exception) {
                mutable.update { it.copy(message = SettingsUiText.Res(failed)) }
                false
            } finally {
                mutable.update { it.copy(busy = false) }
            }
        }
    }
}

private fun <T> List<T>.moved(from: Int, to: Int): List<T> = toMutableList().apply { add(to, removeAt(from)) }

/**
 * The entries in the order shown: a drag's while one runs, else an order on its way to the connection, else the
 * connection's own ([ConnectionEndpointsState.entries], the order it tries).
 */
internal fun shownEntries(entries: List<EndpointEntry>, ui: EndpointEditorUi): List<EndpointEntry> =
    (ui.drag?.urls ?: ui.pending?.urls)?.let { arranged(entries, it) } ?: entries

/**
 * [entries] in the order of [urls], the way the connection applies a stored order: urls no longer among the
 * entries are skipped, and entries [urls] does not list (new from the hub) follow at the end in the connection's order.
 */
internal fun arranged(entries: List<EndpointEntry>, urls: List<String>): List<EndpointEntry> {
    val byUrl = entries.associateBy { it.url }
    val listed = urls.distinct().mapNotNull(byUrl::get)
    val seen = listed.mapTo(HashSet()) { it.url }
    return listed + entries.filter { it.url !in seen }
}

/** The connection's [entries] are in the order of [urls]. */
private fun reports(entries: List<EndpointEntry>, urls: List<String>) = entries.map { it.url } == arranged(entries, urls).map { it.url }

/** A candidate row as laid out: its [top] edge and [size] in pixels, in the list's item coordinates. */
internal data class RowSpan(val url: String, val top: Int, val size: Int) {
    val middle: Float get() = top + size / 2f
}

/**
 * Where the dragged row goes, as an index of [order], while it is drawn with its top edge at [top]: past every row
 * whose middle its own middle has crossed. Trading places at the middles keeps it from flickering back and forth:
 * after a swap the row has to travel back by its own height before it swaps again. [rows] are the candidate rows on
 * screen, top to bottom. Null when it stays, and while the layout does not show [order] yet (the previous move is not
 * laid out, so the positions are stale).
 */
internal fun dragMove(order: List<String>, rows: List<RowSpan>, dragged: String, top: Float): Int? {
    val shown = rows.map { it.url }
    val onScreen = shown.toHashSet()
    if (order.filter { it in onScreen } != shown) return null
    val self = shown.indexOf(dragged).takeIf { it >= 0 } ?: return null
    val middle = top + rows[self].size / 2f
    var target = self
    rows.forEachIndexed { index, row ->
        if (index > self && middle > row.middle) target = maxOf(target, index)
        if (index < self && middle < row.middle) target = minOf(target, index)
    }
    return if (target == self) null else order.indexOf(shown[target])
}

/**
 * Pixels per second to scroll the list while the dragged row ([top]..[bottom]) is within [zone] of the clear part of
 * the viewport ([start]..[end], inside any floating bars): faster the deeper it goes, only in the [direction] it was
 * last moved (1 down, -1 up), and only while candidate rows are still out of sight that way. [groupTop] and
 * [groupBottom] are the top of the first and the bottom of the last candidate row, null while that row is off screen.
 */
internal fun edgeScrollVelocity(
    top: Float, bottom: Float, start: Float, end: Float, zone: Float, maxSpeed: Float, direction: Int,
    groupTop: Float?, groupBottom: Float?,
): Float {
    if (zone <= 0f) return 0f
    return when {
        direction > 0 && bottom > end - zone && (groupBottom == null || groupBottom > end) ->
            maxSpeed * ((bottom - (end - zone)) / zone).coerceIn(0f, 1f)
        direction < 0 && top < start + zone && (groupTop == null || groupTop < start) ->
            -maxSpeed * ((start + zone - top) / zone).coerceIn(0f, 1f)
        else -> 0f
    }
}

@get:StringRes
internal val EndpointSource.label: Int get() = when (this) {
    EndpointSource.Manual -> R.string.settings_endpoint_source_manual
    EndpointSource.Hub -> R.string.settings_endpoint_source_hub
    EndpointSource.Pairing -> R.string.settings_endpoint_source_pairing
}

/** "LAN, added by hand": what kind of address it is and where it came from. */
internal fun EndpointEntry.origin(resources: Resources): String =
    resources.getString(R.string.settings_endpoint_origin, routeKind(kind, resources), resources.getString(source.label))

internal enum class EndpointTag(@StringRes val label: Int, val status: Status?) {
    Active(R.string.settings_endpoint_tag_active, Status.Unread),
    Disabled(R.string.settings_endpoint_tag_disabled, null),
}

/**
 * The latest result shown for a candidate: [text] (what happened and how long it took) and [label] (which kind of
 * check, and when) go on separate lines, so a phone never breaks the line inside the duration.
 */
internal data class EndpointOutcome(val text: String, val failed: Boolean, val label: String = "")

/** One candidate row of "Connection diagnostics". */
internal data class EndpointRow(
    val entry: EndpointEntry,
    val origin: String,
    val tags: List<EndpointTag>,
    val outcome: EndpointOutcome?,
    val testing: Boolean,
    val status: Status,
    /** Its turn in the order tried, 1 first; null for a disabled row, which keeps its place but is skipped. */
    val rank: Int? = null,
    val canMoveUp: Boolean = false,
    val canMoveDown: Boolean = false,
) {
    val url: String get() = entry.url
}

/**
 * Rows in the order shown ([entries]: the connection's order, or one arranged here that it has not reported yet).
 * The dot: in use → online; the latest result failed → needs you; else neutral.
 */
internal fun endpointRows(
    resources: Resources,
    routes: ConnectionEndpointsState,
    probes: List<ConnectionCandidate>,
    testing: Set<String> = emptySet(),
    entries: List<EndpointEntry> = routes.entries,
    zone: ZoneId = ZoneId.systemDefault(),
    today: LocalDate = LocalDate.now(zone),
): List<EndpointRow> {
    var tried = 0
    return entries.mapIndexed { index, entry ->
        val active = entry.active || entry.url == routes.activeUrl
        val outcome = endpointOutcome(resources, routes.tests[entry.url], probes.firstOrNull { it.url == entry.url }, zone, today)
        EndpointRow(
            entry = entry,
            origin = entry.origin(resources),
            tags = buildList {
                if (active) add(EndpointTag.Active)
                if (!entry.enabled) add(EndpointTag.Disabled)
            },
            outcome = outcome,
            testing = entry.url in routes.testing || entry.url in testing,
            status = when {
                active -> Status.Unread
                outcome?.failed == true -> Status.NeedsYou
                else -> Status.Idle
            },
            rank = if (entry.enabled) ++tried else null,
            canMoveUp = index > 0,
            canMoveDown = index < entries.lastIndex,
        )
    }
}

/** What the connection's own probe reports for a candidate that answered (`OkHttpHubConnection`); any other measured result failed. */
private val ProbePassed = persona.core.hub.endpointResultLabel(null)

/**
 * The newer of a test started here and the connection's automatic probe of the same url (the test wins a tie or an
 * unreadable time). Both are single samples, and the text says so rather than passing the number off as a baseline.
 */
internal fun endpointOutcome(
    resources: Resources,
    test: EndpointTestResult?,
    probe: ConnectionCandidate?,
    zone: ZoneId = ZoneId.systemDefault(),
    today: LocalDate = LocalDate.now(zone),
): EndpointOutcome? {
    val measured = probe?.takeIf { it.sampleCount > 0 || !it.checkedAt.isNullOrBlank() }
    if (test != null && (measured == null || !isAfter(measured.checkedAt, test.testedAt))) {
        return EndpointOutcome(measuredText(resources, test.result.resolve(resources), test.elapsedMillis, 1), !test.passed,
            measuredLabel(resources, R.string.settings_endpoint_label_test, R.string.settings_endpoint_label_test_at, localTime(test.testedAt, resources.textLocale, zone, today)))
    }
    if (measured == null) return null
    val millis = measured.probeDurationMillis?.takeIf { measured.sampleCount > 0 }
    val result = measured.result.resolve(resources).ifBlank { resources.getString(R.string.settings_endpoint_no_result) }
    return EndpointOutcome(measuredText(resources, result, millis, measured.sampleCount), result != ProbePassed.resolve(resources),
        measuredLabel(resources, R.string.settings_endpoint_label_probe, R.string.settings_endpoint_label_probe_at, localTime(measured.checkedAt, resources.textLocale, zone, today)))
}

/** [label] alone, or [labelAt] with the [time] it was checked. */
private fun measuredLabel(resources: Resources, @StringRes label: Int, @StringRes labelAt: Int, time: String?) =
    time?.takeIf(String::isNotBlank)?.let { resources.getString(labelAt, it) } ?: resources.getString(label)

/** No break between a number and its unit (U+00A0, in the resources). */
private fun measuredText(resources: Resources, result: String, millis: Long?, samples: Int): String {
    val ms = millis?.takeIf { it >= 0 } ?: return result
    return if (samples <= 1) resources.getString(R.string.settings_endpoint_measured_once, result, ms)
        else resources.getString(R.string.settings_endpoint_measured_samples, result, ms, samples)
}

private fun isAfter(first: String?, second: String?): Boolean {
    val a = instant(first) ?: return false
    val b = instant(second) ?: return false
    return a.isAfter(b)
}

private fun instant(value: String?): Instant? = value?.takeIf(String::isNotBlank)?.let {
    runCatching { Instant.parse(it) }.getOrNull() ?: runCatching { OffsetDateTime.parse(it).toInstant() }.getOrNull()
}

/** "In use": [label] goes in the value slot (the kind, or why there is no url), [url] under the title. */
internal data class ActiveRoute(val url: String?, val label: String?)

/** The url in use: what the connection reports, else the last path it observed (`kind · url`). */
internal fun activeRoute(resources: Resources, routes: ConnectionEndpointsState, currentPath: String?, connection: ConnectionState): ActiveRoute {
    val path = currentPath?.takeIf(String::isNotBlank)?.split(" · ", limit = 2)
    val url = routes.activeUrl ?: path?.last()?.takeIf(String::isNotBlank)
    val kind = routes.entries.firstOrNull { it.url == url }?.kind ?: path?.takeIf { it.size == 2 }?.first()
    return when {
        url != null -> ActiveRoute(url, kind?.let { routeKind(it, resources) })
        connection == ConnectionState.Connecting || connection == ConnectionState.Updating -> ActiveRoute(null, resources.getString(R.string.settings_endpoint_choosing))
        else -> ActiveRoute(null, resources.getString(R.string.settings_endpoint_not_connected))
    }
}
