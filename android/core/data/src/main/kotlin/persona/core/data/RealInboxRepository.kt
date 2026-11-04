package persona.core.data

import persona.core.contracts.InboxEvent
import persona.core.contracts.LarkEvent
import persona.core.contracts.api.*
import persona.core.domain.inbox.InboxInteraction
import persona.core.domain.inbox.InboxItem
import persona.core.domain.sort.AgentPreferences
import persona.core.domain.sort.AgentSession
import persona.feature.inbox.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.*
import java.time.Instant
import java.io.IOException

enum class LarkWindowSyncState { Cached, Syncing, Complete, Incomplete, Failed }

@OptIn(ExperimentalCoroutinesApi::class)
class RealInboxRepository(
    private val connection: InboxAssistantConnection,
    private val journal: InboxReplyJournal,
    private val sessions: SessionsRepository,
    private val interactions: InteractionCenter,
    private val scope: CoroutineScope,
    private val snapshots: ListSnapshotStore? = null,
    private val images: ImageBytesCache = ImageBytesCache(),
    private val hubPreferences: HubPreferences? = null,
    private val hubConnection: HubConnection? = connection as? HubConnection,
    private val mainConversationSender: MainConversationSender? = null,
    private val actions: InboxActionsTransport? = null,
    private val lookupReply: suspend (ReplyTarget, String) -> InboxReplyReceipt? = { _, _ -> null },
    private val classifyReplyFailure: (Exception) -> OutboxState = { OutboxState.Unknown },
    private val sendThread: (suspend (String, ReplyRequest) -> JsonObject)? = null,
    /** The ordinary sessions' outbox (the one Agents uses): 交给 AI to an Agent queues its message there. */
    private val messageSender: MessageSender? = null,
    // Last, so existing callers that pass it as a trailing lambda keep compiling.
    private val download: suspend (InboxAssistantTransport, LarkPart.File) -> String,
) : persona.feature.inbox.InboxRepository {
    override val supportsThreadOutbox: Boolean get() = sendThread != null
    override val personaOutbox = mainConversationSender?.outbox ?: MutableStateFlow(emptyList())
    override val connectionState: Flow<ConnectionState> = hubConnection?.state ?: connection.current.map {
        if (it == null) ConnectionState.Connecting else ConnectionState.Online
    }
    private val current = MutableStateFlow(InboxSnapshot())
    override val snapshot = current.asStateFlow()
    override val entries = snapshot.map { state -> state.records.map {
        InboxEntry(it.item.id, it.item.title, it.item.preview, if (it.feedId != null) "lark" else "mail")
    } }.stateIn(scope, SharingStarted.Eagerly, emptyList())
    private val sync = MutableStateFlow<Map<String, LarkWindowSyncState>>(emptyMap())
    val syncStates: StateFlow<Map<String, LarkWindowSyncState>> = sync.asStateFlow()
    private val outbox = MutableStateFlow<List<InboxPendingReply>>(emptyList())
    private val versions = mutableMapOf<String, String>()
    private val refreshLock = Mutex()
    private val sendLock = Mutex()
    private val personaLock = Mutex()
    private val personaRequests = mutableMapOf<String, String>()
    // Access order is maintained when a window is opened.
    private val windows = LinkedHashMap<String, Window>(16, 0.75f, true)
    private var restoredScope: String? = null

    private suspend fun restore(transport: InboxAssistantTransport) {
        if (restoredScope == transport.scope) return
        quietly { snapshots.restore<JsonObject>("${transport.scope}:inbox")?.let { data ->
            val records = data.array("items").map { record(it.jsonObject, transport) }
            if (owns(transport)) current.update { it.copy(records = records, loading = false) }
        } }
        restoredScope = transport.scope
    }
    private val windowsLock = Any()

    private class Window(val transport: InboxAssistantTransport, val feedId: String, val around: Long?) {
        val state = MutableStateFlow(LarkConversation(loading = true))
        val lock = Mutex()
        var older: String? = null
        var newer: String? = null
        var tail = false
        var initialized = false
        var restored = false
        var users = 0
        var raw = emptyList<JsonObject>()
        var generation = 0L
    }

    init {
        (hubConnection as? OpenWindowReconciliationOwner)?.openWindowReconciliation
            ?.register(scope, ::reconcileOpened)
        if (hubPreferences != null) scope.launch {
            hubPreferences.preferences.collect { state ->
                current.update { it.copy(mutes = AgentPreferences(state.mutes.spaces, state.mutes.sessions, state.mutes.allowed)) }
            }
        }
        scope.launch {
            connection.current.collectLatest { transport ->
                refreshLock.withLock {
                    current.update { it.copy(records = emptyList(), loading = false, error = null,
                        contentSource = transport?.let(::LarkContentSource),
                        mutes = hubPreferences?.preferences?.value?.mutes?.let { m -> AgentPreferences(m.spaces, m.sessions, m.allowed) }
                            ?: AgentPreferences()) }
                    outbox.value = emptyList()
                    sync.value = emptyMap()
                    synchronized(windowsLock) { windows.entries.removeAll { it.value.transport !== transport } }
                    versions.clear()
                    restoredScope = null
                    if (transport != null) restore(transport)
                }
                if (transport != null) {
                    sendLock.withLock {
                        val saved = journal.update(transport.scope) { document ->
                            var restored = document.copy(outbox = document.outbox.map {
                                if (it.state == "sending") it.copy(state = "unknown") else it
                            })
                            document.drafts.filterKeys { it.startsWith("lark:thread-pending:") }.forEach { (key, draft) ->
                                if (draft.text.isNotBlank() && restored.outbox.none { it.request.clientId == draft.clientId }) {
                                    restored = restored.enqueue(InboxPendingReply(
                                        ReplyTarget.Lark("thread:${key.removePrefix("lark:thread-pending:")}"),
                                        ReplyRequest(draft.text, draft.clientId), state = "unknown"))
                                }
                            }
                            restored
                        }
                        if (owns(transport)) publishLocal(saved)
                    }
                    // One collector owns startup and reconnect recovery for this transport.
                    (hubConnection?.state ?: flowOf(ConnectionState.Online)).collectLatest { state ->
                        if (state == ConnectionState.Online) recoverReplies(transport)
                    }
                }
            }
        }
        scope.launch {
            combine(sessions.sessions, interactions.pending) { agents, waiting ->
                agents.map { AgentSession(it) } to waiting.map {
                    InboxInteraction(it.id, it.machineId, it.sessionId, it.createdAt,
                        agents.firstOrNull { agent -> agent.key == it.sessionKey }?.displayTitle.orEmpty(), it.summary, it.headline)
                }
            }.collect { (agents, waiting) -> current.update { it.copy(sessions = agents, interactions = waiting) } }
        }
        scope.launch {
            sessions.machines.collect { machines -> current.update { it.copy(machines = machines.associate { machine -> machine.id to machine.displayName }) } }
        }
        hubConnection?.let { hub ->
            scope.launch(start = CoroutineStart.UNDISPATCHED) {
                hub.state.collect { state ->
                    if (state == ConnectionState.Online) coroutineScope {
                        launch { quietly { refresh() } }
                        val active = synchronized(windowsLock) {
                            windows.values.filter { it.users > 0 && !it.state.value.verified }
                        }
                        active.forEach { window -> launch { quietly { load(window, older = false) } } }
                    } else invalidateWindows()
                }
            }
            scope.launch(start = CoroutineStart.UNDISPATCHED) {
                hub.events.collect { event ->
                    val feeds = when (event) {
                        is LarkEvent -> event.feedIds.toSet()
                        is InboxEvent -> null
                        else -> return@collect
                    }
                    val active = invalidateWindows(feeds)
                    coroutineScope {
                        launch { quietly { refresh() } }
                        active.forEach { window -> launch { quietly { load(window, older = false) } } }
                    }
                }
            }
        }
        // Polling remains a fallback for hubs without invalidation events.
        scope.launch {
            current.subscriptionCount.map { it > 1 }.distinctUntilChanged().collectLatest { visible ->
                if (visible) while (isActive) {
                    try { if (online()) refresh() } catch (e: CancellationException) { throw e } catch (_: Exception) { }
                    delay(15_000)
                }
            }
        }
    }

    override suspend fun refresh() = refreshLock.withLock {
        val transport = requireTransport()
        if (!online()) return@withLock
        restore(transport)
        current.update { it.copy(loading = true, error = null) }
        try {
            val data = transport.inbox().array("items").map { it.jsonObject }
            val mute = if (hubPreferences == null) transport.mutes() else null
            if (!owns(transport)) return@withLock
            val records = data.map { record(it, transport) }
            val newVersions = data.associate { it.string("id") to (it.obj("agent")?.toString().orEmpty()) }
            val saved = journal.update(transport.scope) { document ->
                document.copy(drafts = document.drafts.filter { (key, draft) ->
                    !key.startsWith("inbox:") || draft.version == null || newVersions[key.removePrefix("inbox:")] == draft.version
                })
            }
            if (!owns(transport)) return@withLock
            versions.clear(); versions.putAll(newVersions)
            outbox.value = saved.outbox
            current.update { it.copy(records = records.map { item -> withLocalStatus(item, saved) },
                mutes = mute?.let { m -> AgentPreferences(m.strings("spaces"), m.strings("sessions"), m.strings("allowed").toSet()) } ?: it.mutes,
                loading = false, error = null) }
            snapshots.save("${transport.scope}:inbox", buildJsonObject { put("items", JsonArray(data)) })
        } catch (e: CancellationException) { throw e }
        catch (e: Exception) {
            if (owns(transport)) current.update { it.copy(loading = false, error = "收件箱刷新失败，请重试") }
            throw e
        }
    }

    override suspend fun refreshNow() { actions?.collectNow(); refresh() }

    override suspend fun personaContext(itemId: String): String? {
        val transport = requireTransport()
        val key = "${transport.scope}:inbox-context:$itemId"
        val cached = snapshots.restore<JsonObject>(key)?.optional("context")
        if (!online() || actions == null) return cached
        return try {
            actions.context(itemId).takeIf { it.isNotBlank() }?.also { context ->
                if (!owns(transport)) throw CancellationException("Pairing changed")
                snapshots.save(key, buildJsonObject { put("context", context) })
            } ?: cached
        } catch (e: CancellationException) { throw e }
        catch (_: Exception) { cached }
    }

    override suspend fun setDone(itemId: String, done: Boolean) = refreshLock.withLock {
        val transport = requireTransport()
        val old = current.value.records.firstOrNull { it.item.id == itemId }
        current.update { it.copy(records = it.records.map { record ->
            if (record.item.id == itemId) record.copy(item = record.item.copy(doneAt = if (done) Instant.now().toString() else null)) else record
        }) }
        try {
            val result = transport.setDone(itemId, done)
            if (owns(transport)) replaceRecord(record(result.obj("item") ?: result, transport))
        } catch (e: Exception) {
            if (owns(transport) && old != null) replaceRecord(old)
            throw e
        }
    }

    override suspend fun setDone(items: List<InboxDoneTarget>, done: Boolean): InboxBulkResult {
        val result = refreshLock.withLock {
            val transport = requireTransport()
            val ids = items.map { it.id }.toSet()
            val old = current.value.records.filter { it.item.id in ids }
            val doneAt = if (done) Instant.now().toString() else null
            current.update { it.copy(records = it.records.map { record ->
                if (record.item.id in ids) record.copy(item = record.item.copy(doneAt = doneAt)) else record
            }) }
            try {
                val data = transport.setDone(items, done)
                val result = InboxBulkResult.from(data)
                if (owns(transport)) {
                    data.array("changed").forEach { replaceRecord(record(it.jsonObject, transport)) }
                    // Not moved here (new messages came in, gone, already so): back to the hub's state until the refresh.
                    old.filter { it.item.id !in result.changed }.forEach(::replaceRecord)
                }
                result
            } catch (e: Exception) {
                if (owns(transport)) old.forEach(::replaceRecord)
                throw e
            }
        }
        if (result.changedSince.isNotEmpty()) quietly { refresh() }
        return result
    }

    override suspend fun readDraft(target: ReplyTarget): ReplyDraft? {
        val transport = requireTransport()
        val document = journal.read(transport.scope)
        // A reply refused because its mail target changed must be confirmed again under a new identifier.
        fun restored(text: String, clientId: String) =
            if (document.outbox.any { it.request.clientId == clientId && it.state == "target_changed" }) ReplyDraft(text)
            else ReplyDraft(text, clientId)
        return document.drafts[target.storageKey()]?.let { restored(it.text, it.clientId).copy(ai = it.ai) }
            ?: document.outbox.lastOrNull { it.target == target && it.state != "sent" }?.let {
                restored(it.request.text, it.request.clientId)
            }
    }
    override suspend fun saveDraft(target: ReplyTarget, draft: ReplyDraft) {
        val transport = requireTransport()
        val version = refreshLock.withLock { (target as? ReplyTarget.Inbox)?.let { versions[it.itemId] } }
        val saved = journal.update(transport.scope) {
            it.copy(drafts = it.drafts + (target.storageKey() to InboxStoredDraft(draft.text, draft.clientId, version, draft.ai)))
        }
        if (owns(transport)) publishLocal(saved)
    }

    override suspend fun draftReply(itemId: String, instruction: String): String {
        val transport = requireTransport()
        val text = checkNotNull(actions) { "这个连接不能草拟回复" }.draft(itemId, instruction)
        if (!owns(transport)) throw CancellationException("Pairing changed")
        return text
    }

    override suspend fun handOff(request: InboxHandOffRequest) {
        require(request.itemId.isNotBlank() && request.text.isNotBlank() && request.operationId.isNotBlank())
        scope.coroutineContext.ensureActive()
        val machine = request.machineId
        val session = request.sessionId
        // Both outboxes commit before returning and deduplicate by the operation ID, so a retry is not queued twice.
        if (machine.isNullOrBlank() || session.isNullOrBlank()) checkNotNull(mainConversationSender) { "主对话发送服务尚未接入" }
            .sendToMain(request.text, request.operationId)
        else checkNotNull(messageSender) { "Agent 消息发送服务尚未接入" }
            .send(persona.core.contracts.SessionKey(machine, session), request.text, request.operationId)
    }

    override suspend fun reply(target: ReplyTarget, request: ReplyRequest) = sendLock.withLock {
        require(request.text.isNotBlank() && request.clientId.isNotBlank())
        val transport = requireTransport()
        val previous = journal.read(transport.scope).outbox.firstOrNull { it.request.clientId == request.clientId }
        require(previous == null || previous.request == request && (previous.target == target ||
            target is ReplyTarget.Lark && previous.feedId == target.feedId)) { "回复标识已被使用" }
        val actualTarget = previous?.target ?: target
        if (previous?.state == "sent") return@withLock
        val feed = previous?.feedId ?: when (actualTarget) {
            is ReplyTarget.Lark -> actualTarget.feedId.takeUnless { it.startsWith("thread:") }
            is ReplyTarget.Inbox -> current.value.records.firstOrNull { it.item.id == actualTarget.itemId }?.feedId
        }
        // Reconcile before retrying, under the same lock as sending. Text is never evidence of delivery.
        if (previous != null && online()) {
            if (previous.feedId == null && feed != null) {
                val linked = journal.update(transport.scope) { it.enqueue(previous.copy(feedId = feed)) }
                if (owns(transport)) publishLocal(linked)
            }
            reconcileReplyEchoes(transport, feed)
            if (journal.read(transport.scope).outbox.any { it.request.clientId == request.clientId && it.state == "sent" })
                return@withLock
        }
        if (previous != null && previous.outboxState !in setOf(OutboxState.Queued, OutboxState.WaitingForNetwork, OutboxState.Refused)) {
            if (online()) reconcilePending(transport, previous)
            if (replyStatus(actualTarget, request.clientId) == ReplyDelivery.Sent) return@withLock
            throw ReplyUnconfirmed()
        }
        val pending = InboxPendingReply(actualTarget, request, feed, state = if (online()) "sending" else "waitingForNetwork",
            serverId = previous?.serverId)
        val saved = journal.update(transport.scope) {
            it.enqueue(pending)
        }
        if (!owns(transport)) throw CancellationException("Pairing changed")
        publishLocal(saved)
        if (saved.outbox.any { it.request.clientId == request.clientId && it.state == "sent" }) return@withLock
        if (!online()) throw IOException("连上后自动发送")
        try {
            val mailTarget = request.mailTarget
            val response = if (actualTarget is ReplyTarget.Lark && actualTarget.feedId.startsWith("thread:"))
                checkNotNull(sendThread)(actualTarget.feedId.removePrefix("thread:"), request)
            else if (mailTarget != null && actualTarget is ReplyTarget.Inbox)
                transport.replyMail(actualTarget.itemId, request.text, request.clientId, mailTarget)
            else transport.reply((actualTarget as? ReplyTarget.Inbox)?.itemId,
                (actualTarget as? ReplyTarget.Lark)?.feedId, request.text, request.clientId)
            val result = response.obj("message") ?: response
            if (response.string("state") in setOf("pending", "unknown") || result.string("state") in setOf("pending", "unknown")) {
                val uncertain = journal.update(transport.scope) { it.transition(request.clientId, "unknown") }
                if (owns(transport)) publishLocal(uncertain)
                throw ReplyUnconfirmed()
            }
            val accepted = journal.update(transport.scope) { it.confirm(request.clientId,
                // A Feishu send answers {"ok":true,"message_id":…} (hub lark.go); older replies said "id".
                if (feed != null) result.optional("message_id") ?: result.optional("id") else null) }
            if (owns(transport)) {
                publishLocal(accepted)
                if (actualTarget is ReplyTarget.Inbox) {
                    val item = response.obj("item") ?: response
                    if (item.optional("id") != null) replaceRecord(record(item, transport))
                    else current.update { it.copy(records = it.records.map { record ->
                        if (record.item.id == actualTarget.itemId) record.copy(item = record.item.copy(doneAt = Instant.now().toString())) else record
                    }) }
                }
            }
        } catch (e: Exception) {
            withContext(NonCancellable) {
                if (owns(transport)) {
                    val state = when (e) {
                        is MailReplyTargetChanged -> "target_changed"
                        is CancellationException -> OutboxState.Unknown.wire
                        else -> classifyReplyFailure(e).wire
                    }
                    val failed = journal.update(transport.scope) { it.transition(request.clientId, state) }
                    publishLocal(failed)
                }
            }
            throw e
        }
    }

    fun larkOutbox(feedId: String): Flow<List<LarkMessage>> = outbox.map { pending ->
        pending.filter { it.feedId == feedId }.map { local ->
            LarkMessage(local.serverId ?: "local:${local.request.clientId}", "", "我", mine = true,
                parts = listOf(LarkPart.Text(local.request.text)), clientId = local.request.clientId,
                delivery = when (local.outboxState) {
                    OutboxState.Sent, OutboxState.Shown -> LarkDelivery.Sent
                    OutboxState.Sending -> LarkDelivery.Sending
                    OutboxState.Queued, OutboxState.WaitingForNetwork -> LarkDelivery.WaitingForNetwork
                    else -> LarkDelivery.Failed
                }, pendingDelivery = local.outboxState.delivery)
        }
    }

    suspend fun acceptLarkWindow(feedId: String, raw: List<JsonObject>) {
        val transport = requireTransport()
        val window = synchronized(windowsLock) {
            windows[feedId] ?: Window(transport, feedId, null).also { windows[feedId] = it }
        }
        window.raw = raw
        window.state.value = window.state.value.copy(messages = raw.map { message(it, window) })
        reconcileReplies(window, raw)
    }

    override fun conversation(feedId: String, startPosition: Long?): Flow<LarkConversation> =
        connection.current.flatMapLatest { transport ->
            if (transport == null) flowOf(LarkConversation(error = "请先配对 hub"))
            else {
                val window = synchronized(windowsLock) {
                    windows[feedId]?.takeIf { it.transport === transport && it.around == startPosition }
                        ?: Window(transport, feedId, startPosition).also { trimWindows(7); windows[feedId] = it }
                }
                channelFlow {
                    synchronized(windowsLock) { window.users++ }
                    try {
                    restoreWindow(window)
                    send(window.state.value)
                    launch {
                        while (isActive) {
                            try { if (online()) load(window, older = false) }
                            catch (e: CancellationException) { throw e }
                            catch (_: Exception) { }
                            delay(15_000)
                        }
                    }
                    combine(window.state, outbox) { state, pending ->
                        val locals = if (window.tail) pending.filter { it.feedId == feedId }.filterNot { local ->
                            state.messages.any { it.clientId == local.request.clientId || local.serverId != null && it.id == local.serverId }
                        }.map { local ->
                            LarkMessage(local.serverId ?: "local:${local.request.clientId}", "", "我", mine = true,
                                parts = listOf(LarkPart.Text(local.request.text)), clientId = local.request.clientId,
                                delivery = when (local.outboxState) { OutboxState.Sent -> LarkDelivery.Sent; OutboxState.Sending -> LarkDelivery.Sending; OutboxState.WaitingForNetwork -> LarkDelivery.WaitingForNetwork; else -> LarkDelivery.Failed }, pendingDelivery = local.outboxState.delivery)
                        } else emptyList()
                        state.copy(messages = state.messages + locals)
                    }.collect { send(it) }
                    } finally { synchronized(windowsLock) { window.users--; trimWindows() } }
                }
            }
        }

    /** Retain at most eight idle windows, evicting least recently opened first; policy, unmeasured. */
    private fun trimWindows(limit: Int = 8) {
        val idle = windows.entries.filter { it.value.users == 0 }
        idle.take((idle.size - limit).coerceAtLeast(0)).forEach { windows.remove(it.key) }
    }

    private fun windowKey(window: Window) = "${window.transport.scope}:lark:${window.feedId}:${window.around}"
    private suspend fun restoreWindow(window: Window) = window.lock.withLock {
        if (window.restored) return@withLock
        snapshots.restore<JsonObject>(windowKey(window))?.let { data ->
            val raw = data.array("messages").map { it.jsonObject }
            val positions = raw.mapNotNull { it.number("position") }
            if (window.around == null || positions.isNotEmpty() && window.around in positions.min()..positions.max()) {
                window.raw = raw
                window.older = data.optional("older_cursor")
                window.newer = data.optional("newer_cursor")
                window.tail = !data.flag("has_newer")
                window.initialized = true
                sync.update { it + (window.feedId to LarkWindowSyncState.Cached) }
                window.state.value = LarkConversation(messages = raw.map { message(it, window) }, loading = false,
                    hasOlder = data.flag("has_older"), hasNewer = !window.tail, verified = false)
            }
        }
        window.restored = true
    }

    /** Updating permits read-only reconciliation, while user writes remain gated by online(). */
    suspend fun reconcileOpened() = coroutineScope {
        val active = invalidateWindows()
        active.map { window -> async {
            restoreWindow(window)
            load(window, older = false, reconciling = true)
            window.state.first { it.verified }
        } }.awaitAll()
        Unit
    }

    private fun online() = hubConnection?.state?.value?.let { it == ConnectionState.Online } ?: true

    private fun invalidateWindows(feeds: Set<String>? = null): List<Window> = synchronized(windowsLock) {
        windows.values.filter { feeds == null || it.feedId in feeds }.onEach {
            it.generation++
            it.state.update { state -> state.copy(verified = false) }
            sync.update { states -> states + (it.feedId to LarkWindowSyncState.Cached) }
        }.filter { it.users > 0 }
    }

    override fun replyUpdates(target: ReplyTarget): Flow<Unit> = outbox.map { replies ->
        replies.filter { it.target == target || target is ReplyTarget.Lark && it.feedId == target.feedId }
    }.distinctUntilChanged().map { Unit }

    override suspend fun replyStatus(target: ReplyTarget, clientId: String): ReplyDelivery {
        val reply = journal.read(requireTransport().scope).outbox.firstOrNull {
            it.request.clientId == clientId && (it.target == target || target is ReplyTarget.Lark && it.feedId == target.feedId)
        } ?: return ReplyDelivery.Ready
        if (reply.state == "target_changed") return ReplyDelivery.Ready
        return when (reply.outboxState) {
            OutboxState.Sent, OutboxState.Shown -> ReplyDelivery.Sent
            OutboxState.Sending -> ReplyDelivery.Sending
            OutboxState.Queued -> ReplyDelivery.Queued
            OutboxState.WaitingForNetwork -> ReplyDelivery.WaitingForNetwork
            OutboxState.Refused -> ReplyDelivery.Refused
            OutboxState.Verifying -> ReplyDelivery.Verifying
            OutboxState.NeedsConfirmation -> ReplyDelivery.NeedsConfirmation
            else -> ReplyDelivery.Unknown
        }
    }

    override suspend fun verifyReply(target: ReplyTarget, clientId: String): ReplyDelivery = sendLock.withLock {
        val transport = requireTransport()
        val pending = journal.read(transport.scope).outbox.firstOrNull { it.request.clientId == clientId }
            ?: return@withLock ReplyDelivery.Ready
        require(pending.target == target || target is ReplyTarget.Lark && pending.feedId == target.feedId)
        if (pending.state !in setOf("sent", "target_changed") && online()) reconcilePending(transport, pending)
        if (journal.read(transport.scope).outbox.any { it.request.clientId == clientId && it.state == "target_changed" })
            throw MailReplyTargetChanged()
        replyStatus(target, clientId)
    }

    /** The public outbox calls this before opening its sending gate. Only GETs are allowed here. */
    suspend fun reconcilePendingReplies() = sendLock.withLock {
        val transport = requireTransport()
        quietly {
            for (pending in journal.read(transport.scope).outbox.filter { it.outboxState in uncertainReplyStates }) {
                if (!owns(transport)) break
                reconcilePending(transport, pending)
            }
        }
    }

    private suspend fun reconcilePending(transport: InboxAssistantTransport, pending: InboxPendingReply) {
        if (pending.state == "sent" || !owns(transport)) return
        val id = pending.request.clientId
        if (pending.outboxState !in uncertainReplyStates) return
        publishLocal(journal.update(transport.scope) { it.transition(id, OutboxState.Verifying.wire) })
        try {
            val receipt = lookupReply(pending.target, id)
            if (!owns(transport)) throw CancellationException("Pairing changed")
            if (receipt?.state == OutboxState.Sent) {
                publishLocal(journal.update(transport.scope) { it.confirm(id, receipt.serverId) })
                return
            }
            if (receipt?.state == OutboxState.Refused) {
                publishLocal(journal.update(transport.scope) { it.transition(id, receipt.state.wire, receipt.error) })
                return
            }
            reconcileReplyEchoes(transport, pending.feedId)
        } finally {
            withContext(NonCancellable) {
                if (owns(transport)) publishLocal(journal.update(transport.scope) { document ->
                    if (document.outbox.any { it.request.clientId == id && it.state == "verifying" })
                        document.transition(id, OutboxState.Unknown.wire) else document
                })
            }
        }
    }

    private val uncertainReplyStates = setOf(OutboxState.Sending, OutboxState.Unknown,
        OutboxState.Verifying, OutboxState.NeedsConfirmation)

    private suspend fun recoverReplies(transport: InboxAssistantTransport) {
        reconcilePendingReplies()
        quietly {
            val pending = journal.read(transport.scope).outbox.filter { it.outboxState in setOf(OutboxState.Queued, OutboxState.WaitingForNetwork) }
            if (pending.any { it.target is ReplyTarget.Inbox && it.feedId == null }) quietly { refresh() }
            for (pendingReply in pending) {
                if (!owns(transport) || !online()) break
                quietly { reply(pendingReply.target, pendingReply.request) }
            }
        }
    }

    private suspend fun reconcileReplyEchoes(transport: InboxAssistantTransport, feed: String?) {
        if (feed == null) return
        val window = synchronized(windowsLock) { windows[feed] } ?: Window(transport, feed, null)
        var page = transport.messages(feed)
        val seen = mutableSetOf<String>()
        var before: String? = null
        repeat(100) {
            if (!owns(transport)) throw CancellationException("Pairing changed")
            reconcileReplies(window, rows(page))
            if (!complete(page)) throw IncompleteWindow()
            if (!page.flag("has_older") || journal.read(transport.scope).outbox.none { it.feedId == feed && it.state != "sent" }) return
            before = checkedCursor(page, true, before, seen)
            page = transport.messages(feed, before = before)
        }
        throw IncompleteWindow()
    }

    override suspend fun loadOlder(feedId: String) { load(window(feedId), older = true) }
    override suspend fun refreshConversation(feedId: String) { load(window(feedId), older = false) }
    override suspend fun latestConversation(feedId: String) {
        val window = window(feedId)
        window.lock.withLock { if (owns(window.transport) && online()) replaceLatest(window) }
    }
    private fun window(feedId: String) = synchronized(windowsLock) { requireNotNull(windows[feedId]) }

    private class WindowGap : Exception()
    private class ReplyUnconfirmed : IOException()
    private class IncompleteWindow : Exception()
    private fun complete(page: JsonObject) = (page.optional("sync_state") ?: page.optional("syncState")) == "complete"
    private fun rows(page: JsonObject) = page.array("messages").map { it.jsonObject }
    private fun cursor(page: JsonObject, older: Boolean) = page.optional(if (older) "older_cursor" else "newer_cursor")
    private fun checkedCursor(page: JsonObject, older: Boolean, used: String?, seen: MutableSet<String>): String {
        val next = cursor(page, older) ?: throw WindowGap()
        if (next == used || !seen.add(next)) throw WindowGap()
        val number = next.toLongOrNull()
        val prior = used?.toLongOrNull()
        if (number != null && prior != null && if (older) number >= prior else number <= prior) throw WindowGap()
        return next
    }

    private suspend fun fetch(window: Window, around: Long? = null, before: String? = null, after: String? = null): JsonObject {
        val page = window.transport.messages(window.feedId, around, before, after)
        if (!owns(window.transport)) throw CancellationException("Pairing changed")
        // Positions are sparse. Only authoritative pages and their cursor chain establish coverage.
        if (!complete(page)) {
            reconcileReplies(window, rows(page))
            if ((page.optional("sync_state") ?: page.optional("syncState")) == "gap") throw WindowGap()
            if (window.raw.isEmpty() && before == null && after == null) {
                window.raw = rows(page)
                window.older = cursor(page, true)
                window.newer = cursor(page, false)
                window.tail = !page.flag("has_newer")
                window.initialized = true
                window.state.update { it.copy(messages = window.raw.map { row -> message(row, window) },
                    hasOlder = page.flag("has_older"), hasNewer = page.flag("has_newer"), verified = false) }
            }
            throw IncompleteWindow()
        }
        val afterPosition = after?.toLongOrNull()
        val beforePosition = before?.toLongOrNull()
        val positions = rows(page).mapNotNull { it.number("position") }
        if (afterPosition != null && positions.isNotEmpty() && positions.max() < afterPosition) throw WindowGap()
        if (beforePosition != null && positions.isNotEmpty() && positions.min() > beforePosition) throw WindowGap()
        return page
    }

    private suspend fun load(window: Window, older: Boolean, reconciling: Boolean = false) = window.lock.withLock {
        if (!owns(window.transport) || !(online() || reconciling && hubConnection?.state?.value == ConnectionState.Updating)) return@withLock
        if (older && (!window.state.value.verified || !window.state.value.hasOlder)) return@withLock
        val generation = window.generation
        sync.update { it + (window.feedId to LarkWindowSyncState.Syncing) }
        window.state.update { it.copy(loading = !window.initialized, loadingOlder = older,
            loadingNewer = !older && window.initialized, verified = false, error = null) }
        try {
            if (older) {
                val before = window.older ?: throw WindowGap()
                val page = fetch(window, before = before)
                if (page.flag("has_older")) checkedCursor(page, true, before, mutableSetOf(before))
                val incoming = rows(page)
                val edge = window.raw.firstOrNull()?.number("position")
                if (page.flag("has_older") && incoming.none { (it.number("position") ?: Long.MAX_VALUE) < (edge ?: Long.MAX_VALUE) }) throw WindowGap()
                install(window, window.raw + incoming, cursor(page, true), window.newer,
                    page.flag("has_older"), !window.tail, generation)
            } else {
                val firstPosition = window.raw.firstOrNull()?.number("position")
                val initial = fetch(window, around = firstPosition ?: window.around)
                var first = initial
                var last = initial
                val incoming = rows(initial).toMutableList()
                val beforeSeen = mutableSetOf<String>()
                val afterSeen = mutableSetOf<String>()
                var budget = 100
                var before: String? = null
                while (firstPosition != null && first.flag("has_older") &&
                    (incoming.mapNotNull { it.number("position") }.minOrNull() ?: Long.MAX_VALUE) > firstPosition) {
                    if (--budget == 0) throw WindowGap()
                    before = checkedCursor(first, true, before, beforeSeen)
                    first = fetch(window, before = before)
                    incoming.addAll(0, rows(first))
                }
                var after: String? = null
                while (last.flag("has_newer")) {
                    if (--budget == 0) throw WindowGap()
                    after = checkedCursor(last, false, after, afterSeen)
                    last = fetch(window, after = after)
                    incoming += rows(last)
                }
                val preserved = if (firstPosition == null) incoming else incoming.filter { (it.number("position") ?: Long.MAX_VALUE) >= firstPosition }
                install(window, preserved, if (firstPosition != null) window.older ?: cursor(first, true) else cursor(first, true),
                    cursor(last, false), if (firstPosition != null) window.state.value.hasOlder else first.flag("has_older"),
                    false, generation, reconciling = reconciling)
            }
        } catch (e: CancellationException) { throw e }
        catch (_: WindowGap) {
            try { replaceLatest(window, reconciling) }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) {
                sync.update { it + (window.feedId to LarkWindowSyncState.Incomplete) }
                window.state.update { it.copy(loading = false, loadingOlder = false,
                loadingNewer = false, verified = false, error = "消息记录尚未同步完整") } }
        } catch (_: IncompleteWindow) {
            sync.update { it + (window.feedId to LarkWindowSyncState.Incomplete) }
            window.state.update { it.copy(loading = false, loadingOlder = false, loadingNewer = false,
                verified = false, error = "消息记录尚未同步完整") }
        } catch (e: Exception) {
            sync.update { it + (window.feedId to LarkWindowSyncState.Failed) }
            window.state.update { it.copy(loading = false, loadingOlder = false, loadingNewer = false,
                verified = false, error = "消息加载失败，请重试") }
            throw e
        }
    }

    private suspend fun replaceLatest(window: Window, reconciling: Boolean = false) {
        val generation = window.generation
        val page = fetch(window)
        if (page.flag("has_newer")) throw WindowGap()
        install(window, rows(page), cursor(page, true), cursor(page, false), page.flag("has_older"), false, generation,
            replaced = true, reconciling = reconciling)
    }

    private suspend fun reconcileReplies(window: Window, raw: List<JsonObject>) {
        val saved = journal.update(window.transport.scope) { document ->
            var confirmed = document
            for (local in document.outbox.filter { it.feedId == window.feedId }) {
                val remote = raw.firstOrNull { it.optional("client_id") == local.request.clientId ||
                    it.optional("clientId") == local.request.clientId || local.serverId != null && it.string("id") == local.serverId }
                if (remote != null && (remote.optional("local_state") ?: remote.optional("localState")) !in setOf("sending", "pending", "failed", "unknown", "verifying", "queued", "waiting", "waitingForNetwork") && remote.optional("id") != null)
                    confirmed = confirmed.confirm(local.request.clientId, remote.string("id"))
            }
            confirmed
        }
        if (owns(window.transport)) publishLocal(saved)
    }

    private suspend fun install(window: Window, incoming: List<JsonObject>, older: String?, newer: String?,
        hasOlder: Boolean, hasNewer: Boolean, generation: Long, replaced: Boolean = false, reconciling: Boolean = false) {
        reconcileReplies(window, incoming)
        if (!owns(window.transport)) return
        window.raw = incoming.associateBy { it.string("id") }.values.sortedBy { it.number("position") ?: Long.MAX_VALUE }
        window.older = older
        window.newer = newer
        window.tail = !hasNewer
        window.initialized = true
        window.state.update { it.copy(
            title = current.value.records.firstOrNull { record -> record.feedId == window.feedId }?.item?.title ?: "飞书会话",
            messages = window.raw.map { row -> message(row, window) }, loading = false, loadingOlder = false, loadingNewer = false,
            hasOlder = hasOlder, hasNewer = hasNewer, verified = generation == window.generation &&
                (online() || reconciling && hubConnection?.state?.value == ConnectionState.Updating),
            windowRevision = it.windowRevision + if (replaced) 1 else 0,
            error = if (replaced) "有更多新消息，已切换到最新消息" else null) }
        sync.update { it + (window.feedId to if (window.state.value.verified) LarkWindowSyncState.Complete else LarkWindowSyncState.Cached) }
        if (!window.state.value.verified) return
        val start = window.around?.let { anchor -> window.raw.indexOfFirst { (it.number("position") ?: Long.MAX_VALUE) >= anchor } }
        val offset = if (start != null && start >= 0) (start - 40).coerceIn(0, (window.raw.size - 80).coerceAtLeast(0))
            else (window.raw.size - 80).coerceAtLeast(0)
        val cached = window.raw.drop(offset).take(80)
        snapshots.save(windowKey(window), buildJsonObject {
            put("messages", JsonArray(cached))
            put("older_cursor", if (offset > 0) cached.firstOrNull()?.optional("position") else older)
            put("newer_cursor", if (offset + cached.size < window.raw.size) cached.lastOrNull()?.optional("position") else newer)
            put("has_older", hasOlder || offset > 0)
            put("has_newer", hasNewer || offset + cached.size < window.raw.size)
        })
    }

    override suspend fun downloadFile(file: LarkPart.File): String = download(requireTransport(), file)
    override suspend fun askPersona(itemId: String, action: InboxAiAction) = askPersona(InboxPersonaRequest(itemId, action))

    override suspend fun askPersona(request: InboxPersonaRequest) = personaLock.withLock {
        scope.coroutineContext.ensureActive()
        require(request.itemId.isNotBlank() && request.operationId.isNotBlank())
        val sender = checkNotNull(mainConversationSender) { "主对话发送服务尚未接入" }
        val text = sender.outbox.value.firstOrNull { it.clientMessageId == request.operationId }?.text
            ?: personaRequests.getOrPut(request.operationId) {
                val record = current.value.records.firstOrNull { it.item.id == request.itemId }
                require(record != null || !request.context.isNullOrBlank()) { "事项尚未加载，请先刷新" }
                val messages = synchronized(windowsLock) { record?.feedId?.let { windows[it]?.state?.value?.messages } }.orEmpty()
                val loaded = messages.joinToString("\n") { message ->
                    val body = message.parts.joinToString("\n") { part -> when (part) {
                        is LarkPart.Text -> part.text
                        is LarkPart.Card -> part.content.summary
                        is LarkPart.File -> "附件：${part.name}（${part.mimeType}）"
                        is LarkPart.Image -> part.description
                        is LarkPart.External -> "${part.title}\n${part.larkUrl}"
                        is LarkPart.Code -> "代码：\n${part.code}"
                        is LarkPart.Audio -> "语音（${part.durationMs / 1000} 秒）"
                        is LarkPart.Video -> "视频（${part.durationMs / 1000} 秒）"
                        is LarkPart.Sticker -> part.description
                        is LarkPart.MergeForward -> "聊天记录：${part.title}（${part.messages.size} 条）"
                    } }
                    "${message.senderName} ${message.time}\n$body"
                }
                inboxPersonaText(request, record, loaded.ifBlank { record?.item?.preview.orEmpty() })
            }
        sender.sendToMain(text, request.operationId)
    }
    private fun requireTransport() = requireNotNull(connection.current.value) { "请先配对 hub" }
    private fun owns(transport: InboxAssistantTransport) = connection.current.value === transport
    private fun replaceRecord(record: InboxRecord) {
        current.update { it.copy(records = it.records.map { old -> if (old.item.id == record.item.id) record else old }) }
    }
    private fun publishLocal(saved: InboxReplyDocument) {
        outbox.value = saved.outbox
        current.update { it.copy(records = it.records.map { record -> withLocalStatus(record, saved) }) }
    }
    private fun withLocalStatus(record: InboxRecord, saved: InboxReplyDocument): InboxRecord {
        val target = ReplyTarget.Inbox(record.item.id)
        val pending = saved.outbox.lastOrNull {
            (it.target == target || record.feedId != null && it.feedId == record.feedId) && it.state !in setOf("sent", "target_changed")
        }
        val draft = saved.drafts[target.storageKey()]?.takeIf { it.text.isNotBlank() }
        val status = when {
            pending?.state == "sending" -> "发送中"
            pending?.outboxState == OutboxState.WaitingForNetwork -> "连上后自动发送"
            pending?.state == "unknown" -> "结果待确认"
            pending?.state == "verifying" -> "正在核对"
            pending?.state == "refused" -> "没有发出"
            pending != null -> "发送失败"
            // An AI draft shows as its card; only the user's own words read 「草稿 · …」.
            draft != null && !draft.ai -> "草稿 · ${draft.text}"
            else -> null
        }
        return record.copy(localStatus = status, localDelivery = pending?.outboxState?.delivery, aiDraft = draft?.ai == true)
    }
    private fun record(data: JsonObject, transport: InboxAssistantTransport): InboxRecord {
        val lark = data.obj("lark")
        val agent = data.obj("agent")
        val mail = data.obj("mail")
        val moment = data.obj("moment")
        return InboxRecord(
            InboxItem(data.string("id"), data.string("account"), data.string("kind"), data.string("title"),
                data.optional("sender"), data.string("preview"), data.optional("summary"), data.optional("tier"),
                data.optional("updated_at"), data.optional("done_at"), data.optional("version"),
                notice = moment?.optional("notice")),
            sourceLabel = data.optional("account_name") ?: if (data.string("source") == "lark") "飞书" else "邮件",
            feedId = lark?.optional("feed_id"), startPosition = lark?.number("start_position"), count = data.number("count")?.toInt() ?: 0,
            agent = agent?.let { InboxAgent(it.string("state"), it.string("draft"), it.optional("note") ?: it.string("reason"),
                it.string("task"), it.optional("machine_id"), it.optional("session_id")) },
            source = data.string("source"),
            atAll = lark?.flag("at_all") == true,
            read = data.flag("read"),
            mail = mail?.let {
                InboxMailRef(it.string("account_id"), it.optional("mailbox") ?: "INBOX",
                    it.array("uids").mapNotNull { uid -> (uid as? JsonPrimitive)?.longOrNull },
                    // The hub omits direct when false: a mail the user was only copied on (or got through a list).
                    direct = it.flag("direct"))
            },
            // Only a Feishu chat has a picture on the hub; a mail row keeps its initial, as on iOS.
            avatar = if (data.string("source") == "lark") avatar(data.optional("avatar"), transport) else null,
            moment = moment?.let {
                InboxMomentRef(
                    postId = it.string("post_id"),
                    notice = it.optional("notice"),
                    replyTo = it.optional("reply_to"),
                    myPost = it.flag("my_post"),
                    postText = it.optional("post_text"),
                )
            },
        )
    }

    override suspend fun momentsPost(postId: String): persona.core.contracts.LarkMomentsPost {
        val transport = requireTransport()
        val json = transport.momentsPost(postId)
        return persona.core.contracts.JsonCodec.decodeFromJsonElement(persona.core.contracts.LarkMomentsPost.serializer(), json)
    }

    override suspend fun momentsComments(postId: String): List<persona.core.contracts.LarkMomentsComment> {
        val transport = requireTransport()
        val json = transport.momentsComments(postId)
        return persona.core.contracts.JsonCodec.decodeFromJsonElement(persona.core.contracts.LarkMomentsCommentsResponse.serializer(), json).comments
    }

    override suspend fun urlPreview(messageId: String, url: String): persona.core.contracts.LarkUrlPreview? {
        val transport = requireTransport()
        return runCatching {
            val json = transport.urlPreview(messageId, url)
            persona.core.contracts.JsonCodec.decodeFromJsonElement(persona.core.contracts.LarkUrlPreview.serializer(), json)
        }.getOrNull()
    }
    private fun avatar(ref: String?, transport: InboxAssistantTransport) =
        larkAvatar(ref, transport.scope) { sized -> images.get("${transport.scope}:$sized") { transport.media(sized) } }
    private fun message(data: JsonObject, window: Window): LarkMessage {
        val source = LarkContentSource(window.transport)
        val id = data.string("id")
        val external = "https://applink.feishu.cn/client/chat/open?chatId=${java.net.URLEncoder.encode(window.feedId, "UTF-8")}&messageId=${java.net.URLEncoder.encode(id, "UTF-8")}"
        val parts = if (data.flag("recalled")) listOf(LarkPart.Text("消息已撤回")) else data.array("parts").map { element ->
            val part = element.jsonObject
            val metadata = part.obj("metadata") ?: JsonObject(emptyMap())
            val text = part.optional("text") ?: data.string("text")
            when (part.string("kind")) {
                "image", "emotion" -> {
                    val ref = part.string("url")
                    LarkPart.Image(AuthenticatedLarkImage("${window.transport.scope}:$ref") { images.get("${window.transport.scope}:$ref") { window.transport.media(ref) } },
                        width = metadata.number("width")?.toInt()?.coerceAtLeast(1) ?: 1,
                        height = metadata.number("height")?.toInt()?.coerceAtLeast(1) ?: 1)
                }
                "file" -> LarkPart.File(part.string("url"), metadata.optional("name") ?: text.ifBlank { "文件" },
                    metadata.optional("mimeType") ?: "application/octet-stream")
                "text", "at", "code" -> LarkPart.Text(text, source)
                "card", "interactive" -> larkInteractivePart(metadata, text, window.transport)
                "link" -> LarkPart.Text(listOf(text, part.string("url")).filter { it.isNotBlank() }.distinct().joinToString("\n"), source)
                else -> LarkPart.External(text, external, if (part.string("kind") == "media") "video" else "card",
                    metadata.string("duration"))
            }
        }.ifEmpty { listOf(LarkPart.Text(data.string("text"), source)) }
        return LarkMessage(id, data.string("sender_id"), data.string("sender_name"), data.number("position"), data.string("created_at"),
            data.flag("is_mine"), parts, data.optional("client_id") ?: data.optional("clientId"),
            delivery = when (data.string("local_state")) {
                "failed" -> LarkDelivery.Failed
                "sending", "pending" -> LarkDelivery.Sending
                else -> LarkDelivery.Sent
            }, avatar = avatar(data.optional("sender_avatar"), window.transport),
            replyCount = maxOf(0L, data.number("reply_count") ?: 0L, data.number("thread_reply_count") ?: 0L),
            thread = larkThreadSource(data, window.transport, window.feedId))
    }
}

private fun JsonObject.strings(key: String): List<String> = array(key).mapNotNull { (it as? JsonPrimitive)?.contentOrNull }
