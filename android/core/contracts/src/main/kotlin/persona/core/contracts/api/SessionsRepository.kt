package persona.core.contracts.api

import persona.core.contracts.Machine
import persona.core.contracts.Session
import kotlinx.coroutines.flow.StateFlow

interface SessionsRepository {
    val machines: StateFlow<List<Machine>>
    val sessions: StateFlow<List<Session>>
    suspend fun refresh()
}
