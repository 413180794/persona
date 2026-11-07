package persona.core.contracts.stream

import java.util.UUID
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.*
import persona.core.contracts.JsonCodec
import persona.core.contracts.generated.*

/** Local routing identity, never JSON authority. Transport and incoming callbacks must bind this exact scope. */
data class NativeStreamIdentity(
    val owner: String, val hubGroup: String, val extension: String, val contribution: String, val navigation: String,
)

fun interface NativeStreamTransport {
    /** Completion means written, not action success. Only [NativeStreamNotSent] permits an explicit safe retry. */
    suspend fun send(request: ExtensionUIStreamRequest)
}

/** A transport may use this only when it proves no bytes of this request were delivered. */
class NativeStreamNotSent : Exception()

enum class NativeStreamPhase { Dormant, Opening, Ready, Recovering, Suspended, Disconnected, Closed }
data class NativeStreamDraft(val name: String, val type: String, val value: JsonElement)
data class NativeStreamState(
    val phase: NativeStreamPhase = NativeStreamPhase.Dormant,
    val tree: NativeStreamTree? = null,
    val drafts: Map<String, NativeStreamDraft> = emptyMap(),
    val pendingEvents: Int = 0,
    /** Capability paths only, never input values, media addresses or secret material. */
    val unsupported: List<String> = emptyList(),
)

/**
 * Server-only execution; this session only orders typed sends and commits native payloads supplied by its caller.
 * Its owning controller binds the canonical envelope and actual authenticated app connection.
 */
class NativeStreamSession(
    val identity: NativeStreamIdentity,
    private val context: ExtensionUIContext,
    private val transport: NativeStreamTransport,
    private val initialState: String = "",
    private val id: () -> String = { UUID.randomUUID().toString() },
) {
    private val mutex = Mutex()
    private val sending = Mutex()
    val components: Set<String> = context.stream?.components.orEmpty().toSet()
    private var viewProfile: ((JsonObject) -> List<String>)? = null
    private var profileGaps: List<String> = emptyList()
    private val reducer = NativeStreamReducer(components) { view ->
        profileGaps = viewProfile?.invoke(view).orEmpty()
        profileGaps.isEmpty()
    }
    /** Install a renderer's field-level profile before opening. Not a new wire capability/envelope. */
    fun rendererProfile(profile: (JsonObject) -> List<String>) {
        require(viewProfile === profile || mutable.value.phase == NativeStreamPhase.Dormant)
        viewProfile = profile
    }
    private val mutable = MutableStateFlow(NativeStreamState())
    val state = mutable.asStateFlow()
    private val events = ArrayDeque<ExtensionUIStreamEventRequest>()
    private val eventDrafts = mutableMapOf<String, Map<String, NativeStreamDraft>>()
    private var discarded: (ExtensionUIStreamEvent) -> Unit = {}
    /** Navigation owner settles callbacks that never crossed the wire; no server receipt is invented. */
    fun onDiscard(handler: (ExtensionUIStreamEvent) -> Unit) { discarded = handler }
    private val lifecycle = ArrayDeque<ExtensionUIStreamRequest>()
    private var flight: ExtensionUIStreamRequest? = null
    private var retryable = false
    private var retryRequested = false
    private var drainRequested = false
    private var expectedSnapshot: String? = null
    private var newSessionExpected = false
    private var visible = false
    private var foreground = false
    private var connected = true
    private val retiredEpochs = linkedSetOf<String>()
    private val openingRequests = linkedMapOf<String, ExtensionUIStreamOpenRequest>()

    init {
        require(listOf(identity.owner, identity.hubGroup, identity.extension, identity.contribution, identity.navigation).all { it.isNotEmpty() })
        require(context.stream?.protocol == 1)
        require(initialState.toByteArray(Charsets.UTF_8).size <= 16384)
    }

    suspend fun visibility(shown: Boolean, inForeground: Boolean) {
        mutex.withLock {
            if (mutable.value.phase == NativeStreamPhase.Closed) return
            val wasActive = visible && foreground
            visible = shown; foreground = inForeground
            val active = visible && foreground
            if (!active && (wasActive || mutable.value.phase == NativeStreamPhase.Dormant)) {
                discardEvents()
                lifecycle.clear()
                expectedSnapshot = null
                current()?.let { enqueue(ExtensionUIStreamSuspendRequest(protocol = 1, requestId = id(), session = it.session, epoch = it.epoch)) }
                phase(NativeStreamPhase.Suspended)
            } else if (active && (!wasActive || mutable.value.phase == NativeStreamPhase.Dormant)) {
                if (connected) refresh(resume = true) else phase(NativeStreamPhase.Disconnected)
            }
        }
        flush()
    }

    suspend fun connection(online: Boolean) {
        mutex.withLock {
            if (mutable.value.phase == NativeStreamPhase.Closed) { connected = online; return@withLock }
            if (connected == online) return
            connected = online
            discardEvents(); lifecycle.clear(); expectedSnapshot = null
            if (!online) phase(NativeStreamPhase.Disconnected)
            else if (visible && foreground) refresh(resume = true)
            else phase(NativeStreamPhase.Suspended)
        }
        flush()
    }

    /** Caller reports that the server disposed the generation (upgrade/revocation/runtime restart). No envelope invented here. */
    suspend fun reopen() {
        mutex.withLock {
            if (mutable.value.phase == NativeStreamPhase.Closed) return
            newSessionExpected = true
            discardEvents(); lifecycle.clear(); expectedSnapshot = null
            if (active()) refresh(resume = false)
            else phase(if (connected) NativeStreamPhase.Suspended else NativeStreamPhase.Disconnected)
        }
        flush()
    }

    /** A failed read barrier keeps allocation ownership and permits a later read-only retry. */
    suspend fun snapshotFailed(requestId: String) {
        mutex.withLock {
            if (expectedSnapshot != requestId) return
            expectedSnapshot = null
            phase(if (connected) NativeStreamPhase.Recovering else NativeStreamPhase.Disconnected)
        }
    }

    suspend fun requestSnapshot() {
        mutex.withLock { if (active() && expectedSnapshot == null) refresh(resume = false) }
        flush()
    }

    /** Correlation is supplied by the authenticated adapter, not invented as a new wire envelope field. */
    suspend fun snapshot(source: NativeStreamIdentity, requestId: String, text: String): Boolean {
        val accepted = mutex.withLock {
            if (source != identity || requestId != expectedSnapshot && requestId !in openingRequests) return@withLock false
            val parsed = runCatching { NativeStreamSchema.parse(text, "UIStreamSnapshot") }.getOrNull()
            val next = parsed?.let { runCatching { JsonCodec.decodeFromJsonElement(ExtensionUIStreamSnapshot.serializer(), it) }.getOrNull() }
            if (next == null) { if (active()) recover(); return@withLock false }
            if (requestId != expectedSnapshot || !active()) {
                if (openingRequests.remove(requestId) != null) {
                    // An open may complete after hide/remove. Release that orphan instead of accepting its tree.
                    enqueue(ExtensionUIStreamCloseRequest(protocol = 1, requestId = id(), session = next.session, epoch = next.epoch))
                }
                return@withLock false
            }
            val old = current()
            if (old != null && (if (newSessionExpected) next.session == old.session else
                (next.session != old.session || next.epoch in retiredEpochs || next.epoch == old.epoch && next.version < old.version))) return@withLock false
            val opened = openingRequests.remove(requestId) != null
            val update = reducer.snapshot(text)
            if (update !is NativeStreamUpdate.Applied) {
                if (profileGaps.isNotEmpty()) {
                    closeLocked()
                    mutable.value = mutable.value.copy(unsupported = profileGaps)
                    if (old == null || old.session != next.session || old.epoch != next.epoch)
                        enqueue(ExtensionUIStreamCloseRequest(protocol = 1, requestId = id(), session = next.session, epoch = next.epoch))
                } else if (opened) {
                    // The server allocated a session, but its tree cannot be committed. Opening
                    // again would leak this allocation and can loop on incompatible capabilities.
                    closeLocked()
                    enqueue(ExtensionUIStreamCloseRequest(protocol = 1, requestId = id(), session = next.session, epoch = next.epoch))
                } else recover()
                return@withLock false
            }
            if (newSessionExpected) retiredEpochs.clear()
            else if (old != null && old.epoch != next.epoch) {
                retiredEpochs.add(old.epoch)
                // Bound memory; requiring a new session is safer than forgetting retired generations.
                if (retiredEpochs.size > 64) { closeLocked(); return@withLock false }
            }
            expectedSnapshot = null
            newSessionExpected = false
            commit(update.tree)
            phase(NativeStreamPhase.Ready)
            true
        }
        flush()
        return accepted
    }

    /** A live authoritative replacement (including an event's snapshot result) cannot bypass resume correlation. */
    suspend fun liveSnapshot(source: NativeStreamIdentity, text: String): Boolean {
        val accepted = mutex.withLock {
            val old = current() ?: return@withLock false
            if (source != identity || !active() || mutable.value.phase != NativeStreamPhase.Ready || expectedSnapshot != null) return@withLock false
            val raw = runCatching { NativeStreamSchema.parse(text, "UIStreamSnapshot") }.getOrNull() ?: return@withLock false
            val next = runCatching { JsonCodec.decodeFromJsonElement(ExtensionUIStreamSnapshot.serializer(), raw) }.getOrNull() ?: return@withLock false
            if (next.session != old.session || next.epoch != old.epoch || next.version < old.version) return@withLock false
            when (val update = reducer.snapshot(text)) {
                is NativeStreamUpdate.Applied -> { commit(update.tree); true }
                NativeStreamUpdate.SnapshotRequired -> {
                    if (profileGaps.isNotEmpty()) {
                        closeLocked()
                        mutable.value = mutable.value.copy(unsupported = profileGaps)
                    } else recover()
                    false
                }
            }
        }
        flush()
        return accepted
    }

    suspend fun delta(source: NativeStreamIdentity, text: String): Boolean {
        val accepted = mutex.withLock {
            if (source != identity || !active() || mutable.value.phase != NativeStreamPhase.Ready) return@withLock false
            when (val update = reducer.delta(text)) {
                is NativeStreamUpdate.Applied -> { commit(update.tree); true }
                NativeStreamUpdate.SnapshotRequired -> {
                    if (profileGaps.isNotEmpty()) {
                        closeLocked()
                        mutable.value = mutable.value.copy(unsupported = profileGaps)
                    } else recover()
                    false
                }
            }
        }
        flush()
        return accepted
    }

    /** Drafts are keyed by stable node identity, never section index or a field name shared by two nodes. */
    suspend fun edit(key: String, value: JsonElement, expectedTree: NativeStreamTree? = null): Boolean = mutex.withLock {
        if (expectedTree != null && mutable.value.tree !== expectedTree) return@withLock false
        val node = mutable.value.tree?.nodes?.get(key) ?: return@withLock false
        if ((node["control"] as? JsonObject)?.get("disabled") == JsonPrimitive(true)) return@withLock false
        val descriptor = draftDescriptor(node) ?: return@withLock false
        if (mutable.value.phase == NativeStreamPhase.Closed) return@withLock false
        if (runCatching { NativeStreamSchema.validate("UIStreamValue", value) }.isFailure) return@withLock false
        mutable.value = mutable.value.copy(drafts = mutable.value.drafts + (key to NativeStreamDraft(descriptor.first, descriptor.second, value)))
        true
    }

    /** Drop only the retained debounce's exact draft, never typing that followed it. */
    suspend fun discardDraft(key: String, draft: NativeStreamDraft?) = mutex.withLock {
        if (draft != null && mutable.value.drafts[key] === draft) mutable.value = mutable.value.copy(drafts = mutable.value.drafts - key)
    }

    suspend fun event(key: String, action: String, event: String, value: JsonElement, expectedTree: NativeStreamTree? = null,
                      draftsAtPress: Map<String, NativeStreamDraft>? = null,
                      onQueued: (ExtensionUIStreamEvent) -> Unit = {}): Boolean {
        val accepted = mutex.withLock {
            val tree = mutable.value.tree ?: return@withLock false
            if (expectedTree != null && tree !== expectedTree) return@withLock false
            if (!active() || mutable.value.phase != NativeStreamPhase.Ready || !tree.callback(key, action, event)) return@withLock false
            if (events.size + (if (flight is ExtensionUIStreamEventRequest) 1 else 0) >= 64) {
                recover(); return@withLock false
            }
            val saved = tree.snapshot
            val request = ExtensionUIStreamEventRequest(protocol = 1, requestId = id(), interaction = ExtensionUIStreamEvent(
                protocol = 1, session = saved.session, epoch = saved.epoch, version = saved.version, eventId = id(),
                key = key, action = action, event = event, value = value))
            if (!validRequest(request)) return@withLock false
            val values = value as? JsonObject
            // Submit buttons carry drafts from other nodes. Preserve each exact draft instance,
            // including whitespace, so authority can settle it without erasing later/ABA typing.
            eventDrafts[request.requestId] = (draftsAtPress ?: mutable.value.drafts).filter { (draftKey, draft) ->
                draftKey == key || tree.submits(key, action) && values?.containsKey(draft.name) == true
            }
            events.addLast(request)
            onQueued(request.interaction)
            pending()
            true
        }
        flush()
        return accepted
    }

    /** Only an ordered canonical receipt can settle the original input draft. */
    suspend fun received(source: NativeStreamIdentity, requestId: String, version: Long) {
        mutex.withLock {
            val request = flight as? ExtensionUIStreamEventRequest ?: return
            val tree = mutable.value.tree ?: return
            if (source != identity || requestId != request.requestId || version != request.interaction.version + 1 ||
                tree.snapshot.session != request.interaction.session || tree.snapshot.epoch != request.interaction.epoch || tree.snapshot.version < version) return
            settleDrafts(requestId)
        }
        acknowledged(source, requestId)
    }

    /** Caller supplies a definitive server result. Unknown delivery is not an acknowledgement and is never replayed. */
    suspend fun acknowledged(source: NativeStreamIdentity, requestId: String) {
        mutex.withLock {
            if (source != identity || (flight as? ExtensionUIStreamEventRequest)?.requestId != requestId) return
            eventDrafts.remove(requestId)
            flight = null; retryable = false; pending()
        }
        flush()
    }

    /** Definitive action failure from the authenticated owner. Revert only the failed control draft, never later typing. */
    suspend fun rejected(source: NativeStreamIdentity, requestId: String): ExtensionUIStreamEvent? {
        val failed = mutex.withLock {
            val request = flight as? ExtensionUIStreamEventRequest ?: return@withLock null
            if (source != identity || request.requestId != requestId) return@withLock null
            val interaction = request.interaction
            settleDrafts(requestId)
            eventDrafts.remove(requestId)
            flight = null; retryable = false; pending()
            interaction
        }
        flush()
        return failed
    }

    /** Explicit retry only for provably unwritten requests; event/request IDs and the original payload stay identical. */
    suspend fun retryUnsent() {
        mutex.withLock {
            if (!retryable || !connected || flight is ExtensionUIStreamEventRequest && !active()) return
            retryRequested = true
        }
        flush()
    }

    suspend fun close() {
        mutex.withLock { closeLocked() }
        flush()
    }

    private fun closeLocked() {
        if (mutable.value.phase == NativeStreamPhase.Closed) return
        discardEvents(); lifecycle.clear(); expectedSnapshot = null
        current()?.let { enqueue(ExtensionUIStreamCloseRequest(protocol = 1, requestId = id(), session = it.session, epoch = it.epoch)) }
        reducer.clear()
        retiredEpochs.clear()
        mutable.value = NativeStreamState(phase = NativeStreamPhase.Closed)
        drainRequested = true // Closing must wake an in-progress sender even if its renderer is cancelled.
    }

    private fun current() = mutable.value.tree?.snapshot
    private fun active() = connected && visible && foreground && mutable.value.phase != NativeStreamPhase.Closed
    private fun phase(value: NativeStreamPhase) { mutable.value = mutable.value.copy(phase = value) }
    private fun pending() { mutable.value = mutable.value.copy(pendingEvents = events.size + if (flight is ExtensionUIStreamEventRequest) 1 else 0) }
    private fun settleDrafts(requestId: String) {
        val originals = eventDrafts[requestId].orEmpty()
        val remove = originals.filter { (key, draft) ->
            mutable.value.drafts[key] === draft && events.none { it.requestId != requestId && eventDrafts[it.requestId]?.containsKey(key) == true }
        }.keys
        mutable.value = mutable.value.copy(drafts = mutable.value.drafts - remove)
    }
    private fun discard(request: ExtensionUIStreamEventRequest) {
        settleDrafts(request.requestId)
        eventDrafts.remove(request.requestId)
        discarded(request.interaction)
    }
    private fun discardEvents() {
        val removed = events.toList() + listOfNotNull(flight as? ExtensionUIStreamEventRequest)
        events.clear(); flight = null
        removed.forEach(::discard)
        eventDrafts.clear(); retryable = false; retryRequested = false; pending()
    }
    private fun validRequest(request: ExtensionUIStreamRequest) = runCatching {
        val raw = JsonCodec.encodeToString(ExtensionUIStreamRequest.serializer(), request)
        NativeStreamSchema.parse(raw, "UIStreamRequest", NativeStreamSchema.RequestBytes)
    }.isSuccess
    private fun enqueue(request: ExtensionUIStreamRequest) {
        require(validRequest(request))
        require(lifecycle.size < 8)
        lifecycle.addLast(request)
    }
    private fun refresh(resume: Boolean) {
        discardEvents(); lifecycle.clear()
        val saved = if (newSessionExpected) null else current()
        if (saved == null && openingRequests.size >= 8) { closeLocked(); return }
        val request = when {
            saved == null && !newSessionExpected && openingRequests.isNotEmpty() -> openingRequests.values.last()
            saved == null -> ExtensionUIStreamOpenRequest(protocol = 1, requestId = id(), extension = identity.extension,
                contribution = identity.contribution, context = context, state = current()?.view?.state ?: initialState)
            resume -> ExtensionUIStreamResumeRequest(protocol = 1, requestId = id(), session = saved.session, epoch = saved.epoch, version = saved.version)
            else -> ExtensionUIStreamSnapshotRequest(protocol = 1, requestId = id(), session = saved.session, epoch = saved.epoch, version = saved.version)
        }
        expectedSnapshot = when (request) {
            is ExtensionUIStreamOpenRequest -> request.requestId
            is ExtensionUIStreamResumeRequest -> request.requestId
            is ExtensionUIStreamSnapshotRequest -> request.requestId
            else -> error("Unreachable")
        }
        if (request is ExtensionUIStreamOpenRequest) openingRequests[request.requestId] = request
        enqueue(request)
        phase(if (saved == null) NativeStreamPhase.Opening else NativeStreamPhase.Recovering)
    }
    private fun recover() { if (active()) refresh(resume = false) }
    private fun draftDescriptor(node: JsonObject): Pair<String, String>? {
        val type = node["type"]?.jsonPrimitive?.content ?: if ("control" in node) "tile" else return null
        val source = if (type in setOf("row", "tile")) node["control"] as? JsonObject ?: return null else node
        if (type !in setOf("field", "composer", "segmented", "row", "tile") || source["kind"] == JsonPrimitive("secret")) return null
        val name = source["name"]?.jsonPrimitive?.content ?: return null
        return name to "$type:${source["kind"]?.jsonPrimitive?.content.orEmpty()}"
    }

    private fun commit(tree: NativeStreamTree) {
        val invalid = events.filter { request ->
            val event = request.interaction
            event.session != tree.snapshot.session || event.epoch != tree.snapshot.epoch || event.version != tree.snapshot.version ||
                !tree.callback(event.key, event.action, event.event)
        }
        events.removeAll(invalid.toSet())
        invalid.forEach(::discard)
        val retained = events.map { it.requestId }.toSet() + listOfNotNull((flight as? ExtensionUIStreamEventRequest)?.requestId)
        eventDrafts.keys.retainAll(retained)
        val drafts = mutable.value.drafts.filter { (key, draft) ->
            tree.nodes[key]?.let(::draftDescriptor) == (draft.name to draft.type)
        }
        mutable.value = mutable.value.copy(tree = tree, drafts = drafts)
        pending()
    }

    private suspend fun flush() {
        // Sender ownership and its wake-up bit must change under the same lock. Otherwise a
        // failed tryLock can record its wake-up after the outgoing sender has already stopped.
        mutex.withLock {
            if (!sending.tryLock()) { drainRequested = true; return }
            drainRequested = false
        }
        try {
            while (true) {
                val request = mutex.withLock {
                    if (!connected) return@withLock null
                    if (flight != null) {
                        if (retryRequested && retryable) {
                            retryRequested = false; retryable = false; flight
                        } else null
                    } else {
                        val next = lifecycle.removeFirstOrNull() ?: if (active() && mutable.value.phase == NativeStreamPhase.Ready) events.removeFirstOrNull() else null
                        retryRequested = false; retryable = false
                        flight = next; pending(); next
                    }
                } ?: break
                try {
                    transport.send(request)
                    mutex.withLock {
                        if (flight === request && request !is ExtensionUIStreamEventRequest) flight = null
                    }
                } catch (cancelled: CancellationException) {
                    withContext(NonCancellable) { mutex.withLock { if (flight === request) { flight = null; if (active()) recover() } } }
                    throw cancelled
                } catch (_: NativeStreamNotSent) {
                    mutex.withLock { if (flight === request) retryable = true }
                    break
                } catch (_: Exception) {
                    mutex.withLock { if (flight === request) { flight = null; if (active()) recover() } }
                    // Stop here: the caller must retry recovery, never this possibly delivered event.
                    break
                }
            }
        } finally {
            val again = withContext(NonCancellable) { mutex.withLock {
                sending.unlock()
                drainRequested.also { drainRequested = false }
            } }
            if (again && currentCoroutineContext().isActive) flush()
            else if (again && mutable.value.phase == NativeStreamPhase.Closed) {
                // A capability refusal can remove the renderer while its open is still unwinding.
                // Only close cleanup survives cancellation; never send queued events from here.
                withContext(NonCancellable) { withTimeoutOrNull(5000) { flush() } }
            }
        }
    }
}
