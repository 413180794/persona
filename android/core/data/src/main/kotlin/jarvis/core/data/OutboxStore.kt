package jarvis.core.data

import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.DeliveryState
import jarvis.core.contracts.api.DraftSnapshot
import kotlinx.coroutines.flow.Flow

enum class OutboxState(val wire: String, val delivery: DeliveryState) {
    Queued("queued", DeliveryState.Queued),
    Sending("sending", DeliveryState.Sending),
    WaitingForNetwork("waitingForNetwork", DeliveryState.WaitingForNetwork),
    Unknown("unknown", DeliveryState.Unknown),
    Refused("refused", DeliveryState.Refused),
    Verifying("verifying", DeliveryState.Verifying),
    Sent("sent", DeliveryState.Sent),
    NeedsConfirmation("needsReview", DeliveryState.NeedsConfirmation),
    Cancelled("cancelled", DeliveryState.Rejected),
    Shown("shown", DeliveryState.Sent),
}

data class OutboxEntry(
    val clientMessageId: String,
    val session: SessionKey,
    val text: String,
    val clearDraft: String? = null,
    val state: OutboxState = OutboxState.Queued,
    val error: String? = null,
    val createdAt: Long,
    val mainTarget: Boolean = false,
)

/** All methods operate on exactly one pairing scope. */
interface OutboxStore {
    val entries: Flow<List<OutboxEntry>>
    /** Persist without clearing an editor whose revision is unknown. */
    suspend fun enqueue(entry: OutboxEntry)
    /** Compare the draft revision, persist the message and clear that draft in one transaction. */
    suspend fun enqueueDraft(entry: OutboxEntry, draft: DraftSnapshot?) {
        check(draft == null) { "Versioned drafts are not supported by this store" }
        enqueue(entry)
    }
    suspend fun mainConversation(): SessionKey? = null
    suspend fun claimMainCreation(): Boolean = error("Main conversation creation is not supported by this store")
    suspend fun bindMainConversation(session: SessionKey): Unit = error("Main conversation binding is not supported by this store")
    suspend fun transition(id: String, from: Set<OutboxState>, to: OutboxState, error: String? = null): Boolean
    suspend fun recoverInterrupted()
}
