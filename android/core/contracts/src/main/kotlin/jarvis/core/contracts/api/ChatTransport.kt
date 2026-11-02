package jarvis.core.contracts.api

import jarvis.core.contracts.*

/** HTTP operations for one paired hub; WebSocket ownership stays with HubConnection. */
interface ChatTransport {
    suspend fun machines(): List<Machine>
    suspend fun sessions(): List<Session>
    suspend fun items(session: SessionKey, query: ItemsQuery): ItemsResponse
    suspend fun interactions(): List<Interaction>
    suspend fun resolve(id: String, resolution: Resolution)
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

/** A request already handled elsewhere must disappear from the pending list. */
class InteractionGoneException : Exception("请求已在别处处理")
