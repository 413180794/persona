package persona.core.contracts.api

import kotlinx.coroutines.flow.StateFlow

data class InboxEntry(val id: String, val title: String, val preview: String, val source: String)

interface InboxRepository {
    val entries: StateFlow<List<InboxEntry>>
    suspend fun refresh()
}
