package persona.core.contracts.api

/** Additional inbox reads and explicit collection, bound to one pairing. */
interface InboxActionsTransport {
    suspend fun collectNow()
    suspend fun context(itemId: String): String

    /**
     * Asks the hub to draft a reply in the user's voice (`POST /api/v1/inbox/{id}/draft`). [instruction] may be
     * empty; the text comes back for the user to edit and is never sent by this call.
     */
    suspend fun draft(itemId: String, instruction: String): String =
        throw UnsupportedOperationException("Reply drafting unavailable")
}
