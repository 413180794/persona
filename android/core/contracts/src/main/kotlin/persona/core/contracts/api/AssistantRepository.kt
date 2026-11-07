package persona.core.contracts.api

import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import kotlinx.coroutines.flow.StateFlow

data class AssistantSnapshot(
    val mainSession: SessionKey? = null, val conversations: List<Session> = emptyList(),
    /** Whose conversations these are (docs/managers.md step 4): the manager the page showed when read; null: the default. */
    val manager: String? = null,
)

interface AssistantRepository {
    val assistant: StateFlow<AssistantSnapshot>
    suspend fun refresh()
}
