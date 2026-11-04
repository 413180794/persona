package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

internal data class VerifiedSessionLists(
    val machines: List<Machine>? = null,
    val sessions: Map<String, List<Session>> = emptyMap(),
)

/** List events are broadcast by the hub; listing never subscribes to session item streams. */
class HubSessionsRepository(
    private val transport: ChatTransport,
    private val connection: HubConnection,
    scope: CoroutineScope,
    private val snapshots: ListSnapshotStore? = null,
) : SessionsRepository {
    private val mutableMachines = MutableStateFlow<List<Machine>>(emptyList())
    override val machines = mutableMachines.asStateFlow()
    private val mutableSessions = MutableStateFlow<List<Session>>(emptyList())
    override val sessions = mutableSessions.asStateFlow()
    internal val verifiedLists = MutableStateFlow(VerifiedSessionLists())
    private val lock = Mutex()
    private val refreshing = Mutex()
    private var revision = 0L
    private var machinesChanged = 0L
    private val sessionChanges = mutableMapOf<String, Long>()

    private val restored = scope.async {
        lock.withLock {
            snapshots.restore<List<Machine>>("machines")?.let { mutableMachines.value = it }
            snapshots.restore<List<Session>>("sessions")?.let { mutableSessions.value = it }
        }
    }

    private suspend fun persist() {
        snapshots.save("machines", mutableMachines.value)
        snapshots.save("sessions", mutableSessions.value)
    }

    init {
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.events.collect { event -> restored.await(); lock.withLock {
                when (event) {
                    is MachinesEvent -> {
                        machinesChanged = ++revision
                        mutableMachines.value = event.machines
                        if (connection.state.value == ConnectionState.Online) verifiedLists.update { prior ->
                            prior.copy(machines = event.machines, sessions = prior.sessions.filterKeys { machine ->
                                event.machines.firstOrNull { it.id == machine }?.online == true &&
                                    prior.machines?.firstOrNull { it.id == machine }?.online != false
                            })
                        }
                    }
                    is SessionsEvent -> {
                        sessionChanges[event.machineId] = ++revision
                        mutableSessions.value = mutableSessions.value.filterNot { it.machineId == event.machineId } +
                            event.sessions.map { it.copy(machineId = event.machineId) }
                        if (connection.state.value == ConnectionState.Online) verifiedLists.update {
                            it.copy(sessions = if (it.machines?.firstOrNull { machine -> machine.id == event.machineId }?.online == false)
                                it.sessions - event.machineId
                            else it.sessions + (event.machineId to
                                event.sessions.map { session -> session.copy(machineId = event.machineId) }))
                        }
                    }
                    is RevsEvent -> {
                        sessionChanges[event.machineId] = ++revision
                        mutableSessions.update { list -> list.map { session ->
                            event.revs[session.sessionId]?.takeIf { session.machineId == event.machineId }
                                ?.let { session.copy(rev = it) } ?: session
                        } }
                    }
                    else -> return@withLock
                }
                persist()
            } }
        }
        scope.launch {
            connection.state.collectLatest {
                // A resumed socket already had the hub's machines and sessions snapshots and every broadcast missed
                // (#1574): what is held now is the hub's, no refetch needed.
                if (it == ConnectionState.Online) {
                    if (connection.resumed) lock.withLock { verifyHeld() } else quietly { refresh() }
                }
                else lock.withLock { verifiedLists.value = VerifiedSessionLists() }
            }
        }
    }

    override suspend fun refresh() = refreshing.withLock {
        restored.await()
        val before = lock.withLock { revision }
        val (machines, sessions) = coroutineScope {
            val machines = async { transport.machines() }
            val sessions = async { transport.sessions() }
            machines.await() to sessions.await()
        }
        lock.withLock {
            if (machinesChanged <= before) mutableMachines.value = machines
            val changed = sessionChanges.filterValues { it > before }.keys
            mutableSessions.value = sessions.filterNot { it.machineId in changed } +
                mutableSessions.value.filter { it.machineId in changed }
            verifyHeld()
            persist()
        }
    }

    /** Marks the lists held now as the hub's; called under [lock]. */
    private fun verifyHeld() {
        if (connection.state.value == ConnectionState.Online) verifiedLists.value = VerifiedSessionLists(
            mutableMachines.value,
            (mutableMachines.value.map { it.id } + mutableSessions.value.map { it.machineId })
                .distinct().associateWith { machine -> mutableSessions.value.filter { it.machineId == machine } },
        )
    }

    override suspend fun adopt(session: Session) {
        if (session.machineId.isBlank() || session.sessionId.isBlank()) return
        restored.await()
        lock.withLock {
            // A push or refresh that already has it is newer than the start's answer.
            if (mutableSessions.value.any { it.key == session.key }) return@withLock
            // Newer than a refresh already in flight, which must not drop it.
            sessionChanges[session.machineId] = ++revision
            mutableSessions.value = mutableSessions.value + session
            persist()
        }
    }

    override suspend fun applyMachine(machine: Machine) {
        if (machine.id.isBlank()) return
        restored.await()
        lock.withLock {
            machinesChanged = ++revision
            val list = mutableMachines.value
            mutableMachines.value = if (list.any { it.id == machine.id }) list.map { if (it.id == machine.id) machine else it } else list + machine
            persist()
        }
    }
}

internal suspend inline fun quietly(block: () -> Unit) {
    try { block() }
    catch (cancelled: CancellationException) { throw cancelled }
    catch (_: Exception) { /* A later refresh or reconnect reconciles state. */ }
}
