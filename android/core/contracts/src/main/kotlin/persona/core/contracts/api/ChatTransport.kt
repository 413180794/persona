package persona.core.contracts.api

import persona.core.contracts.CoreTextException
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.*

/** HTTP operations for one paired hub; WebSocket ownership stays with HubConnection. */
interface ChatTransport {
    suspend fun machines(): List<Machine>
    suspend fun sessions(): List<Session>
    suspend fun items(session: SessionKey, query: ItemsQuery): ItemsResponse
    suspend fun interactions(): List<Interaction>
    suspend fun resolve(id: String, resolution: Resolution)
    /** [resolve], with what the hub said about it (a card answered already carries the answer that took effect). */
    suspend fun resolveOutcome(id: String, resolution: Resolution): ResolveOutcome {
        resolve(id, resolution)
        return ResolveOutcome()
    }
    suspend fun image(session: SessionKey, itemId: String): ByteArray
    suspend fun fullItem(session: SessionKey, itemId: String): Item
}

data class ItemsQuery(
    val beforeSeq: Int? = null,
    val afterSeq: Int? = null,
    val fromSeq: Int? = null,
    val throughSeq: Int? = null,
) {
    init {
        require(listOfNotNull(beforeSeq, afterSeq, fromSeq, throughSeq).all { it >= 0 })
        require((fromSeq == null) == (throughSeq == null))
        require(fromSeq == null || throughSeq!! >= fromSeq)
        require(listOf(beforeSeq != null, afterSeq != null, fromSeq != null).count { it } <= 1)
    }
}

/**
 * The hub's 200 to an answer (docs/protocol.md, #1673): sent now, or [already] answered, [resolution] being the answer
 * that took effect then (redacted as the history keeps it; it can differ from the one just sent).
 */
data class ResolveOutcome(val already: Boolean = false, val status: String? = null, val resolution: Resolution? = null)

/**
 * A request that is over (410) must disappear from the pending list. [status] is how it ended — `expired`,
 * `cancelled` or `dismissed` — or null when the hub could not tell (or is too old to say).
 */
class InteractionGoneException(val status: String? = null) : CoreTextException(CoreText.of(when (status) {
    "expired" -> Key.InteractionExpired
    "cancelled" -> Key.InteractionCancelled
    "dismissed" -> Key.InteractionDismissed
    else -> Key.InteractionGone
}))
