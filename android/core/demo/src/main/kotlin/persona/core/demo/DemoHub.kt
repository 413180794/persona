package persona.core.demo

import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.*
import java.time.Instant
import java.util.Base64
import java.util.UUID

/**
 * What the demo hub says, in the app's language ([DemoText.from] reads the module's strings; the defaults are the
 * English ones, for tests without resources).
 */
data class DemoText(
    val notSent: String = "Nothing is really sent in demo mode",
    val missing: String = "Demo mode doesn’t have this data",
    val reply: String = "This is demo mode: the message didn’t really go to a computer, and the agents here are demo data. " +
        "Connect your own hub to let agents carry on.",
    val scopeChanged: String = "The connection changed",
    val emptyMessage: String = "The message can’t be empty",
    val idUsed: String = "This message id is already used",
    val newChat: String = "New chat",
) {
    companion object {
        fun from(resources: android.content.res.Resources) = DemoText(
            resources.getString(R.string.demo_not_sent), resources.getString(R.string.demo_missing),
            resources.getString(R.string.demo_reply), resources.getString(R.string.demo_scope_changed),
            resources.getString(R.string.demo_empty_message), resources.getString(R.string.demo_id_used),
            resources.getString(R.string.demo_new_chat),
        )
    }
}

/** Local transports only: no URL, credentials, HTTP client or real transport can be supplied. */
class DemoHub(
    asset: String,
    parentScope: CoroutineScope,
    private val now: () -> Instant = Instant::now,
    val hubScope: HubScope = HubScope("demo", UUID.randomUUID().toString()),
    private val words: DemoText = DemoText(),
) : ChatTransport, MessageTransport, InboxAssistantTransport, HubConnection, AutoCloseable {
    companion object {
        private const val pageSize = 80
    }

    private val data = DemoData.load(asset, now())
    private val machineList = decode<List<Machine>>(data.getValue("machines"))
    private var sessionList = decode<List<Session>>(data.getValue("sessions"))
    private val itemLists = data.getValue("items").jsonObject.mapValues { (_, value) ->
        decode<List<Item>>(value).sortedBy { it.seq }
    }.toMutableMap()
    private var pending = decode<List<Interaction>>(data.getValue("interactions"))
    private val subscriptions = mutableSetOf<SessionKey>()
    private val accepted = mutableMapOf<String, MessageRequest>()
    private var main = data.getValue("assistant").jsonObject.text("main")
    private val history = "demo-${hubScope.pairingGeneration}"
    private val lock = Mutex()
    private val job = SupervisorJob(parentScope.coroutineContext[Job])
    private val worker = CoroutineScope(parentScope.coroutineContext + job)
    private val mutableState = MutableStateFlow(ConnectionState.Online)
    override val state = mutableState.asStateFlow()
    private val ready = MutableStateFlow(true)
    override val readyToSend = ready.asStateFlow()
    private val eventStream = MutableSharedFlow<ServerEvent>(extraBufferCapacity = 64)
    override val events = eventStream.asSharedFlow()
    private val noticeQueue = Channel<String>(Channel.UNLIMITED)
    /** Collect once at the composition root and show an informational snackbar in either theme. */
    val notices: Flow<String> = noticeQueue.receiveAsFlow()
    override val scope: String = hubScope.path
    override val supportsClientOps = true
    private val currentTransport = MutableStateFlow<InboxAssistantTransport?>(this)
    val inboxConnection: InboxAssistantConnection = object : InboxAssistantConnection {
        override val current = currentTransport.asStateFlow()
    }

    init {
        require(hubScope.hubId == "demo") { "Demo requires an isolated scope" }
        job.invokeOnCompletion { finish() }
    }

    private inline fun <reified T> decode(value: JsonElement): T = JsonCodec.decodeFromJsonElement(value)
    private fun checkOpen() { if (!job.isActive) throw CancellationException("Demo closed") }
    private suspend fun <T> locked(block: suspend () -> T): T = lock.withLock { checkOpen(); block() }
    private fun missing(): Nothing = throw HubFailure(
        HubError.Server, words.missing, status = 404, code = "demo_missing", retryable = false, delivered = "no",
    )
    private fun notice() { noticeQueue.trySend(words.notSent) }
    private fun refuse(): Nothing {
        checkOpen()
        notice()
        throw HubFailure(HubError.Server, words.notSent, code = "demo", retryable = false, delivered = "no")
    }
    private fun session(key: SessionKey): Session = sessionList.firstOrNull { it.key == key } ?: missing()
    private fun list(key: SessionKey): List<Item> {
        session(key)
        return itemLists[key.sessionId].orEmpty()
    }

    override suspend fun machines(): List<Machine> = locked { machineList }
    override suspend fun sessions(): List<Session> = locked { sessionList }
    override suspend fun interactions(): List<Interaction> = locked { pending }

    override suspend fun items(session: SessionKey, query: ItemsQuery): ItemsResponse = locked { page(session, query) }

    private fun page(key: SessionKey, query: ItemsQuery): ItemsResponse {
        val all = list(key)
        val before = query.beforeSeq
        val after = query.afterSeq
        val from = query.fromSeq
        val matches = all.filter {
            (before == null || it.seq < before) &&
                (after == null || it.seq > after) &&
                (from == null || it.seq in from..query.throughSeq!!)
        }
        val forward = query.afterSeq != null || query.fromSeq != null
        val selected = if (forward) matches.take(pageSize) else matches.takeLast(pageSize)
        return ItemsResponse(
            items = selected, history = history,
            fromSeq = query.afterSeq ?: selected.firstOrNull()?.seq,
            toSeq = selected.lastOrNull()?.seq ?: query.afterSeq,
            hasMore = !forward && matches.size > selected.size,
            moreAfter = forward && matches.size > selected.size,
        )
    }

    override suspend fun fullItem(session: SessionKey, itemId: String): Item = locked {
        val original = list(session).firstOrNull { it.id == itemId } ?: missing()
        data.getValue("full").jsonObject[itemId]?.let { decode<Item>(it) } ?: original
    }

    override suspend fun image(session: SessionKey, itemId: String): ByteArray = locked {
        val image = list(session).firstOrNull { it.id == itemId }?.image ?: missing()
        image.data.takeIf { it.isNotEmpty() }?.let { Base64.getDecoder().decode(it) } ?: missing()
    }

    override suspend fun resolve(id: String, resolution: Resolution) = locked {
        val interaction = pending.firstOrNull { it.id == id } ?: throw InteractionGoneException()
        // Only the local card and transcript change; no answer is forwarded or persisted.
        notice()
        pending = pending.filterNot { it.id == id }
        eventStream.emit(InteractionResolvedEvent(id))
        append(interaction.sessionKey, null)
    }

    override suspend fun send(request: MessageRequest): MessageTransportResult = locked {
        if (request.scope != hubScope) return@locked MessageTransportResult.HubError(
            code = "scope_changed", delivered = "no", error = words.scopeChanged,
        )
        if (request.text.isBlank() || request.clientMessageId.isBlank()) return@locked MessageTransportResult.HubError(
            code = "invalid_request", delivered = "no", error = words.emptyMessage,
        )
        val previous = accepted[request.clientMessageId]
        if (previous != null) return@locked if (previous == request) MessageTransportResult.Accepted else
            MessageTransportResult.HubError(code = "op_conflict", delivered = "no", error = words.idUsed)
        if (sessionList.none { it.key == request.session }) return@locked MessageTransportResult.HubError(
            code = "demo_missing", delivered = "no", error = words.missing,
        )
        notice()
        append(request.session, request.text, request.clientMessageId)
        accepted[request.clientMessageId] = request
        MessageTransportResult.Accepted
    }

    /**
     * The user's message keeps its client message id as its item id, as the hub records it: the chat hides the outbox
     * copy once the transcript has that id, so a random id left a second "Delivered, waiting for the transcript" bubble for ever.
     */
    private suspend fun append(key: SessionKey, text: String?, id: String? = null) {
        val old = list(key)
        val previousSeq = old.lastOrNull()?.seq ?: 0
        val timestamp = now().toString()
        val newItems = buildList {
            if (text != null) add(Item(id ?: UUID.randomUUID().toString(), previousSeq + 1, timestamp, "user", "text", text))
            add(Item(UUID.randomUUID().toString(), previousSeq + size + 1, timestamp, "assistant", "text", words.reply))
        }
        itemLists[key.sessionId] = old + newItems
        sessionList = sessionList.map {
            if (it.key != key) it else {
                val waiting = pending.count { interaction -> interaction.sessionKey == key }
                it.copy(status = if (waiting > 0) "blocked" else "done", state = if (waiting > 0) "needs_you" else "unread",
                    updatedAt = timestamp, stateSince = timestamp, lastText = words.reply,
                    pendingInteractions = waiting, rev = (it.rev ?: 0) + newItems.size,
                    queued = emptyList(), background = emptyList(), loop = null)
            }
        }
        if (key in subscriptions) eventStream.emit(ItemsEvent(key.machineId, key.sessionId, newItems,
            history = history, fromSeq = previousSeq, toSeq = newItems.last().seq))
        publishSessions(key.machineId)
        eventStream.emit(RevsEvent(key.machineId, mapOf(key.sessionId to (session(key).rev ?: 0))))
    }

    override suspend fun inbox(): JsonObject = locked { buildJsonObject { put("items", data.getValue("inbox")) } }
    override suspend fun setDone(itemId: String, done: Boolean): JsonObject = refuse()
    override suspend fun reply(itemId: String?, feedId: String?, text: String, clientId: String): JsonObject = refuse()

    override suspend fun messages(feedId: String, around: Long?, before: String?, after: String?): JsonObject = locked {
        val messages = data.getValue("lark").jsonObject[feedId]?.jsonArray ?: missing()
        // The packaged conversations fit in one page; around therefore includes both sides of the anchor.
        val selected = messages.filter { message ->
            val position = message.jsonObject.text("position").toLong()
            (before == null || position < before.toLong()) && (after == null || position > after.toLong())
        }
        buildJsonObject {
            put("messages", JsonArray(selected))
            put("has_more", false)
            put("has_more_before", false)
            put("has_more_after", false)
            put("next_cursor", "")
        }
    }

    override suspend fun assistant(): JsonObject = locked {
        JsonObject(data.getValue("assistant").jsonObject + mapOf(
            "main" to JsonPrimitive(main),
            "conversations" to JsonArray(sessionList.filter { it.assistant }.map { JsonCodec.encodeToJsonElement(it) }),
        ))
    }

    override suspend fun createConversation(main: Boolean, launcher: String): JsonObject = createConversation(main)

    override suspend fun createConversation(main: Boolean): JsonObject = locked {
        val machine = data.getValue("assistant").jsonObject.text("machine_id")
        val newSession = Session(machineId = machine, sessionId = "demo-${UUID.randomUUID()}", title = words.newChat,
            status = "idle", state = "idle", assistant = true, agent = "claude", launcher = "claude",
            updatedAt = now().toString(), startedByPersona = true, rev = 0)
        sessionList = listOf(newSession) + sessionList
        itemLists[newSession.sessionId] = emptyList()
        if (main) this.main = newSession.sessionId
        publishSessions(machine)
        buildJsonObject { put("session", JsonCodec.encodeToJsonElement(newSession)) }
    }

    override suspend fun pins(): JsonObject = locked { data.getValue("pins").jsonObject }
    override suspend fun mutes(): JsonObject = locked {
        buildJsonObject { listOf("spaces", "sessions", "allowed").forEach { put(it, JsonArray(emptyList())) } }
    }
    override suspend fun media(ref: String): ByteArray = locked { missing() }

    override fun reconnect() {
        if (!job.isActive) return
        worker.launch { locked {
            eventStream.emit(Hello(serverVersion = "demo", hubId = hubScope.hubId, features = listOf("client_ops")))
            eventStream.emit(MachinesEvent(machineList))
            machineList.forEach { publishSessions(it.id) }
            pending.forEach { eventStream.emit(InteractionEvent(it)) }
            subscriptions.forEach { publishBacklog(it, null) }
        } }
    }

    override fun send(message: JsonObject): Boolean {
        if (!job.isActive) return false
        val type = message.text("type")
        if (type !in setOf("subscribe", "unsubscribe", "seen", "ping", "client_hello")) {
            // Fail closed, including phone registration, terminal keys and future write commands.
            notice()
            return false
        }
        val key = SessionKey(message.text("machine_id"), message.text("session_id"))
        if (type in setOf("subscribe", "unsubscribe", "seen") &&
            (key.machineId.isBlank() || key.sessionId.isBlank())) return false
        worker.launch { locked {
            if (type in setOf("subscribe", "unsubscribe", "seen") && sessionList.none { it.key == key }) {
                noticeQueue.trySend(words.missing)
                return@locked
            }
            when (type) {
                "subscribe" -> {
                    subscriptions.add(key)
                    publishBacklog(key, (message["since_seq"] as? JsonPrimitive)?.intOrNull?.coerceAtLeast(0))
                }
                "unsubscribe" -> subscriptions.remove(key)
                "seen" -> {
                    sessionList = sessionList.map { if (it.key == key) it.copy(
                        seenAt = now().toString(), state = if (it.state == "unread") "idle" else it.state,
                    ) else it }
                    publishSessions(key.machineId)
                }
                "ping" -> eventStream.emit(Pong(message.text("id")))
                "client_hello" -> eventStream.emit(ClientHelloAck(Heartbeat(interval = 30, pongTimeout = 10)))
            }
        } }
        return true
    }

    private suspend fun publishBacklog(key: SessionKey, since: Int?) {
        val page = page(key, ItemsQuery(afterSeq = since))
        eventStream.emit(ItemsEvent(key.machineId, key.sessionId, page.items, history,
            fromSeq = page.fromSeq, toSeq = page.toSeq, backlog = true,
            hasMore = if (since == null) page.hasMore else null, moreAfter = page.moreAfter))
    }

    private suspend fun publishSessions(machineId: String) {
        eventStream.emit(SessionsEvent(machineId, sessionList.filter { it.machineId == machineId }))
    }

    override fun close() {
        job.cancel()
        finish()
    }

    private fun finish() {
        ready.value = false
        mutableState.value = ConnectionState.WaitingForNetwork
        currentTransport.value = null
        noticeQueue.close()
    }
}

private fun JsonObject.text(key: String): String = (this[key] as? JsonPrimitive)?.contentOrNull.orEmpty()
