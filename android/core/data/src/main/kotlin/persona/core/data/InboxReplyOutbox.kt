package persona.core.data

import persona.feature.inbox.ReplyRequest
import persona.feature.inbox.ReplyTarget
import kotlinx.serialization.Serializable

/** AI-authored drafts require confirmation; the flag is omitted on disk when false. */
@Serializable
internal data class InboxStoredDraft(val text: String, val clientId: String, val version: String? = null, val ai: Boolean = false)

@Serializable
internal data class InboxPendingReply(
    val target: ReplyTarget,
    val request: ReplyRequest,
    val feedId: String? = null,
    val state: String = "sending",
    val serverId: String? = null,
    val error: String? = null,
    /** The server's copy has been seen in a chat window: the entry stays only as the receipt for dedupe (#4687). */
    val seen: Boolean = false,
) {
    // A mail target change is a definite refusal: nothing was sent and it needs a new identifier.
    val outboxState: OutboxState get() = if (state == "waiting") OutboxState.WaitingForNetwork
        else if (state == "target_changed") OutboxState.Refused
        else OutboxState.entries.firstOrNull { it.wire == state } ?: OutboxState.Unknown
}

/** Additional durable queues join the public outbox's catch-up boundary. */
fun interface OutboxReconciler { suspend fun onReady() }

@Serializable
internal data class InboxReplyDocument(
    val drafts: Map<String, InboxStoredDraft> = emptyMap(),
    val outbox: List<InboxPendingReply> = emptyList(),
) {
    /** Both reply entry points share one durable, ordered queue and immutable request identity. */
    fun enqueue(reply: InboxPendingReply): InboxReplyDocument {
        val previous = outbox.firstOrNull { it.request.clientId == reply.request.clientId }
        require(previous == null || previous.request == reply.request && previous.target == reply.target) {
            "Reply identifier is already in use"
        }
        if (previous?.state == "sent") return this
        return copy(outbox = if (previous == null) outbox + reply else outbox.map {
            if (it.request.clientId == reply.request.clientId) reply else it
        })
    }

    fun transition(clientId: String, state: String, error: String? = null): InboxReplyDocument = copy(outbox = outbox.map {
        if (it.request.clientId == clientId && it.state != "sent") it.copy(state = state, error = error) else it
    })

    fun confirm(clientId: String, serverId: String? = null, seen: Boolean = false): InboxReplyDocument = copy(
        drafts = drafts.filterValues { it.clientId != clientId },
        outbox = outbox.map { reply -> if (reply.request.clientId == clientId)
            reply.copy(state = "sent", serverId = serverId ?: reply.serverId, seen = seen || reply.seen) else reply },
    )
}
