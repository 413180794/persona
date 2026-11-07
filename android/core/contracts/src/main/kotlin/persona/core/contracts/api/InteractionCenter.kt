package persona.core.contracts.api

import persona.core.contracts.Interaction
import persona.core.contracts.Resolution
import kotlinx.coroutines.flow.StateFlow

interface InteractionCenter {
    val pending: StateFlow<List<Interaction>>
    suspend fun refresh()
    /** [refresh], as far as request [id] is concerned: merged over hub groups, only the group it belongs to (#5697). */
    suspend fun refresh(id: String) = refresh()
    suspend fun resolve(id: String, resolution: Resolution)
    /** Hide this card on this device durably, without contacting or answering its source. */
    suspend fun discardLocal(id: String) { error("Local dismissal is unavailable") }
    /** [resolve], with what the hub said about it ([ResolveOutcome]). */
    suspend fun resolveOutcome(id: String, resolution: Resolution): ResolveOutcome {
        resolve(id, resolution)
        return ResolveOutcome()
    }
}
