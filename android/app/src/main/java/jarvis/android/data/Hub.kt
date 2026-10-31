package jarvis.android.data

import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener

/** Unauthorized: the hub refused this device's token (revoked, or a different hub): retrying can't help, pair again. */
enum class ConnState { Connecting, Online, Offline, Unauthorized }

/** Something the UI or the notifier may want to react to, beyond the state flows. */
sealed interface HubEvent {
    data class NewInteraction(val interaction: Interaction) : HubEvent
    data class Resolved(val id: String) : HubEvent
    data class SessionChanged(val before: Session?, val after: Session) : HubEvent
    data class ToolCall(val callId: String, val name: String, val args: JsonObject) : HubEvent
}

/**
 * The live connection to one hub: the WebSocket (`/api/v1/ws`, docs/protocol.md) with reconnects, and the state it
 * keeps current (machines, sessions, pending interactions, the items of subscribed sessions).
 */
class Hub(val api: ApiClient, private val scope: CoroutineScope, private val deviceName: String) {
    private val _conn = MutableStateFlow(ConnState.Connecting)
    val conn: StateFlow<ConnState> = _conn.asStateFlow()

    private val _machines = MutableStateFlow<List<Machine>>(emptyList())
    val machines: StateFlow<List<Machine>> = _machines.asStateFlow()

    /** machine id → that machine's sessions, as the hub last sent them (full list per machine). */
    private val _sessions = MutableStateFlow<Map<String, List<Session>>>(emptyMap())
    val sessions: StateFlow<Map<String, List<Session>>> = _sessions.asStateFlow()

    private val _interactions = MutableStateFlow<List<Interaction>>(emptyList())
    val interactions: StateFlow<List<Interaction>> = _interactions.asStateFlow()

    /** The inbox list; refreshed when the hub says it changed (WebSocket `inbox`), on (re)connect and on demand. */
    private val _inbox = MutableStateFlow<List<InboxItem>>(emptyList())
    val inbox: StateFlow<List<InboxItem>> = _inbox.asStateFlow()

    suspend fun loadInbox() { runCatching { _inbox.value = api.inbox() } }

    /** Marks done / back at once; put back if the hub refuses. */
    suspend fun setInboxDone(item: InboxItem, done: Boolean) {
        val before = _inbox.value
        val now = java.time.Instant.now().toString()
        _inbox.update { list -> list.map { if (it.id == item.id) it.copy(doneAt = if (done) now else null, doneBy = if (done) "user" else null) else it } }
        runCatching { api.setInboxDone(item.id, done) }.onSuccess { up -> _inbox.update { l -> l.map { if (it.id == up.id) up else it } } }
            .onFailure { _inbox.value = before; throw it }
    }

    /** Pinned / muted spaces and agents, kept by the hub (Postgres) so every device sees the same. */
    private val _pins = MutableStateFlow(Pins())
    val pins: StateFlow<Pins> = _pins.asStateFlow()
    private val _mutes = MutableStateFlow(Pins())
    val mutes: StateFlow<Pins> = _mutes.asStateFlow()

    private val _events = MutableSharedFlow<HubEvent>(extraBufferCapacity = 64)
    val events: SharedFlow<HubEvent> = _events.asSharedFlow()

    var serverVersion: String? = null
        private set
    /** What the hub can do (hello `features`): terminal_stream, terminal_control, terminal_scroll, terminal_click, … */
    @Volatile var features: Set<String> = emptySet()
        private set

    /** A pane's live terminal (docs/terminal.md): one handler per session, told every frame / control / reconnect. */
    sealed interface TerminalEvent {
        data class Frame(val epoch: String, val seq: Int, val full: Boolean, val cols: Int, val rows: Int, val data: ByteArray) : TerminalEvent
        data class Ended(val error: String, val unsupported: Boolean) : TerminalEvent
        data class Control(val state: String, val error: String?) : TerminalEvent
        /** A new connection's hello: subscribe again (continuing from the last frame). */
        data object Reconnected : TerminalEvent
    }
    private val terminalHandlers = java.util.concurrent.ConcurrentHashMap<SessionKey, (TerminalEvent) -> Unit>()

    fun terminalSubscribe(k: SessionKey, epoch: String?, afterSeq: Int, control: Pair<Int, Int>?, takeover: Boolean, handler: (TerminalEvent) -> Unit) {
        terminalHandlers[k] = handler
        send(buildJsonObject {
            put("type", "terminal_subscribe"); put("machine_id", k.machineId); put("session_id", k.sessionId)
            if (epoch != null) { put("epoch", epoch); put("after_seq", afterSeq) }
            if (control != null) put("control", buildJsonObject { put("cols", control.first); put("rows", control.second) })
            if (takeover) put("takeover", true)
        })
    }

    /** Bytes typed into a terminal the phone drives, a new size, the wheel (lines, > 0 back) or a click at [col, row]. */
    fun terminalInput(k: SessionKey, text: String = "", control: Pair<Int, Int>? = null, scroll: Int? = null, at: Pair<Int, Int>? = null, click: Pair<Int, Int>? = null) {
        send(buildJsonObject {
            put("type", "terminal_input"); put("machine_id", k.machineId); put("session_id", k.sessionId)
            if (text.isNotEmpty()) put("text", text)
            if (control != null) put("control", buildJsonObject { put("cols", control.first); put("rows", control.second) })
            if (scroll != null) put("scroll", scroll)
            at?.let { put("at", kotlinx.serialization.json.JsonArray(listOf(JsonPrimitive(it.first), JsonPrimitive(it.second)))) }
            click?.let { put("click", kotlinx.serialization.json.JsonArray(listOf(JsonPrimitive(it.first), JsonPrimitive(it.second)))) }
        })
    }

    fun terminalUnsubscribe(k: SessionKey) {
        if (terminalHandlers.remove(k) == null) return
        send(buildJsonObject { put("type", "terminal_unsubscribe"); put("machine_id", k.machineId); put("session_id", k.sessionId) })
    }

    /** Phone tools declared on every (re)connect; set before start() so the first hello registers them. */
    @Volatile var phoneTools: JsonArray? = null

    private val chats = HashMap<SessionKey, ChatStore>()
    private var ws: WebSocket? = null
    private var loop: Job? = null
    @Volatile private var stopped = false
    @Volatile private var unauthorized = false

    fun start() {
        if (loop?.isActive == true) return
        stopped = false
        loop = scope.launch {
            var backoff = 1_000L
            while (!stopped) {
                val closed = kotlinx.coroutines.CompletableDeferred<Unit>()
                _conn.value = ConnState.Connecting
                connect(onOpen = { backoff = 1_000L }, onClosed = { closed.complete(Unit) })
                closed.await()
                if (stopped) break
                if (unauthorized) {
                    _conn.value = ConnState.Unauthorized
                    break
                }
                _conn.value = ConnState.Offline
                delay(backoff)
                backoff = (backoff * 2).coerceAtMost(30_000L)
            }
        }
    }

    fun stop() {
        stopped = true
        ws?.close(1000, null)
        loop?.cancel()
        _conn.value = ConnState.Offline
    }

    /** Reconnect now (the app came back to the foreground, or the network changed). */
    fun kick() {
        if (_conn.value != ConnState.Online) ws?.cancel()
    }

    private fun connect(onOpen: () -> Unit, onClosed: () -> Unit) {
        val req = Request.Builder().url(api.wsUrl()).header("Authorization", "Bearer ${api.token}").build()
        ws = api.http.newWebSocket(req, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                onOpen()
                _conn.value = ConnState.Online
                phoneTools?.let { registerPhone(webSocket, it) }
                synchronized(chats) { chats.values.toList() }.forEach { it.resubscribe() }
                // Pins and mutes have no WebSocket event: read them on every (re)connect; the inbox too.
                scope.launch { loadPins(); loadInbox() }
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                runCatching { onMessage(text) }.onFailure { Log.w(TAG, "bad message", it) }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) = onClosed()
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.i(TAG, "ws closed: ${t.message}")
                if (response?.code == 401) unauthorized = true
                // The next handshake must not reuse a pooled connection that died with this one: OkHttp doesn't retry
                // a WebSocket upgrade on a stale connection, so every reconnect failed with "unexpected end of stream"
                // until the app restarted (seen when the network path under the hub, an adb / ssh forward, reset).
                api.http.connectionPool.evictAll()
                onClosed()
            }
        })
    }

    fun send(obj: JsonObject): Boolean = ws?.send(obj.toString()) ?: false

    private fun registerPhone(socket: WebSocket, tools: JsonArray) {
        socket.send(buildJsonObject {
            put("type", "register_phone"); put("device_name", deviceName); put("tools", tools)
        }.toString())
    }

    fun updatePhoneTools(tools: JsonArray) {
        phoneTools = tools
        ws?.let { if (_conn.value == ConnState.Online) registerPhone(it, tools) }
    }

    fun sendToolResult(callId: String, content: JsonArray, isError: Boolean) {
        send(buildJsonObject {
            put("type", "tool_result"); put("call_id", callId); put("content", content); put("is_error", isError)
        })
    }

    private fun onMessage(text: String) {
        val msg = JsonCodec.parseToJsonElement(text).jsonObject
        when (msg["type"]?.jsonPrimitive?.content) {
            "hello" -> {
                serverVersion = msg["server_version"]?.jsonPrimitive?.content
                features = (msg["features"] as? kotlinx.serialization.json.JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.content }?.toSet().orEmpty()
                terminalHandlers.values.forEach { it(TerminalEvent.Reconnected) }
            }
            "terminal" -> {
                val k = SessionKey(msg["machine_id"]?.jsonPrimitive?.content ?: return, msg["session_id"]?.jsonPrimitive?.content ?: return)
                val h = terminalHandlers[k] ?: return
                val err = msg["error"]?.jsonPrimitive?.content
                if (err != null) {
                    h(TerminalEvent.Ended(err, msg["unsupported"]?.jsonPrimitive?.booleanOrNull == true))
                } else {
                    val data = android.util.Base64.decode(msg["data"]?.jsonPrimitive?.content.orEmpty(), android.util.Base64.DEFAULT)
                    h(TerminalEvent.Frame(
                        msg["epoch"]?.jsonPrimitive?.content.orEmpty(), msg["seq"]?.jsonPrimitive?.intOrNull ?: 0,
                        msg["full"]?.jsonPrimitive?.booleanOrNull == true, msg["cols"]?.jsonPrimitive?.intOrNull ?: 0,
                        msg["rows"]?.jsonPrimitive?.intOrNull ?: 0, data,
                    ))
                }
            }
            "terminal_control" -> {
                val k = SessionKey(msg["machine_id"]?.jsonPrimitive?.content ?: return, msg["session_id"]?.jsonPrimitive?.content ?: return)
                terminalHandlers[k]?.invoke(TerminalEvent.Control(msg["state"]?.jsonPrimitive?.content.orEmpty(), msg["error"]?.jsonPrimitive?.content))
            }
            "ping" -> send(buildJsonObject { put("type", "pong") })
            "machines" -> _machines.value = decode(msg["machines"]) ?: emptyList()
            "sessions" -> {
                val mid = msg["machine_id"]?.jsonPrimitive?.content ?: return
                val list: List<Session> = decode(msg["sessions"]) ?: emptyList()
                val before = _sessions.value[mid].orEmpty().associateBy { it.sessionId }
                _sessions.update { it + (mid to list) }
                list.forEach { s ->
                    val old = before[s.sessionId]
                    if (old == null || old.state != s.state || old.rev != s.rev) _events.tryEmit(HubEvent.SessionChanged(old, s))
                    if (old?.rev != s.rev) chat(s.key, create = false)?.onRev()
                }
            }
            "session" -> {
                val s: Session = decode(msg["session"]) ?: return
                var old: Session? = null
                _sessions.update { map ->
                    val list = map[s.machineId].orEmpty()
                    old = list.firstOrNull { it.sessionId == s.sessionId }
                    map + (s.machineId to (if (old == null) list + s else list.map { if (it.sessionId == s.sessionId) s else it }))
                }
                _events.tryEmit(HubEvent.SessionChanged(old, s))
            }
            "revs" -> {
                val mid = msg["machine_id"]?.jsonPrimitive?.content ?: return
                val revs = msg["revs"]?.jsonObject ?: return
                _sessions.update { map ->
                    val list = map[mid] ?: return@update map
                    map + (mid to list.map { s -> revs[s.sessionId]?.jsonPrimitive?.content?.toLongOrNull()?.let { s.copy(rev = it) } ?: s })
                }
                revs.keys.forEach { sid -> chat(SessionKey(mid, sid), create = false)?.onRev() }
            }
            "interaction" -> {
                val ix: Interaction = decode(msg["interaction"]) ?: return
                var isNew = false
                _interactions.update { list ->
                    isNew = list.none { it.id == ix.id }
                    if (isNew) list + ix else list.map { if (it.id == ix.id) ix else it }
                }
                if (isNew) _events.tryEmit(HubEvent.NewInteraction(ix))
            }
            "interaction_resolved" -> {
                val id = msg["id"]?.jsonPrimitive?.content ?: return
                _interactions.update { list -> list.filterNot { it.id == id } }
                _events.tryEmit(HubEvent.Resolved(id))
            }
            "items" -> {
                val mid = msg["machine_id"]?.jsonPrimitive?.content ?: return
                val sid = msg["session_id"]?.jsonPrimitive?.content ?: return
                val items: List<Item> = decode(msg["items"]) ?: emptyList()
                chat(SessionKey(mid, sid), create = false)?.onPush(
                    items,
                    history = msg["history"]?.jsonPrimitive?.content,
                    fromSeq = msg["from_seq"]?.jsonPrimitive?.intOrNull,
                    toSeq = msg["to_seq"]?.jsonPrimitive?.intOrNull,
                    moreAfter = msg["more_after"]?.jsonPrimitive?.booleanOrNull == true,
                )
            }
            "delta" -> {
                val mid = msg["machine_id"]?.jsonPrimitive?.content ?: return
                val sid = msg["session_id"]?.jsonPrimitive?.content ?: return
                chat(SessionKey(mid, sid), create = false)?.onDelta(msg["text"]?.jsonPrimitive?.content.orEmpty())
            }
            "inbox" -> scope.launch { loadInbox() } // no content: the app pulls the list
            "tool_call" -> {
                val id = msg["call_id"]?.jsonPrimitive?.content ?: return
                val name = msg["name"]?.jsonPrimitive?.content ?: return
                val args = (msg["args"] as? JsonObject) ?: JsonObject(emptyMap())
                _events.tryEmit(HubEvent.ToolCall(id, name, args))
            }
        }
    }

    private inline fun <reified T> decode(e: JsonElement?): T? =
        e?.let { runCatching { JsonCodec.decodeFromJsonElement<T>(it) }.onFailure { t -> Log.w(TAG, "decode", t) }.getOrNull() }

    fun session(k: SessionKey): Session? = _sessions.value[k.machineId]?.firstOrNull { it.sessionId == k.sessionId }

    /** The item store of a session; created (and subscribed) on first use by a chat screen. */
    fun chat(k: SessionKey): ChatStore = chat(k, create = true)!!

    private fun chat(k: SessionKey, create: Boolean): ChatStore? = synchronized(chats) {
        chats[k] ?: if (create) ChatStore(this, k, scope).also { chats[k] = it } else null
    }

    fun release(k: SessionKey) {
        synchronized(chats) { chats.remove(k) }?.close()
    }

    /** Pulls everything once (after pairing, or when the socket is down and the user pulls to refresh). */
    suspend fun refresh() {
        runCatching { _machines.value = api.machines() }
        runCatching { _sessions.value = api.sessions().groupBy { it.machineId } }
        runCatching { _interactions.value = api.interactions() }
        loadPins()
    }

    private suspend fun loadPins() {
        runCatching { _pins.value = api.pins() }
        runCatching { _mutes.value = api.mutes() }
    }

    /** Panes of the agents running on a machine now: taken before a start, to tell the new one apart afterwards. */
    fun livePanes(machine: String): Set<String> =
        _sessions.value[machine].orEmpty().filter { it.isLive }.mapNotNull { it.paneId }.toSet()

    /**
     * The session a start brought up although the hub reported a failure: live, in that directory, in a pane that
     * wasn't there before the start. (Not by time: a session with no transcript yet has no updated_at.)
     */
    fun startedIn(machine: String, cwd: String, panesBefore: Set<String>): Session? =
        _sessions.value[machine].orEmpty()
            .filter { it.isLive && it.cwd.trimEnd('/') == cwd.trimEnd('/') && it.paneId != null && it.paneId !in panesBefore }
            .maxByOrNull { it.paneId!! }

    /** Marks agents seen (the hub stops counting them as done-unseen); the next sessions push shows it. */
    suspend fun markSeen(list: List<Session>) {
        list.forEach { s -> runCatching { api.seen(s.key) } }
    }

    fun isMuted(s: Session): Boolean = _mutes.value.mutes(s.pinKey(), s.spacePinKey())

    /** Pins / unpins an agent: shown at once, saved on the hub, put back if saving fails. */
    suspend fun togglePin(s: Session) {
        val before = _pins.value
        _pins.value = before.toggling(s.pinKey())
        runCatching { _pins.value = api.setPins(_pins.value) }.onFailure { _pins.value = before; throw it }
    }

    /** Pins / unpins a whole space ("machine/space name"). */
    suspend fun toggleSpacePin(g: SpaceGroup) {
        val before = _pins.value
        val k = g.pinKey
        _pins.value = before.copy(spaces = if (k in before.spaces) before.spaces - k else listOf(k) + before.spaces)
        runCatching { _pins.value = api.setPins(_pins.value) }.onFailure { _pins.value = before; throw it }
    }

    /** Mutes / unmutes a whole space; agents turned back on inside it keep their notifications. */
    suspend fun toggleSpaceMute(g: SpaceGroup) {
        val before = _mutes.value
        val k = g.pinKey
        _mutes.value = before.copy(spaces = if (k in before.spaces) before.spaces - k else listOf(k) + before.spaces)
        runCatching { _mutes.value = api.setMutes(_mutes.value) }.onFailure { _mutes.value = before; throw it }
    }

    /** Renames the herdr workspace; the hub moves its pin / mute keys to the new name, so read them again. */
    suspend fun renameSpace(g: SpaceGroup, name: String) {
        api.renameSpace(g.machineId, g.workspaceId ?: error("这个空间没有 herdr 的 workspace id"), name)
        loadPins()
    }

    /** Mutes / unmutes one agent on top of its space's setting. */
    suspend fun toggleMute(s: Session) {
        val before = _mutes.value
        _mutes.value = before.togglingMute(s.pinKey(), s.spacePinKey())
        runCatching { _mutes.value = api.setMutes(_mutes.value) }.onFailure { _mutes.value = before; throw it }
    }

    /** Resolves and drops the card at once (the hub's interaction_resolved follows). */
    suspend fun resolve(ix: Interaction, r: Resolution) {
        api.resolve(ix.id, r)
        _interactions.update { list -> list.filterNot { it.id == ix.id } }
    }

    companion object {
        const val TAG = "JarvisHub"
    }
}
