package persona.feature.inbox

import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.LarkConversationTransport
import persona.core.contracts.api.LarkInvalidation
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.*

/** Optional decorator for the composition root; the existing repository still owns drafts and outbox. */
@OptIn(ExperimentalCoroutinesApi::class)
class LarkInboxRepository(
    private val base: InboxRepository,
    private val transports: StateFlow<LarkConversationTransport?>,
    private val connection: Flow<ConnectionState>,
    private val invalidations: Flow<LarkInvalidation>,
    private val cache: LarkWindowCache,
    private val localMessages: ((String) -> Flow<List<LarkMessage>>)? = null,
    private val onMessages: suspend (String, List<JsonObject>) -> Unit = { _, _ -> },
) : InboxRepository by base {
    private class Window(val transport: LarkConversationTransport, val feed: String, val around: Long?) {
        val state = MutableStateFlow(LarkConversation(loading = true))
        val lock = Mutex()
        val readLock = Mutex()
        var raw = emptyList<JsonObject>()
        var older: String? = null
        var newer: String? = null
        var readPosition = -1L
        val readIds = mutableSetOf<String>()
    }
    private val windows = mutableMapOf<String, Window>()
    private fun window(feed: String) = synchronized(windows) { requireNotNull(windows[feed]) }
    private fun owns(w: Window) = transports.value === w.transport

    override fun conversation(feedId: String, startPosition: Long?): Flow<LarkConversation> = transports.flatMapLatest { transport ->
        if (transport == null) flowOf(LarkConversation(error = "请先配对 hub")) else channelFlow {
            val w = synchronized(windows) {
                windows.entries.removeAll { it.value.transport !== transport }
                windows[feedId]?.takeIf { it.around == startPosition } ?: Window(transport, feedId, startPosition).also { windows[feedId] = it }
            }
            launch {
                w.lock.withLock {
                    if (w.raw.isEmpty()) cache.read(transport.scope, feedId, startPosition)?.let { saved ->
                        val rows = saved.messages()
                        val positions = rows.mapNotNull { it.number("position") }
                        if (startPosition == null || positions.isNotEmpty() && startPosition in positions.min()..positions.max()) {
                            install(w, saved, rows, verified = false)
                        }
                    }
                }
                connection.distinctUntilChanged().collectLatest {
                    if (it == ConnectionState.Online) safely(w) { reconcile(w) }
                    else w.state.update { state -> state.copy(verified = false) }
                }
            }
            launch { invalidations.collect { if (feedId in it.feedIds) safely(w) { reconcile(w) } } }
            // The legacy flow supplies persisted outbox bubbles until DATA-06 exposes an outbox-only stream.
            val locals = localMessages?.invoke(feedId) ?: base.conversation(feedId, startPosition).map { it.messages }
            combine(w.state, locals, base.snapshot) { state, messages, inbox ->
                val pending = if (!state.hasNewer) messages.filter { it.position == null && it.clientId != null }
                    .filterNot { local -> state.messages.any { it.id == local.id || it.clientId == local.clientId } } else emptyList()
                state.copy(title = inbox.records.firstOrNull { it.feedId == feedId }?.item?.title ?: state.title,
                    messages = state.messages + pending)
            }.collect { send(it) }
        }
    }

    private suspend fun safely(w: Window, block: suspend () -> Unit) {
        try { block() } catch (e: CancellationException) { throw e }
        catch (_: WindowGap) {
            try { latestConversation(w.feed) }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { w.state.update { it.copy(verified = false, loading = false, error = "消息记录尚未同步完整") } }
        }
        catch (_: Exception) { if (owns(w)) w.state.update { it.copy(loading = false, loadingOlder = false, loadingNewer = false, verified = false, error = "消息加载失败，请重试") } }
    }

    override suspend fun refreshConversation(feedId: String) = safely(window(feedId)) { reconcile(window(feedId)) }
    override suspend fun loadOlder(feedId: String) = safely(window(feedId)) { page(window(feedId), older = true) }
    override suspend fun loadNewer(feedId: String) = safely(window(feedId)) { page(window(feedId), older = false) }
    override suspend fun latestConversation(feedId: String) {
        val w = window(feedId)
        w.lock.withLock {
            val page = fetch(w)
            if (page.flag("has_newer")) throw WindowGap()
            if (owns(w)) install(w, page, page.messages(), revision = true)
        }
    }

    /** Re-read the displayed span so edits and recalls replace old rows, then append through the same cursor chain. */
    private suspend fun reconcile(w: Window) = w.lock.withLock {
        if (!owns(w)) return@withLock
        val last = w.raw.lastOrNull()?.number("position")
        val wasTail = w.raw.isNotEmpty() && !w.state.value.hasNewer
        w.state.update { it.copy(verified = false, error = null) }
        val firstPosition = w.raw.firstOrNull()?.number("position")
        var page = fetch(w, around = firstPosition ?: w.around)
        var first = page
        val rows = page.messages().toMutableList()
        val beforeCursors = mutableSetOf<String>()
        while (firstPosition != null && first.flag("has_older") &&
            (rows.mapNotNull { it.number("position") }.minOrNull() ?: Long.MAX_VALUE) > firstPosition) {
            val cursor = first.cursor("older_cursor") ?: throw WindowGap()
            if (!beforeCursors.add(cursor)) throw WindowGap()
            first = fetch(w, before = cursor)
            rows.addAll(0, first.messages())
        }
        val cursors = mutableSetOf<String>()
        while (page.flag("has_newer") && (wasTail || last != null && (rows.lastOrNull()?.number("position") ?: -1) < last)) {
            val cursor = page.cursor("newer_cursor") ?: throw WindowGap()
            if (!cursors.add(cursor)) throw WindowGap()
            page = fetch(w, after = cursor)
            rows += page.messages()
        }
        if (!owns(w)) return@withLock
        val merged = JsonObject(page + mapOf("has_older" to (first["has_older"] ?: JsonPrimitive(false)),
            "older_cursor" to (first["older_cursor"] ?: JsonNull)))
        install(w, merged, rows)
    }

    private suspend fun page(w: Window, older: Boolean) = w.lock.withLock {
        val state = w.state.value
        if (!owns(w) || !state.verified || if (older) !state.hasOlder else !state.hasNewer) return@withLock
        val cursor = requireNotNull(if (older) w.older else w.newer)
        w.state.update { it.copy(loadingOlder = older, loadingNewer = !older, error = null) }
        try {
            val page = fetch(w, before = cursor.takeIf { older }, after = cursor.takeUnless { older })
            if (!owns(w)) return@withLock
            val hasMore = page.flag(if (older) "has_older" else "has_newer")
            if (hasMore && page.cursor(if (older) "older_cursor" else "newer_cursor")?.let { it != cursor } != true) throw WindowGap()
            val rows = w.raw + page.messages()
            val preserved = if (older) mapOf("has_newer" to JsonPrimitive(state.hasNewer), "newer_cursor" to (w.newer?.let(::JsonPrimitive) ?: JsonNull))
                else mapOf("has_older" to JsonPrimitive(state.hasOlder), "older_cursor" to (w.older?.let(::JsonPrimitive) ?: JsonNull))
            install(w, JsonObject(page + preserved), rows)
        } finally { w.state.update { it.copy(loadingOlder = false, loadingNewer = false) } }
    }

    private class WindowGap : Exception()
    private suspend fun fetch(w: Window, around: Long? = null, before: String? = null, after: String? = null): JsonObject {
        val page = w.transport.messages(w.feed, around, before, after)
        val sync = page.cursor("sync_state") ?: page.cursor("syncState")
        if (sync == "gap") throw WindowGap()
        check(sync == null || sync == "complete") { "Window is not authoritative" }
        return page
    }

    private suspend fun install(w: Window, page: JsonObject, rows: List<JsonObject>, verified: Boolean = true, revision: Boolean = false) {
        if (!owns(w)) return
        val sync = page.cursor("sync_state") ?: page.cursor("syncState")
        if (verified && sync != null && sync != "complete") {
            if (sync == "gap" && !revision) {
                val latest = w.transport.messages(w.feed)
                install(w, latest, latest.messages(), verified = true, revision = true)
                return
            }
            w.state.update { it.copy(verified = false, loading = false, loadingOlder = false,
                loadingNewer = false, error = "消息记录尚未同步完整") }
            return
        }
        w.raw = rows.associateBy { it.text("id") }.values.sortedBy { it.number("position") ?: Long.MAX_VALUE }
        w.older = page.cursor("older_cursor")
        w.newer = page.cursor("newer_cursor")
        w.state.update { it.copy(messages = w.raw.map { row -> larkMessage(row, w.transport, w.feed) }, loading = false,
            hasOlder = page.flag("has_older"), hasNewer = page.flag("has_newer"), loadingOlder = false, loadingNewer = false,
            verified = verified, error = null, windowRevision = it.windowRevision + if (revision) 1 else 0) }
        if (verified) {
            onMessages(w.feed, w.raw)
            try { cache.write(w.transport.scope, w.feed, w.around, JsonObject(page + ("messages" to JsonArray(w.raw)))) }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { /* A disk failure must not discard the verified network window. */ }
        }
    }

    override suspend fun markVisible(feedId: String, messages: List<LarkMessage>) {
        val w = window(feedId)
        w.readLock.withLock {
            if (!owns(w) || !w.state.value.verified) return@withLock
            val visible = messages.mapNotNull { candidate -> w.state.value.messages.firstOrNull {
                it.id == candidate.id && it.position == candidate.position && it.position != null && it.delivery == LarkDelivery.Sent
            } }
            val highest = visible.maxByOrNull { it.position!! } ?: return@withLock
            if (highest.position!! < w.readPosition) return@withLock
            val ids = visible.filter { !it.mine && !it.isRead && it.id !in w.readIds }.map { it.id }
            if (highest.position == w.readPosition && ids.isEmpty()) return@withLock
            w.transport.markRead(feedId, highest.position, highest.badgeCount, ids)
            if (owns(w)) { w.readPosition = highest.position; w.readIds += ids }
        }
    }
}
