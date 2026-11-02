package jarvis.core.contracts.api

import jarvis.core.contracts.SessionKey
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.json.JsonObject

/** One immutable pairing owns a transport and all its requests, including media. */
interface InboxAssistantTransport {
    val scope: String
    suspend fun inbox(): JsonObject
    suspend fun larkConversations(cursor: String? = null): JsonObject = throw UnsupportedOperationException("Conversation metadata unavailable")
    suspend fun setDone(itemId: String, done: Boolean): JsonObject
    suspend fun reply(itemId: String?, feedId: String?, text: String, clientId: String): JsonObject
    suspend fun replyMail(itemId: String, text: String, clientId: String, target: MailReplyTarget): JsonObject =
        throw UnsupportedOperationException("Checked mail replies unavailable")
    suspend fun messages(feedId: String, around: Long? = null, before: String? = null, after: String? = null): JsonObject
    suspend fun assistant(): JsonObject
    suspend fun createConversation(main: Boolean): JsonObject
    suspend fun pins(): JsonObject
    suspend fun mutes(): JsonObject
    suspend fun media(ref: String): ByteArray
    suspend fun readDocument(url: String): JsonObject = throw UnsupportedOperationException("Document reader unavailable")
    suspend fun documentMedia(ref: String): ByteArray = media(ref)
}

interface InboxAssistantConnection {
    val current: StateFlow<InboxAssistantTransport?>
}

interface AssistantConversations : AssistantRepository {
    val pinned: StateFlow<Set<SessionKey>>
    suspend fun startConversation(): SessionKey
}
