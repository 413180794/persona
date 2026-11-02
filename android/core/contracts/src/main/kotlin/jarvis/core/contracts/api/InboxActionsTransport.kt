package jarvis.core.contracts.api

/** Additional inbox reads and explicit collection, bound to one pairing. */
interface InboxActionsTransport {
    suspend fun collectNow()
    suspend fun context(itemId: String): String
}
