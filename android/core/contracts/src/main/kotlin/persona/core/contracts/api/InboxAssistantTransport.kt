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
    /** A conversation that runs on [launcher] (POST /api/v1/assistant/conversations `launcher`), not the default one. */
    suspend fun createConversation(main: Boolean, launcher: String): JsonObject =
        throw UnsupportedOperationException("Picking a launcher unavailable")
    suspend fun pins(): JsonObject
    suspend fun mutes(): JsonObject
    suspend fun media(ref: String): ByteArray
    suspend fun readDocument(url: String): JsonObject = throw UnsupportedOperationException("Document reader unavailable")
    suspend fun documentMedia(ref: String): ByteArray = media(ref)
    /**
     * What the reader shows around a document (`/api/v1/lark/raw/documents/<path>?url=…`): [path] is `reader/info`
     * (owner, counts), `header` (who edited it last, when) or `ai-summary` (Feishu's cached summary).
     */
    suspend fun documentExtra(path: String, url: String): JsonObject = throw UnsupportedOperationException("Document details unavailable")
    suspend fun momentsPost(postId: String): JsonObject = throw UnsupportedOperationException("Moments post unavailable")
    suspend fun momentsComments(postId: String): JsonObject = throw UnsupportedOperationException("Moments comments unavailable")
    suspend fun urlPreview(messageId: String, url: String): JsonObject = throw UnsupportedOperationException("URL preview unavailable")

    // 静音 (docs/inbox.md「静音」): an item's repository, chat or sender, in every source.
    /** `GET /api/v1/inbox/mutes`: `{mutes: [{id, source, source_name, tag, value, name, created_at}]}`. */
    suspend fun inboxMutes(): JsonObject = throw UnsupportedOperationException("Inbox mutes unavailable")
    /** `POST /api/v1/inbox/mutes` `{item_id, tag}`: `{mute, settled}`. */
    suspend fun muteInbox(itemId: String, tag: String): JsonObject = throw UnsupportedOperationException("Inbox mutes unavailable")
    /** `DELETE /api/v1/inbox/mutes/{id}`. */
    suspend fun unmuteInbox(id: String): Unit = throw UnsupportedOperationException("Inbox mutes unavailable")

    // A Feishu chat's Pin 消息 (iOS `LarkChatPins`): reading the list marks the pins read upstream.
    /** `GET /api/v1/lark/conversations/{feed}/pins?limit=50&account_id=&cursor=`. */
    suspend fun larkChatPins(feedId: String, accountId: String?, cursor: String?): JsonObject =
        throw UnsupportedOperationException("Chat pins unavailable")
    /** `GET /api/v1/lark/conversations/{feed}/pin-messages/{message}?account_id=&topic=`: where a pin's message is. */
    suspend fun larkPinTarget(feedId: String, messageId: String, accountId: String, topic: Boolean): JsonObject =
        throw UnsupportedOperationException("Chat pins unavailable")
    /** `POST /api/v1/lark/conversations/{feed}/pins` `{account_id, message_id, pin_id, remove}`: `{account_id, pinned}`. */
    suspend fun larkChangePin(feedId: String, messageId: String, pinId: String?, accountId: String): JsonObject =
        throw UnsupportedOperationException("Chat pins unavailable")
    /** `POST /api/v1/lark/conversations/{feed}/read-states` `{message_ids}`: receipts of my messages. */
    suspend fun larkReadStates(feedId: String, messageIds: List<String>): JsonObject =
        throw UnsupportedOperationException("Read receipts unavailable")
}

interface InboxAssistantConnection {
    val current: StateFlow<InboxAssistantTransport?>
}

interface AssistantConversations : AssistantRepository {
    val pinned: StateFlow<Set<SessionKey>>
    suspend fun startConversation(): SessionKey
    /** A new conversation that runs on [launcher], for it alone (iOS StarterComposer's model menu): the default stays. */
    suspend fun startConversation(launcher: String): SessionKey = throw UnsupportedOperationException("Picking a launcher unavailable")
}
