package persona.core.contracts.api

import persona.core.contracts.Interaction
import persona.core.contracts.Resolution
import kotlinx.coroutines.flow.StateFlow

interface InteractionCenter {
    val pending: StateFlow<List<Interaction>>
    suspend fun refresh()
    suspend fun resolve(id: String, resolution: Resolution)
}
