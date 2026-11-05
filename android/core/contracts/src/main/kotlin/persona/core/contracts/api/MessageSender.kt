package persona.core.contracts.api

import persona.core.contracts.SessionKey
import kotlinx.coroutines.flow.StateFlow

enum class DeliveryState {
    Sending, WaitingForNetwork, Sent, Rejected, NeedsConfirmation,
    Queued, Unknown, Refused, Verifying,
}

data class OutboxMessage(
    val clientMessageId: String,
    val session: SessionKey,
    val text: String,
    val state: DeliveryState,
    val error: String? = null,
    val mainTarget: Boolean = false,
)

interface MessageSender {
    val outbox: StateFlow<List<OutboxMessage>>
    suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String? = null)
    suspend fun cancel(clientMessageId: String): Boolean = false
    suspend fun retry(clientMessageId: String): Boolean = false
    /** Drops a message whose delivery is unknown: off the list and the queue, never retried or reconciled. */
    suspend fun discard(clientMessageId: String): Boolean = false
    suspend fun confirm(clientMessageId: String, delivered: Boolean): Boolean = false
}
