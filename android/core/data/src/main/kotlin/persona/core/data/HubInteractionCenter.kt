package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

class HubInteractionCenter(
    private val transport: ChatTransport,
    connection: HubConnection,
    private val scope: CoroutineScope,
    private val snapshots: ListSnapshotStore? = null,
    /** The hub's sessions: where an agent stood when its card was answered here ([answered]). */
    private val sessions: StateFlow<List<Session>>? = null,
) : InteractionCenter {
    private val mutablePending = MutableStateFlow<List<Interaction>>(emptyList())
    override val pending = mutablePending.asStateFlow()
    private val mutableAnswered = MutableStateFlow<Map<SessionKey, AnsweredMark>>(emptyMap())
    /** The agents a card was answered for here in the last [ANSWERED_MILLIS] (#4525, [answeredSessions]). */
    val answered = mutableAnswered.asStateFlow()
    private val lock = Mutex()
    private val refreshing = Mutex()
    private var revision = 0L
    private val changes = mutableMapOf<String, Pair<Long, Interaction?>>()
    /**
     * Requests that ended here (answered, gone, resolved by an event): a snapshot that still lists one, fetched or
     * served before the hub caught up, does not bring its card back (#1688). Only a live event for the id does.
     */
    private val ended = LinkedHashSet<String>()

    private val restored = scope.async {
        lock.withLock { snapshots.restore<List<Interaction>>("interactions")?.let { mutablePending.value = it } }
    }

    init {
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.events.collect { event -> restored.await(); lock.withLock {
                when (event) {
                    is InteractionEvent -> { ended.remove(event.interaction.id); change(event.interaction.id, event.interaction) }
                    is InteractionResolvedEvent -> end(event.id)
                    else -> Unit
                }
            } }
        }
        scope.launch {
            // A resumed socket already had the pending snapshot and every answer missed (#1574).
            connection.state.collect { if (it == ConnectionState.Online && !connection.resumed) quietly { refresh() } }
        }
    }

    override suspend fun refresh() = refreshing.withLock {
        restored.await()
        val before = lock.withLock { revision }
        val fetched = transport.interactions()
        lock.withLock {
            val merged = fetched.filterNot { it.id in ended }.associateBy { it.id }.toMutableMap()
            changes.filterValues { it.first > before }.forEach { (id, change) ->
                if (change.second == null) merged.remove(id) else merged[id] = change.second!!
            }
            mutablePending.value = merged.values.toList()
            snapshots.save("interactions", mutablePending.value)
        }
    }

    override suspend fun resolve(id: String, resolution: Resolution) { resolveOutcome(id, resolution) }

    override suspend fun resolveOutcome(id: String, resolution: Resolution): ResolveOutcome {
        restored.await()
        // Whose card it is and the hub's state for them now, before an interaction_resolved can take the card away.
        val asked = pending.value.firstOrNull { it.id == id }?.sessionKey
        val since = asked?.let { key -> sessions?.value?.firstOrNull { it.key == key }?.stateSince }
        val outcome = try { transport.resolveOutcome(id, resolution) }
        catch (gone: InteractionGoneException) {
            lock.withLock { end(id) }
            throw gone
        }
        lock.withLock { end(id) }
        // Answered (now, or already): its agent goes on. An expired or withdrawn card (gone) says nothing of that, and a
        // card handed back to the computer (dismiss) leaves the agent waiting there (web Interactions.tsx does the same).
        if (resolution.dismiss != true || outcome.already) asked?.let { answered(it, AnsweredMark(since)) }
        return outcome
    }

    private fun answered(key: SessionKey, mark: AnsweredMark) {
        mutableAnswered.update { it + (key to mark) }
        scope.launch {
            delay(ANSWERED_MILLIS)
            // A later answer for the same agent keeps its own time.
            mutableAnswered.update { if (it[key] === mark) it - key else it }
        }
    }

    private suspend fun end(id: String) {
        ended.remove(id)
        ended.add(id)
        // Ids are not reused; the bound only keeps a long-running app from holding every answer it ever gave.
        while (ended.size > ENDED_LIMIT) ended.remove(ended.first())
        change(id, null)
    }

    private suspend fun change(id: String, interaction: Interaction?) {
        changes[id] = ++revision to interaction
        mutablePending.update { list -> list.filterNot { it.id == id } + listOfNotNull(interaction) }
        snapshots.save("interactions", mutablePending.value)
    }

    internal companion object {
        const val ENDED_LIMIT = 512
        /** As the web's answered cards (store.ts ANSWERED_MS): long enough for the node's next sessions push. */
        const val ANSWERED_MILLIS = 20_000L
    }
}
