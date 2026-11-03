package persona.core.contracts.api

import persona.core.contracts.Machine
import persona.core.contracts.Session
import kotlinx.coroutines.flow.StateFlow

interface SessionsRepository {
    val machines: StateFlow<List<Machine>>
    val sessions: StateFlow<List<Session>>
    suspend fun refresh()

    /**
     * A session the hub just created (an async start answers with it while the agent is still starting): listed at
     * once, so its chat has metadata before the next `sessions` push. A session already listed is left as it is.
     */
    suspend fun adopt(session: Session) {}

    /** A machine the hub just returned (PUT …/engine): shown at once, before the next `machines` push. */
    suspend fun applyMachine(machine: Machine) {}
}
