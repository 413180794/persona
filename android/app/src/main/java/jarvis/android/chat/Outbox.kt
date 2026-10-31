package jarvis.android.chat

import android.content.Context
import android.util.Log
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.HubException
import jarvis.android.data.ImagePayload
import jarvis.android.data.Item
import jarvis.android.data.JsonCodec
import jarvis.android.data.SessionKey
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonPrimitive
import java.io.File
import java.io.IOException
import java.util.UUID

/** A message typed on the phone that the transcript doesn't show yet. */
data class PendingMessage(
    val clientMsgId: String,
    val text: String,
    val phase: Phase = Phase.SENDING,
    /** Why it failed / was refused (Chinese, for the bubble). */
    val error: String? = null,
    /** Error body fields of a refusal (`code`, `draft`, `clearable`, `delivered`). */
    val fields: Map<String, String> = emptyMap(),
    val createdAt: Long = System.currentTimeMillis(),
    val images: List<ImagePayload> = emptyList(),
) {
    enum class Phase {
        /** On its way to the hub. */
        SENDING,
        /** Typed into the pane; waiting for the transcript to show it. */
        SENT,
        /** A hub error: can be retried with the same id. */
        FAILED,
        /** 409 dialog_open / input_draft: nothing was typed. `fields["clearable"]` ⇒ 「清空并发送」. */
        REFUSED,
        /** 502 delivered:unknown: may have arrived; never retried by itself. */
        UNKNOWN,
        /** Written while the hub was out of reach (or the request died on the network): 「连上后自动发送」, kept on disk. */
        WAITING,
    }

    val failed: Boolean get() = phase == Phase.FAILED || phase == Phase.REFUSED || phase == Phase.UNKNOWN
    val canClearDraft: Boolean get() = phase == Phase.REFUSED && !fields["clearable"].isNullOrEmpty()
    val goingOut: Boolean get() = phase == Phase.WAITING || phase == Phase.SENDING

    companion object {
        /** The transcript's copy matches what was typed (whitespace and paste wrappers differ). */
        fun same(itemText: String, typed: String): Boolean {
            val a = itemText.trim()
            val b = typed.trim()
            return a == b || (b.isNotEmpty() && a.contains(b))
        }
    }
}

/** One message of the outbox on disk. */
@Serializable
internal data class OutboxEntry(
    val machineId: String,
    val sessionId: String,
    val clientMsgId: String,
    val text: String,
    val images: List<ImagePayload> = emptyList(),
    val createdAt: Long = 0,
)

/**
 * Messages sent from this phone until the transcript has them (iOS `Outbox`): drawn at once as pending bubbles, sent
 * in the background. While the hub is out of reach nothing is requested: the bubble waits with a clock and goes out by
 * itself, in the order written, once connected and caught up ([ConnectionWatch]). Waiting messages are kept on disk;
 * a resend carries the same client_msg_id, so the node never types one twice.
 */
object Outbox {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val _all = MutableStateFlow<Map<SessionKey, List<PendingMessage>>>(emptyMap())
    val all: StateFlow<Map<SessionKey, List<PendingMessage>>> = _all.asStateFlow()
    private var file: File? = null
    private val flushing = Mutex()

    fun of(key: SessionKey): Flow<List<PendingMessage>> = _all.map { it[key].orEmpty() }.distinctUntilChanged()

    /** Messages still going out (the connection details count them). */
    val waitingCount: Int get() = _all.value.values.sumOf { l -> l.count { it.goingOut } }

    fun init(ctx: Context) {
        val f = File(ctx.filesDir, "outbox.json")
        file = f
        val list = runCatching { JsonCodec.decodeFromString(ListSerializer(OutboxEntry.serializer()), f.readText()) }.getOrNull() ?: return
        _all.value = restore(list)
    }

    internal fun restore(list: List<OutboxEntry>): Map<SessionKey, List<PendingMessage>> =
        list.groupBy { SessionKey(it.machineId, it.sessionId) }.mapValues { (_, es) ->
            es.map { PendingMessage(it.clientMsgId, it.text, PendingMessage.Phase.WAITING, createdAt = it.createdAt, images = it.images) }
        }

    internal fun entries(all: Map<SessionKey, List<PendingMessage>>): List<OutboxEntry> = all.flatMap { (k, l) ->
        l.filter { it.goingOut }.map { OutboxEntry(k.machineId, k.sessionId, it.clientMsgId, it.text, it.images, it.createdAt) }
    }

    private fun save() {
        val f = file ?: return
        val snapshot = entries(_all.value)
        scope.launch {
            runCatching {
                if (snapshot.isEmpty()) f.delete() else f.writeText(JsonCodec.encodeToString(ListSerializer(OutboxEntry.serializer()), snapshot))
            }.onFailure { Log.w(Hub.TAG, "outbox save", it) }
        }
    }

    private fun online(hub: Hub) = hub.conn.value == ConnState.Online

    /** Sends [text] (with [images]) to [key]; returns its client_msg_id. Offline it waits instead. */
    fun send(hub: Hub, key: SessionKey, text: String, images: List<ImagePayload> = emptyList()): String {
        val id = UUID.randomUUID().toString()
        val offline = !online(hub) || ConnectionWatch.catchingUp.value
        val p = PendingMessage(id, text, if (offline) PendingMessage.Phase.WAITING else PendingMessage.Phase.SENDING, images = images)
        edit(key) { it + p }
        save()
        if (!offline) doSend(hub, key, p, null)
        return id
    }

    /** Retries a failed / refused message with the same id; [clearDraft] = 「清空并发送」. */
    fun retry(hub: Hub, key: SessionKey, id: String, clearDraft: Boolean = false) {
        val p = find(key, id) ?: return
        val clearable = if (clearDraft) p.fields["clearable"] else null
        set(key, id) { it.copy(phase = PendingMessage.Phase.SENDING, error = null, fields = emptyMap()) }
        doSend(hub, key, p, clearable)
    }

    /** 「现在重试」 on a waiting message's clock. */
    fun sendNow(hub: Hub, key: SessionKey, id: String) {
        val p = find(key, id) ?: return
        if (!online(hub)) { hub.kick(); return }
        set(key, id) { it.copy(phase = PendingMessage.Phase.SENDING, error = null) }
        doSend(hub, key, p, null)
    }

    /** 删除 / 不发了. */
    fun discard(key: SessionKey, id: String) {
        edit(key) { l -> l.filterNot { it.clientMsgId == id } }
        save()
    }

    /**
     * New transcript items of [key]: the user's messages that match a pending one drop it. Only items written after the
     * message (with a minute of clock skew) count, so an earlier 「继续」 doesn't swallow a new one.
     */
    fun arrived(key: SessionKey, items: List<Item>) {
        val cur = _all.value[key] ?: return
        if (cur.isEmpty()) return
        val left = dropArrived(cur, items)
        if (left.size != cur.size) {
            edit(key) { l -> l.filter { p -> left.any { it.clientMsgId == p.clientMsgId } } }
            save()
        }
    }

    internal fun dropArrived(pending: List<PendingMessage>, items: List<Item>): List<PendingMessage> {
        val typed = items.filter { it.role == "user" && it.kind == "text" }
        if (typed.isEmpty()) return pending
        return pending.filterNot { p ->
            typed.any { item ->
                val t = item.time?.toEpochMilli()
                (t == null || t >= p.createdAt - 60_000) && PendingMessage.same(item.text.orEmpty(), p.text)
            }
        }
    }

    /**
     * Sends what waited, chat by chat and in the order written. A chat that isn't open first loads its latest page: a
     * message that did arrive before the link broke is in the transcript and drops out.
     */
    fun flush(hub: Hub) {
        if (_all.value.values.none { l -> l.any { it.phase == PendingMessage.Phase.WAITING } }) return
        scope.launch {
            if (!flushing.tryLock()) return@launch
            try {
                for (key in _all.value.keys.toList()) {
                    if (!online(hub)) break
                    if (_all.value[key].orEmpty().none { it.phase == PendingMessage.Phase.WAITING }) continue
                    runCatching { hub.api.items(key) }.onSuccess { arrived(key, it.items) }
                    for (m in _all.value[key].orEmpty().filter { it.phase == PendingMessage.Phase.WAITING }) {
                        if (!online(hub)) break
                        if (find(key, m.clientMsgId)?.phase != PendingMessage.Phase.WAITING) continue
                        set(key, m.clientMsgId) { it.copy(phase = PendingMessage.Phase.SENDING) }
                        if (sendNowAndWait(hub, key, m, null) == PendingMessage.Phase.WAITING) break // keep the order
                    }
                }
            } finally {
                flushing.unlock()
            }
        }
    }

    private fun doSend(hub: Hub, key: SessionKey, p: PendingMessage, clearDraft: String?) {
        scope.launch { sendNowAndWait(hub, key, p, clearDraft) }
    }

    private suspend fun sendNowAndWait(hub: Hub, key: SessionKey, p: PendingMessage, clearDraft: String?): PendingMessage.Phase {
        val r = runCatching { hub.api.send(key, p.text, p.clientMsgId, clearDraft, p.images) }
        val e = r.exceptionOrNull()
        val phase = if (e == null) PendingMessage.Phase.SENT else phaseOf(e)
        val he = e as? HubException
        val fields = he?.fields?.mapNotNull { (k, v) -> (v as? JsonPrimitive)?.content?.let { k to it } }?.toMap().orEmpty()
        set(key, p.clientMsgId) {
            it.copy(phase = phase, error = if (phase == PendingMessage.Phase.WAITING || e == null) null else he?.message ?: e.message, fields = fields)
        }
        save()
        // The socket looks fine but the request got no answer (a weak network): try again shortly.
        if (phase == PendingMessage.Phase.WAITING && online(hub)) scope.launch { delay(10_000); flush(hub) }
        return phase
    }

    /** What a failed send becomes (iOS: network trouble is not an error, the message waits). */
    internal fun phaseOf(e: Throwable): PendingMessage.Phase {
        val he = e as? HubException
        return when {
            e is CancellationException || e is IOException -> PendingMessage.Phase.WAITING
            he == null -> PendingMessage.Phase.FAILED
            he.status == 409 && (he.code == "dialog_open" || he.code == "input_draft") -> PendingMessage.Phase.REFUSED
            (he.fields?.get("delivered") as? JsonPrimitive)?.content == "unknown" -> PendingMessage.Phase.UNKNOWN
            else -> PendingMessage.Phase.FAILED
        }
    }

    fun clear() {
        _all.value = emptyMap()
        save()
    }

    private fun find(key: SessionKey, id: String) = _all.value[key]?.firstOrNull { it.clientMsgId == id }

    private fun set(key: SessionKey, id: String, f: (PendingMessage) -> PendingMessage) =
        edit(key) { l -> l.map { if (it.clientMsgId == id) f(it) else it } }

    private fun edit(key: SessionKey, f: (List<PendingMessage>) -> List<PendingMessage>) = _all.update { m ->
        val l = f(m[key].orEmpty())
        if (l.isEmpty()) m - key else m + (key to l)
    }
}
