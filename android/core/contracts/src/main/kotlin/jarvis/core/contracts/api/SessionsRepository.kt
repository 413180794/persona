package jarvis.core.contracts.api

import jarvis.core.contracts.Machine
import jarvis.core.contracts.Session
import kotlinx.coroutines.flow.StateFlow

interface SessionsRepository {
    val machines: StateFlow<List<Machine>>
    val sessions: StateFlow<List<Session>>
    suspend fun refresh()
}
