package jarvis.core.contracts.api

import jarvis.core.contracts.Interaction
import jarvis.core.contracts.Resolution
import kotlinx.coroutines.flow.StateFlow

interface InteractionCenter {
    val pending: StateFlow<List<Interaction>>
    suspend fun refresh()
    suspend fun resolve(id: String, resolution: Resolution)
}
