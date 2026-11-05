package persona.feature.inbox

import persona.core.domain.inbox.InboxInteraction
import persona.core.domain.inbox.InboxItem
import persona.core.domain.sort.AgentPreferences
import persona.core.domain.sort.AgentSession
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.util.UUID
import persona.core.contracts.LarkReaction
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.MailTransport
import persona.core.contracts.api.OutboxMessage
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf

private val emptyReplyComposers: StateFlow<ReplyComposers?> = MutableStateFlow(null)

private val disconnectedMail: StateFlow<MailTransport?> = MutableStateFlow(null)
private val emptyPersonaOutbox: StateFlow<List<OutboxMessage>> = MutableStateFlow(emptyList())

/** Feature capabilities until the shared repository contract exposes inbox details. */
interface InboxRepository : persona.core.contracts.api.InboxRepository {
    val supportsThreadOutbox: Boolean get() = false
    val replyComposers: StateFlow<ReplyComposers?> get() = emptyReplyComposers
    suspend fun readingStore(): LarkReadingStore? = null
    val snapshot: StateFlow<InboxSnapshot>
    val mailTransport: StateFlow<MailTransport?> get() = disconnectedMail
    val personaOutbox: StateFlow<List<OutboxMessage>> get() = emptyPersonaOutbox
    val connectionState: Flow<ConnectionState> get() = flowOf(ConnectionState.Connecting)
    val preferences: Flow<InboxPreferences> get() = flowOf(InboxPreferences())
    suspend fun savePreferences(value: InboxPreferences) = Unit
    suspend fun refreshNow() = refresh()
    suspend fun askPersona(itemId: String, action: InboxAiAction) {
        error("Persona entry is not connected")
    }
    suspend fun askPersona(request: InboxPersonaRequest) = askPersona(request.itemId, request.action)
    suspend fun personaContext(itemId: String): String? = null

    /**
     * A reply in the user's voice drafted by the hub (`POST /api/v1/inbox/{id}/draft`); [instruction] may be empty.
     * The text only fills the reply for the user to edit: sending it is a separate, confirmed step.
     */
    suspend fun draftReply(itemId: String, instruction: String): String = error("AI 草拟尚未接入")

    /** 交给 AI: the item as context with what to do, queued for Persona's main conversation or a running Agent. */
    suspend fun handOff(request: InboxHandOffRequest): Unit = error("交给 AI 尚未接入")
    fun replyUpdates(target: ReplyTarget): Flow<Unit> = kotlinx.coroutines.flow.emptyFlow()
    suspend fun replyStatus(target: ReplyTarget, clientId: String): ReplyDelivery = ReplyDelivery.Ready
    suspend fun verifyReply(target: ReplyTarget, clientId: String): ReplyDelivery = ReplyDelivery.Unknown
    fun conversation(feedId: String, startPosition: Long?): Flow<LarkConversation>
    suspend fun loadOlder(feedId: String)
    suspend fun refreshConversation(feedId: String)
    suspend fun loadNewer(feedId: String) = refreshConversation(feedId)
    suspend fun latestConversation(feedId: String) = refreshConversation(feedId)
    suspend fun markVisible(feedId: String, messages: List<LarkMessage>) = Unit
    suspend fun setDone(itemId: String, done: Boolean)
    /**
     * 全部标为已处理 / 全部看过 / 全部清除 and their 撤销: [items] done (or back) in one request, each as listed, so one
     * that took new messages since stays open ([InboxBulkResult.changedSince]).
     */
    suspend fun setDone(items: List<persona.core.contracts.api.InboxDoneTarget>, done: Boolean): InboxBulkResult =
        error("批量处理尚未接入")
    suspend fun readDraft(target: ReplyTarget): ReplyDraft?
    suspend fun saveDraft(target: ReplyTarget, draft: ReplyDraft)

    /** Persist to the scoped outbox before transport; encode clientId as client_id.
     * Retry with the same ID, reconcile only by client ID or server message ID, and
     * publish pending/confirmed bubbles through conversation. Inbox replies mark done.
     * Clear the stored draft only if its ID still matches this request after acceptance.
     */
    suspend fun reply(target: ReplyTarget, request: ReplyRequest)

    /** Download authenticated media and return a content URI suitable for a read grant. */
    suspend fun downloadFile(file: LarkPart.File): String

    suspend fun momentsPost(postId: String): persona.core.contracts.LarkMomentsPost = error("Moments post unavailable")
    suspend fun momentsComments(postId: String): List<persona.core.contracts.LarkMomentsComment> = emptyList()
    suspend fun urlPreview(messageId: String, url: String): persona.core.contracts.LarkUrlPreview? = null
    suspend fun react(messageId: String, key: String, remove: Boolean) = Unit
    suspend fun cardAction(messageId: String, actionId: String) = Unit
    suspend fun userProfile(userId: String, feedId: String? = null): kotlinx.serialization.json.JsonObject? = null
    suspend fun quickReactions(): List<String> = emptyList()
    suspend fun emojis(): kotlinx.serialization.json.JsonObject? = null
}

data class InboxSnapshot(
    val records: List<InboxRecord> = emptyList(),
    val interactions: List<InboxInteraction> = emptyList(),
    val sessions: List<AgentSession> = emptyList(),
    val mutes: AgentPreferences = AgentPreferences(),
    val loading: Boolean = false,
    val error: String? = null,
    val contentSource: LarkContentSource? = null,
    /** Machine names by id, for saying where an Agent runs. */
    val machines: Map<String, String> = emptyMap(),
    /** How triage stands: new items waiting outside the list (#1773); null from an older hub. */
    val triage: InboxTriage? = null,
    /** How many refreshes have succeeded. A new count says the records are the hub's whole list, even when it is empty. */
    val refreshes: Int = 0,
)

data class InboxRecord(
    val item: InboxItem,
    val sourceLabel: String = "",
    val feedId: String? = null,
    val startPosition: Long? = null,
    val count: Int = 0,
    val agent: InboxAgent? = null,
    val localStatus: String? = null,
    val localDelivery: persona.core.contracts.api.DeliveryState? = null,
    /** lark | mail (also lark for documents and posts without a chat). */
    val source: String = "",
    /** A Feishu mention of everyone (@所有人) rather than of the user. */
    val atAll: Boolean = false,
    val mail: InboxMailRef? = null,
    /** A saved reply that AI wrote (AI 草拟): its row shows it as a draft card, not as 「草稿 · …」. */
    val aiDraft: Boolean = false,
    /** A Feishu chat's picture (the group's, or the other person's), at the thumbnail size. */
    val avatar: AuthenticatedLarkImage? = null,
    val read: Boolean = false,
    val moment: InboxMomentRef? = null,
    /** The hub's translations of a mail item, by target language, done as it came in (设置 › 翻译). */
    val translations: Map<String, persona.core.contracts.api.ItemTranslation> = emptyMap(),
) {
    val isLark: Boolean get() = feedId != null || source == "lark"
}

/** A 公司圈 post reference on an inbox item. */
data class InboxMomentRef(
    val postId: String,
    val notice: String? = null,
    val replyTo: String? = null,
    val myPost: Boolean = false,
    val postText: String? = null,
)

/** Where a mail item's messages are: what 交给 AI tells the agent so it can read them with its mail tools. */
data class InboxMailRef(
    val accountId: String,
    val mailbox: String = "INBOX",
    val uids: List<Long> = emptyList(),
    /** False when the user was only copied. */
    val direct: Boolean = true,
)

@Serializable
data class InboxAgent(
    val state: String,
    val draft: String = "",
    val basis: String = "",
    val task: String = "",
    val machineId: String? = null,
    val sessionId: String? = null,
)

@Serializable
sealed interface ReplyTarget {
    @Serializable data class Inbox(val itemId: String) : ReplyTarget
    @Serializable data class Lark(val feedId: String) : ReplyTarget
}

@Serializable
data class ReplyRequest(val text: String, @SerialName("client_id") val clientId: String,
    val mailTarget: persona.core.contracts.api.MailReplyTarget? = null)

/** [ai]: the text came from AI (the AI 分身, or AI 草拟); sending it asks first, sending the user's own words does not. */
@Serializable
data class ReplyDraft(val text: String, val clientId: String = UUID.randomUUID().toString(), val ai: Boolean = false)

data class LarkConversation(
    val title: String = "飞书会话",
    val messages: List<LarkMessage> = emptyList(),
    val loading: Boolean = false,
    val hasOlder: Boolean = false,
    val loadingOlder: Boolean = false,
    val error: String? = null,
    val hasNewer: Boolean = false,
    val loadingNewer: Boolean = false,
    val verified: Boolean = false,
    val windowRevision: Long = 0,
)

enum class LarkDelivery { Sent, Sending, Failed, WaitingForNetwork }

data class LarkMessage(
    val id: String,
    val senderId: String,
    val senderName: String,
    val position: Long? = null,
    val time: String = "",
    val mine: Boolean = false,
    val parts: List<LarkPart>,
    val clientId: String? = null,
    val delivery: LarkDelivery = LarkDelivery.Sent,
    val pendingDelivery: persona.core.contracts.api.DeliveryState? = null,
    val avatar: AuthenticatedLarkImage? = null,
    val bot: Boolean = false,
    val system: Boolean = false,
    val edited: Boolean = false,
    val quote: LarkQuote? = null,
    val badgeCount: Long? = null,
    val isRead: Boolean = false,
    val replyCount: Long = 0,
    val thread: LarkThreadSource? = null,
    val reactions: List<LarkReaction> = emptyList(),
    /** False for what is not the sender's own text (recalled, encrypted): 设置 › 翻译 leaves it alone. */
    val translatable: Boolean = true,
)

data class LarkQuote(val id: String, val sender: String, val text: String)

data class LarkForwardedMessage(
    val id: String = "",
    val senderName: String = "",
    val senderAvatar: AuthenticatedLarkImage? = null,
    val time: String = "",
    val text: String = "",
    val parts: List<LarkPart> = emptyList(),
)

sealed interface LarkPart {
    data class Text(val text: String, val source: LarkContentSource? = null) : LarkPart
    data class Card(val content: LarkCardContent, val source: LarkContentSource, val messageId: String? = null) : LarkPart
    // Models may carry an authenticated Coil ImageRequest supplied by the data layer.
    data class Image(val model: Any, val description: String = "图片", val width: Int = 1, val height: Int = 1) : LarkPart
    data class File(val ref: String, val name: String, val mimeType: String = "application/octet-stream") : LarkPart
    data class External(val title: String, val larkUrl: String, val kind: String, val duration: String = "", val cover: Image? = null) : LarkPart
    data class Code(val code: String, val language: String = "") : LarkPart
    data class Audio(val ref: String, val durationMs: Long, val source: LarkContentSource? = null) : LarkPart
    data class Video(val ref: String, val durationMs: Long, val cover: Image? = null, val source: LarkContentSource? = null) : LarkPart
    data class Sticker(val model: Any, val description: String = "表情", val width: Int = 1, val height: Int = 1) : LarkPart
    data class MergeForward(val title: String, val messages: List<LarkForwardedMessage>, val source: LarkContentSource? = null) : LarkPart
}

internal fun startsInboxItem(message: LarkMessage, previous: LarkMessage?, start: Long?): Boolean =
    start != null && message.position != null && message.position >= start &&
        (previous?.position == null || previous.position < start)

internal fun showsSender(message: LarkMessage, previous: LarkMessage?, boundary: Boolean): Boolean =
    boundary || previous == null || previous.senderId != message.senderId || previous.mine != message.mine

@Serializable
data class InboxPreferences(
    val source: String = "",
    val kind: String = "",
    val query: String = "",
    val section: String = "",
    val expanded: List<String> = listOf("action", "fyi"),
)

enum class InboxAiAction { Summarize, DraftReply, Handoff }
@Serializable
data class InboxPersonaRequest(
    val itemId: String,
    val action: InboxAiAction,
    val operationId: String = UUID.randomUUID().toString(),
    val context: String? = null,
)
@Serializable
enum class ReplyDelivery { Ready, Queued, WaitingForNetwork, Sending, Unknown, Verifying, Refused, NeedsConfirmation, Sent }

/**
 * One 交给 AI. No [machineId] / [sessionId]: Persona's main conversation. [operationId] stays the same while the same
 * text goes to the same place, so a retry after an unclear failure is not queued twice.
 */
@Serializable
data class InboxHandOffRequest(
    val itemId: String,
    val text: String,
    val machineId: String? = null,
    val sessionId: String? = null,
    val operationId: String = UUID.randomUUID().toString(),
)
