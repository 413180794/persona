package persona.core.contracts.api

import persona.core.contracts.SessionKey
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.json.JsonObject

/** An item of a bulk 已处理: its id and the `version` it was listed at (null: as it is now). */
data class InboxDoneTarget(val id: String, val version: String? = null)

/** What a bulk request does (`POST /api/v1/inbox/bulk`): 全部标为已处理, 全部忽略 (done_by=dismissed), 移回 (撤销). */
enum class InboxBulkAction(val wire: String) { Done("done"), Dismiss("dismiss"), Reopen("reopen") }

/** One immutable pairing owns a transport and all its requests, including media. */
interface InboxAssistantTransport {
    val scope: String
    suspend fun inbox(): JsonObject
    suspend fun larkConversations(cursor: String? = null): JsonObject = throw UnsupportedOperationException("Conversation metadata unavailable")
    suspend fun setDone(itemId: String, done: Boolean): JsonObject
    /**
     * Many items done (or back) in one request (`POST /api/v1/inbox/done`): each with the version it was listed at,
     * so one that took new messages since stays open. Answers `{changed: [items], unchanged: [ids], skipped: [{id, reason}]}`.
     */
    suspend fun setDone(items: List<InboxDoneTarget>, done: Boolean): JsonObject =
        throw UnsupportedOperationException("Bulk done unavailable")
    /**
     * [action] on many items in one request (`POST /api/v1/inbox/bulk`), each with the version it was listed at.
     * Answers what [setDone] does, plus `ids` (the changed ones) and `undo`. Without it, 已处理 / 移回 go the old way.
     */
    suspend fun bulk(items: List<InboxDoneTarget>, action: InboxBulkAction): JsonObject =
        if (action == InboxBulkAction.Dismiss) throw UnsupportedOperationException("Bulk dismiss unavailable")
        else setDone(items, action == InboxBulkAction.Done)
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
    suspend fun momentsPost(postId: String): JsonObject = throw UnsupportedOperationException("Moments post unavailable")
    suspend fun momentsComments(postId: String): JsonObject = throw UnsupportedOperationException("Moments comments unavailable")
    suspend fun urlPreview(messageId: String, url: String): JsonObject = throw UnsupportedOperationException("URL preview unavailable")
}

interface InboxAssistantConnection {
    val current: StateFlow<InboxAssistantTransport?>
}

interface AssistantConversations : AssistantRepository {
    val pinned: StateFlow<Set<SessionKey>>
    suspend fun startConversation(): SessionKey
}
