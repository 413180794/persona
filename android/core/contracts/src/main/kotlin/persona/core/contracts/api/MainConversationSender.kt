package persona.core.contracts.api

import persona.core.contracts.SessionKey
import kotlinx.coroutines.flow.StateFlow

/** Local outbox destination until a main conversation is durably bound. Never send it to the hub. */
val PendingMainConversation = SessionKey("", "main")

/** Shared entry point for sharing, asking Jarvis and handing context to Jarvis. */
interface MainConversationSender {
    val outbox: StateFlow<List<OutboxMessage>>
    /** Commits locally before returning; a failed call must leave the editor intact. */
    suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot? = null)
}

/** Bind to one immutable pairing, like MessageTransport. */
interface MainConversationResolver {
    suspend fun findMainConversation(): SessionKey?
    /** The caller journals this attempt before invoking a legacy, non-idempotent endpoint. */
    suspend fun createMainConversation(): SessionKey
}
