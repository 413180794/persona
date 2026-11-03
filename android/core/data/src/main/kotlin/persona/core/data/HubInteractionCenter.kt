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
    scope: CoroutineScope,
    private val snapshots: ListSnapshotStore? = null,
) : InteractionCenter {
    private val mutablePending = MutableStateFlow<List<Interaction>>(emptyList())
    override val pending = mutablePending.asStateFlow()
    private val lock = Mutex()
    private val refreshing = Mutex()
    private var revision = 0L
    private val changes = mutableMapOf<String, Pair<Long, Interaction?>>()

    private val restored = scope.async {
        lock.withLock { snapshots.restore<List<Interaction>>("interactions")?.let { mutablePending.value = it } }
    }

    init {
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.events.collect { event -> restored.await(); lock.withLock {
                when (event) {
                    is InteractionEvent -> change(event.interaction.id, event.interaction)
                    is InteractionResolvedEvent -> change(event.id, null)
                    else -> Unit
                }
            } }
        }
        scope.launch {
            connection.state.collect { if (it == ConnectionState.Online) quietly { refresh() } }
        }
    }

    override suspend fun refresh() = refreshing.withLock {
        restored.await()
        val before = lock.withLock { revision }
        val fetched = transport.interactions()
        lock.withLock {
            val merged = fetched.associateBy { it.id }.toMutableMap()
            changes.filterValues { it.first > before }.forEach { (id, change) ->
                if (change.second == null) merged.remove(id) else merged[id] = change.second!!
            }
            mutablePending.value = merged.values.toList()
            snapshots.save("interactions", mutablePending.value)
        }
    }

    override suspend fun resolve(id: String, resolution: Resolution) {
        restored.await()
        try { transport.resolve(id, resolution) }
        catch (gone: InteractionGoneException) {
            lock.withLock { change(id, null) }
            throw gone
        }
        lock.withLock { change(id, null) }
    }

    private suspend fun change(id: String, interaction: Interaction?) {
        changes[id] = ++revision to interaction
        mutablePending.update { list -> list.filterNot { it.id == id } + listOfNotNull(interaction) }
        snapshots.save("interactions", mutablePending.value)
    }
}
