package jarvis.android.data

import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * The items of one session, kept continuous (docs/protocol.md「items 的连续性」): `covered` is the last transcript line
 * held without a hole from the first loaded one. A batch that starts after `covered` means one went missing: wait a
 * moment (the subscribe backlog may still be on its way), then fill it with `after_seq`.
 */
class ChatStore(private val hub: Hub, val key: SessionKey, private val scope: CoroutineScope) {
    data class State(
        val items: List<Item> = emptyList(),
        val loading: Boolean = true,
        val hasOlder: Boolean = false,
        val loadingOlder: Boolean = false,
        val error: String? = null,
    )

    private val _state = MutableStateFlow(State())
    val state: StateFlow<State> = _state.asStateFlow()

    private val mutex = Mutex()
    private val byId = LinkedHashMap<String, Item>()
    private var history: String? = null
    private var covered = -1
    private var gapJob: Job? = null
    private var closed = false

    init {
        scope.launch { loadLatest() }
    }

    /** Called on every (re)connect: the hub forgets subscriptions with the socket. */
    fun resubscribe() {
        if (closed) return
        hub.send(buildJsonObject {
            put("type", "subscribe"); put("machine_id", key.machineId); put("session_id", key.sessionId)
            put("lazy_images", true)
            if (covered >= 0) put("since_seq", covered)
        })
    }

    fun close() {
        closed = true
        gapJob?.cancel()
        hub.send(buildJsonObject { put("type", "unsubscribe"); put("machine_id", key.machineId); put("session_id", key.sessionId) })
    }

    private suspend fun loadLatest() {
        try {
            val page = hub.api.items(key)
            mutex.withLock {
                byId.clear()
                history = page.history
                covered = page.toSeq ?: page.items.maxOfOrNull { it.seq } ?: -1
                page.items.forEach { byId[it.id] = it }
                publish(hasOlder = page.hasMore, loading = false)
            }
        } catch (e: Exception) {
            Log.w(Hub.TAG, "items ${key.path}", e)
            _state.value = _state.value.copy(loading = false, error = e.message)
        }
        resubscribe()
    }

    /** A live or backlog batch from the WebSocket. */
    fun onPush(items: List<Item>, history: String?, fromSeq: Int?, toSeq: Int?, moreAfter: Boolean) {
        scope.launch {
            mutex.withLock {
                if (history != null && this@ChatStore.history != null && history != this@ChatStore.history) {
                    // The transcript was replaced or rewritten: what we hold no longer lines up.
                    scope.launch { loadLatest() }
                    return@withLock
                }
                if (history != null) this@ChatStore.history = history
                items.forEach { byId[it.id] = it }
                when {
                    fromSeq == null || toSeq == null -> toSeq?.let { if (it > covered) covered = it }
                    fromSeq <= covered -> if (toSeq > covered) covered = toSeq
                    else -> scheduleGapFill()
                }
                if (moreAfter) scheduleGapFill(0)
                publish()
            }
        }
    }

    /** The session's rev moved but nothing came (not subscribed yet, or a lost batch): catch up after a moment. */
    fun onRev() = scheduleGapFill(1_500)

    private fun scheduleGapFill(wait: Long = 1_500) {
        if (gapJob?.isActive == true) return
        gapJob = scope.launch {
            delay(wait)
            if (covered < 0) return@launch
            var pages = 0
            while (pages++ < 8) {
                val page = runCatching { hub.api.items(key, afterSeq = covered) }.getOrNull() ?: return@launch
                var more = false
                mutex.withLock {
                    if (page.history != null && history != null && page.history != history) {
                        scope.launch { loadLatest() }
                        return@launch
                    }
                    page.items.forEach { byId[it.id] = it }
                    page.toSeq?.let { if (it > covered) covered = it }
                    more = page.moreAfter
                    publish()
                }
                if (!more) return@launch
            }
            loadLatest() // too far behind: take the latest page instead
        }
    }

    fun loadOlder() {
        val s = _state.value
        if (!s.hasOlder || s.loadingOlder) return
        _state.value = s.copy(loadingOlder = true)
        scope.launch {
            val first = mutex.withLock { byId.values.minOfOrNull { it.seq } }
            try {
                val page = hub.api.items(key, beforeSeq = first)
                mutex.withLock {
                    val merged = LinkedHashMap<String, Item>()
                    page.items.forEach { merged[it.id] = it }
                    merged.putAll(byId)
                    byId.clear(); byId.putAll(merged)
                    publish(hasOlder = page.hasMore, loadingOlder = false)
                }
            } catch (e: Exception) {
                _state.value = _state.value.copy(loadingOlder = false, error = e.message)
            }
        }
    }

    private fun publish(hasOlder: Boolean = _state.value.hasOlder, loading: Boolean = false, loadingOlder: Boolean = _state.value.loadingOlder) {
        val sorted = byId.values.sortedWith(compareBy<Item> { it.seq }.thenBy { it.id.substringAfterLast('#').toIntOrNull() ?: 0 })
        _state.value = State(sorted, loading, hasOlder, loadingOlder, null)
    }
}
