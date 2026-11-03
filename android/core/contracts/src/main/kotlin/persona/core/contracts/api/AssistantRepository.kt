package persona.core.contracts.api

import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import kotlinx.coroutines.flow.StateFlow

data class AssistantSnapshot(val mainSession: SessionKey? = null, val conversations: List<Session> = emptyList())

interface AssistantRepository {
    val assistant: StateFlow<AssistantSnapshot>
    suspend fun refresh()
}
