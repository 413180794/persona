package jarvis.core.contracts.api

import jarvis.core.contracts.Session
import jarvis.core.contracts.SessionKey
import kotlinx.coroutines.flow.StateFlow

data class AssistantSnapshot(val mainSession: SessionKey? = null, val conversations: List<Session> = emptyList())

interface AssistantRepository {
    val assistant: StateFlow<AssistantSnapshot>
    suspend fun refresh()
}
