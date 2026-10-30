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

enum class ConnState { Connecting, Online, Offline }

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

    private val _events = MutableSharedFlow<HubEvent>(extraBufferCapacity = 64)
    val events: SharedFlow<HubEvent> = _events.asSharedFlow()

    var serverVersion: String? = null
        private set

    /** Phone tools declared on every (re)connect; set before start() so the first hello registers them. */
    @Volatile var phoneTools: JsonArray? = null

    private val chats = HashMap<SessionKey, ChatStore>()
    private var ws: WebSocket? = null
    private var loop: Job? = null
    @Volatile private var stopped = false

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
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                runCatching { onMessage(text) }.onFailure { Log.w(TAG, "bad message", it) }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) = onClosed()
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.i(TAG, "ws closed: ${t.message}")
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
            "hello" -> serverVersion = msg["server_version"]?.jsonPrimitive?.content
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
