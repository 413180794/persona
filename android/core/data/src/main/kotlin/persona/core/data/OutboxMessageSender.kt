package persona.core.data

import persona.core.contracts.HubScope
import persona.core.contracts.Item
import persona.core.contracts.MessageEcho
import persona.core.contracts.SessionKey
import persona.core.contracts.api.MessageEvidence
import persona.core.contracts.api.MessageRequest
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.MessageTransport
import persona.core.contracts.api.MessageTransportResult
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.DraftMessageSender
import persona.core.contracts.api.DraftSnapshot
import persona.core.contracts.api.MainConversationResolver
import persona.core.contracts.api.MainConversationSender
import persona.core.contracts.api.PendingMainConversation
import persona.core.contracts.api.ChatComposerService
import persona.core.contracts.api.ComposerDelivery
import persona.core.contracts.api.DeliveryState
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull

class OutboxMessageSender(
    private val hubScope: HubScope,
    private val store: OutboxStore,
    private var transport: MessageTransport,
    parentScope: CoroutineScope,
    private val reviewTimeoutMillis: Long = 30_000,
    private val reconciliationTimeoutMillis: Long = 3_000,
    /** How long a delivered message waits for its copy in the transcript before its bubble goes anyway (iOS, web). */
    private val sentGraceMillis: Long = 60_000,
    // Last: callers pass the clock as a trailing lambda.
    private val now: () -> Long = System::currentTimeMillis,
) : MessageSender, DraftMessageSender, MainConversationSender {
    private val job = SupervisorJob(parentScope.coroutineContext[Job])
    private val scope = CoroutineScope(parentScope.coroutineContext + job)
    private val wake = Channel<Unit>(Channel.CONFLATED)
    private val lifecycle = Mutex()
    private val processing = Mutex()
    private val initialized = CompletableDeferred<Unit>()
    private val reconcilers = mutableListOf<OutboxReconciler>()
    @Volatile private var mainResolver: MainConversationResolver? = null
    /** Transcript items that already showed a message (observeTranscript): the same item never shows a second. */
    private val claimed = mutableSetOf<Pair<SessionKey, String>>()
    /** Whether a session is at work (connectActivity): its turn may write a delivered message only when it ends. */
    @Volatile private var busy: (SessionKey) -> Boolean = { false }

    override val outbox = store.entries.map { entries ->
        entries.filter { it.state !in hidden }.map {
            OutboxMessage(it.clientMessageId, it.session, it.text, it.state.delivery, it.error, it.mainTarget)
        }
    }.stateIn(scope, SharingStarted.Eagerly, emptyList())

    init {
        require(reviewTimeoutMillis > 0 && reconciliationTimeoutMillis > 0)
        scope.launch {
            store.recoverInterrupted()
            initialized.complete(Unit)
            wake.trySend(Unit)
            for (ignored in wake) processing.withLock { drain() }
        }
        scope.launch {
            transport.readyToSend.collect { if (it) wake.trySend(Unit) }
        }
        scope.launch {
            val deadlines = mutableMapOf<String, Job>()
            store.entries.collect { entries ->
                val unresolved = entries.filter { it.state in reviewable }.map { it.clientMessageId }.toSet()
                deadlines.keys.toList().filter { it !in unresolved }.forEach { deadlines.remove(it)?.cancel() }
                for (id in unresolved) if (id !in deadlines) {
                    deadlines[id] = scope.launch {
                        delay(reviewTimeoutMillis)
                        store.transition(id, reviewable, OutboxState.NeedsConfirmation)
                        wake.trySend(Unit)
                    }
                }
            }
        }
        scope.launch {
            // A delivered message whose copy never shows (a /compact becomes a notice, Codex writes its queue when the
            // turn ends, other words) would wait for ever: after the grace, and once its session is not at work (for
            // up to 15 more minutes), its bubble goes, as on iOS (AppModel.settleSend) and the web.
            val waits = mutableMapOf<String, Job>()
            store.entries.collect { entries ->
                val sent = entries.filter { it.state == OutboxState.Sent }.associateBy { it.clientMessageId }
                waits.keys.toList().filter { it !in sent }.forEach { waits.remove(it)?.cancel() }
                for ((id, entry) in sent) if (id !in waits) {
                    waits[id] = scope.launch {
                        delay(sentGraceMillis)
                        var checks = 0
                        while (checks++ < 90 && busy(entry.session)) delay(10_000)
                        store.transition(id, setOf(OutboxState.Sent), OutboxState.Shown)
                    }
                }
            }
        }
    }

    /** Tells the outbox which sessions are at work (the pairing's session list). */
    fun connectActivity(busy: (SessionKey) -> Boolean) { this.busy = busy }

    override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = lifecycle.withLock {
        check(job.isActive) { "Pairing scope is closed" }
        require(text.isNotBlank() && clientMessageId.isNotBlank())
        require(session.machineId.isNotBlank() && session.sessionId.isNotBlank())
        store.enqueue(OutboxEntry(clientMessageId, session, text, clearDraft, createdAt = now()))
        wake.trySend(Unit)
        Unit
    }

    /** Not sent yet, or refused (by the computer, or confirmed as never delivered): nothing reached the agent. */
    override suspend fun cancel(clientMessageId: String): Boolean =
        change(clientMessageId, dispatchable + OutboxState.Refused, OutboxState.Cancelled)

    override suspend fun retry(clientMessageId: String): Boolean = change(
        clientMessageId, setOf(OutboxState.Refused, OutboxState.NeedsConfirmation), OutboxState.Queued)

    override suspend fun confirm(clientMessageId: String, delivered: Boolean): Boolean = change(
        clientMessageId, reviewable + OutboxState.NeedsConfirmation,
        if (delivered) OutboxState.Sent else OutboxState.Refused)

    private suspend fun change(id: String, from: Set<OutboxState>, to: OutboxState): Boolean = lifecycle.withLock {
        check(job.isActive) { "Pairing scope is closed" }
        store.transition(id, from, to).also { if (it) wake.trySend(Unit) }
    }

    /** Looks at the queue again: messages were written into the store directly (GroupMerge.kt). */
    internal fun nudge() { wake.trySend(Unit) }

    /** Called before the transport releases sending after connection catch-up. */
    suspend fun onReady() {
        initialized.await()
        processing.withLock {
            reconcilers.forEach { it.onReady() }
            reconcile()
        }
    }

    /** Register a scope-owned queue without changing public message dispatch. */
    suspend fun connectReconciler(reconciler: OutboxReconciler) = processing.withLock {
        check(job.isActive) { "Pairing scope is closed" }
        reconcilers += reconciler
    }

    /** Install the resolver owned by this pairing before exposing the main sender to features. */
    suspend fun connectMainConversation(resolver: MainConversationResolver) = lifecycle.withLock {
        check(job.isActive) { "Pairing scope is closed" }
        check(mainResolver == null || mainResolver === resolver) { "Main resolver already connected" }
        mainResolver = resolver
        wake.trySend(Unit)
        Unit
    }

    /** Install one composer decorator without replacing the original transport's readiness gate. */
    suspend fun connectComposer(factory: (MessageTransport) -> ChatComposerService): ChatComposerService = processing.withLock {
        check(job.isActive) { "Pairing scope is closed" }
        val composer = factory(transport)
        require(composer is MessageTransport)
        transport = composer
        composer
    }

    /** Manual refusal retries must update the durable outbox as well as the composer overlay. */
    suspend fun recordComposerDelivery(id: String, delivery: ComposerDelivery) = lifecycle.withLock {
        check(job.isActive) { "Pairing scope is closed" }
        val state = OutboxState.entries.first { it.delivery == delivery.state }
        val from = if (delivery.state == DeliveryState.Sending) setOf(OutboxState.Refused) else setOf(OutboxState.Sending)
        check(store.transition(id, from, state, delivery.refusal?.code)) { "Message state changed" }
    }

    /** Attachments need the same durably bound target before uploading machine-local files. */
    suspend fun prepareMainConversation(): SessionKey? = processing.withLock {
        check(job.isActive) { "Pairing scope is closed" }
        if (transport.readyToSend.value) resolveMain() else null
    }

    override suspend fun sendDraft(session: SessionKey, draft: DraftSnapshot, clientMessageId: String) = lifecycle.withLock {
        check(job.isActive) { "Pairing scope is closed" }
        require(draft.scope == hubScope && draft.text.isNotBlank() && clientMessageId.isNotBlank())
        require(session.machineId.isNotBlank() && session.sessionId.isNotBlank())
        store.enqueueDraft(OutboxEntry(clientMessageId, session, draft.text, createdAt = now()), draft)
        wake.trySend(Unit)
        Unit
    }

    override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) = lifecycle.withLock {
        check(job.isActive) { "Pairing scope is closed" }
        require(text.isNotBlank() && clientMessageId.isNotBlank())
        require(draft == null || draft.scope == hubScope)
        store.enqueueDraft(OutboxEntry(clientMessageId, PendingMainConversation, text,
            createdAt = now(), mainTarget = true), draft)
        wake.trySend(Unit)
        Unit
    }

    /**
     * The session's transcript holds [items]: authoritative (from the hub), never an optimistic local echo. A message
     * sent from here that is among them is shown, and its bubble goes. Claude Code keeps its own id for the message,
     * so it is found by its words ([MessageEcho]) unless the runtime kept the client message id. One transcript item
     * shows one message (the oldest it can be), so a second identical message stays until its own copy arrives.
     */
    suspend fun observeTranscript(session: SessionKey, items: List<Item>) {
        // Not the `outbox` flow: it lags the store, and an echo that beats it would be passed over.
        if (items.none { it.role == "user" && it.kind == "text" }) return
        lifecycle.withLock {
            check(job.isActive) { "Pairing scope is closed" }
            val pending = store.entries.first().filter { it.session == session && it.state in reconcilable }
                .sortedBy { it.createdAt }.toMutableList()
            for (item in items) {
                if (pending.isEmpty()) break
                if (item.role != "user" || item.kind != "text" || (session to item.id) in claimed) continue
                val entry = pending.firstOrNull { it.clientMessageId == item.id }
                    ?: pending.firstOrNull { MessageEcho.arrived(it.text, it.createdAt, item) } ?: continue
                pending.remove(entry)
                if (store.transition(entry.clientMessageId, reconcilable, OutboxState.Shown)) {
                    claimed += session to item.id
                    wake.trySend(Unit)
                }
            }
        }
    }

    suspend fun close() = lifecycle.withLock { job.cancelAndJoin() }

    private suspend fun reconcile() {
        // Bound the whole catch-up so unresolved entries cannot hold the sending gate forever.
        withTimeoutOrNull(reconciliationTimeoutMillis) {
            val entries = store.entries.first()
            for (entry in entries.filter { it.state in reconcilable }) {
                if (entry.state in uncertain) store.transition(entry.clientMessageId, uncertain, OutboxState.Verifying)
                val evidence = try {
                    transport.reconcile(request(entry), entry.createdAt,
                        entries.count { it.session == entry.session && it.text == entry.text } == 1)
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (_: Exception) { null }
                when (evidence) {
                    MessageEvidence.Shown -> store.transition(entry.clientMessageId, reconcilable, OutboxState.Shown)
                    is MessageEvidence.Receipt -> {
                        val state = classify(evidence.result)
                        store.transition(entry.clientMessageId, uncertain, state,
                            (evidence.result as? MessageTransportResult.HubError)?.let { it.code ?: it.error })
                    }
                    null -> store.transition(entry.clientMessageId, setOf(OutboxState.Verifying), OutboxState.Unknown)
                }
            }
        }
        // Timeout or an interrupted lookup must remain visible and eligible for manual review.
        store.entries.first().filter { it.state == OutboxState.Verifying }.forEach {
            store.transition(it.clientMessageId, setOf(OutboxState.Verifying), OutboxState.Unknown)
        }
    }

    private suspend fun drain() {
        if (!transport.readyToSend.value) {
            store.entries.first().filter { it.state == OutboxState.Queued }.forEach {
                store.transition(it.clientMessageId, setOf(OutboxState.Queued), OutboxState.WaitingForNetwork)
            }
            return
        }
        reconcile()
        for (queued in store.entries.first()) {
            var entry = queued
            if (!transport.readyToSend.value) return
            val allowed = if (transport.supportsClientOps) dispatchable + OutboxState.Unknown else dispatchable
            if (entry.mainTarget && entry.session == PendingMainConversation) {
                if (entry.state !in dispatchable) continue
                val target = resolveMain() ?: run {
                    store.transition(entry.clientMessageId, dispatchable, OutboxState.WaitingForNetwork,
                        "主对话尚未就绪，输入已保存")
                    return
                }
                entry = entry.copy(session = target)
            }
            if (entry.state !in allowed) continue
            if (!store.transition(entry.clientMessageId, allowed, OutboxState.Sending)) continue
            var result = attempt(entry)
            // One immediate retry is safe only when the hub durably deduplicates this exact id.
            if (result == MessageTransportResult.NoReceipt && transport.supportsClientOps && transport.readyToSend.value) {
                store.transition(entry.clientMessageId, setOf(OutboxState.Sending), OutboxState.Unknown)
                if (!store.transition(entry.clientMessageId, setOf(OutboxState.Unknown), OutboxState.Sending)) continue
                result = attempt(entry)
            }
            val state = classify(result)
            val error = (result as? MessageTransportResult.HubError)?.let { it.code ?: it.error }
            store.transition(entry.clientMessageId, setOf(OutboxState.Sending), state, error)
            if (state == OutboxState.WaitingForNetwork) return
        }
    }

    private fun request(entry: OutboxEntry) = MessageRequest(hubScope, entry.session, entry.text, entry.clientMessageId, entry.clearDraft)

    private suspend fun resolveMain(): SessionKey? {
        store.mainConversation()?.let { return it }
        val resolver = mainResolver ?: return null
        return try {
            val target = resolver.findMainConversation() ?: if (store.claimMainCreation()) {
                resolver.createMainConversation()
            } else null
            if (target != null) store.bindMainConversation(target)
            target
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            // A legacy create may have succeeded remotely. Only look up the main pointer on retry.
            null
        }
    }

    private suspend fun attempt(entry: OutboxEntry): MessageTransportResult = try {
        withTimeoutOrNull(reviewTimeoutMillis) { transport.send(request(entry)) } ?: MessageTransportResult.NoReceipt
    } catch (cancelled: CancellationException) {
        withContext(NonCancellable) {
            store.transition(entry.clientMessageId, setOf(OutboxState.Sending), OutboxState.Unknown)
        }
        throw cancelled
    } catch (_: Exception) {
        // The transport must positively identify pre-send failures with NotSent.
        MessageTransportResult.NoReceipt
    }

    companion object {
        private val uncertain = setOf(OutboxState.Sending, OutboxState.Unknown, OutboxState.Verifying)
        private val reviewable = setOf(OutboxState.Unknown, OutboxState.Verifying)
        private val reconcilable = uncertain + setOf(OutboxState.Sent, OutboxState.NeedsConfirmation)
        private val dispatchable = setOf(OutboxState.Queued, OutboxState.WaitingForNetwork)
        private val hidden = setOf(OutboxState.Cancelled, OutboxState.Shown)
        private val refusals = setOf("input_draft", "dialog_open", "op_id_reused", "invalid", "stale_session")

        fun classify(result: MessageTransportResult): OutboxState = when (result) {
            MessageTransportResult.Accepted -> OutboxState.Sent
            MessageTransportResult.NotSent -> OutboxState.WaitingForNetwork
            MessageTransportResult.NoReceipt -> OutboxState.Unknown
            is MessageTransportResult.HubError -> when {
                result.delivered == "unknown" -> OutboxState.Verifying
                result.delivered == "yes" -> OutboxState.Sent
                result.code in refusals -> OutboxState.Refused
                result.delivered == "none" && result.retryable -> OutboxState.WaitingForNetwork
                result.delivered == "none" -> OutboxState.Refused
                else -> OutboxState.Unknown
            }
        }
    }
}
