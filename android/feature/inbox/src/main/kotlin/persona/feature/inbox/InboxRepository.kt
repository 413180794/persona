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
    suspend fun draftReply(itemId: String, instruction: String): String = error("AI 草拟尚未接入") // i18n-ignore: developer error for a repository without the hub

    /** 交给 AI: the item as context with what to do, queued for Persona's main conversation or a running Agent. */
    suspend fun handOff(request: InboxHandOffRequest): Unit = error("交给 AI 尚未接入") // i18n-ignore: developer error for a repository without the hub
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
        bulk(items, if (done) persona.core.contracts.api.InboxBulkAction.Done else persona.core.contracts.api.InboxBulkAction.Reopen)
    /** 全部标为已处理 / 全部忽略 / 移回 over [items] in one request ([setDone] is its done / reopen). */
    suspend fun bulk(items: List<persona.core.contracts.api.InboxDoneTarget>, action: persona.core.contracts.api.InboxBulkAction): InboxBulkResult =
        error("批量处理尚未接入") // i18n-ignore: developer error for a repository without the hub
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

    /** 已静音 (docs/inbox.md「静音」): what the user muted, in every source, newest first. */
    suspend fun inboxMutes(): List<InboxMute> = error("静音尚未接入") // i18n-ignore: developer error for a repository without the hub
    /** 静音这个仓库 / 会话 / 发件人: the item and the open ones of its source it covers are done at once; a failure puts them back. */
    suspend fun mute(record: InboxRecord, option: InboxMuteOption): Unit = error("静音尚未接入") // i18n-ignore: developer error
    suspend fun unmute(id: String): Unit = error("静音尚未接入") // i18n-ignore: developer error

    /** A chat's Pin 消息, a page at a time ([accountId] null: the hub's current account, which the page names). */
    suspend fun larkChatPins(feedId: String, accountId: String?, cursor: String?): LarkChatPinsPage = error("Chat pins unavailable")
    /** Where a pin's message is: the message itself, and the thread's root when it is a reply in one ([topic]: a 话题群). */
    suspend fun larkPinTarget(feedId: String, messageId: String, accountId: String, topic: Boolean): LarkPinTarget = error("Chat pins unavailable")
    /** 添加 Pin ([pinId] null) or 移除 Pin; throws [LarkPinFailure] when the hub answers otherwise. */
    suspend fun larkChangePin(feedId: String, messageId: String, pinId: String?, accountId: String?): Unit = error("Chat pins unavailable")
    /** Read receipts of my messages; ones the Lark gateway has none for are left out. */
    suspend fun larkReadStates(feedId: String, messageIds: List<String>): List<LarkReadState> = emptyList()
    /** The account''t be read (then kept content stays hidden and nothing is decoded). */
    suspend fun larkMessageDisplay(): LarkMessageDisplay? = null
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
    /** A Feishu document's notice (`doc`): no chat behind it, its own page (InboxDocDetail.kt). */
    val doc: InboxDocRef? = null,
    /** A hub connector's item (GitHub, Telegram, an extension's): its thread opens as a conversation (ConnectorThread.kt). */
    val connector: InboxConnectorRef? = null,
    /** What it can be muted by (the row's menu: 静音这个仓库 / 会话 / 发件人); an older hub sends none. */
    val mutes: List<InboxMuteOption> = emptyList(),
    /** A Feishu chat's kind (`lark.is_group` / `is_topic`); null for one listed or cached before the hub said. */
    val isGroup: Boolean? = null,
    val isTopic: Boolean? = null,
) {
    /** A hub connector's item (not Feishu's, not mail): it opens as its connector's thread. */
    val isConnector: Boolean get() = connector != null && !isLark && !isMail && moment == null

    val isLark: Boolean get() = feedId != null || source == "lark"
    /** A mail item: only these open the mail page; the kind says it too in records cached without their source. */
    val isMail: Boolean get() = mail != null || source == "mail" || (source.isEmpty() && item.kind == "mail")
}

/**
 * An item's `connector` block (docs/protocol.md「收件箱」): the [thread] its connector reads it as, the [label] the
 * connector gave it (the row's tag, its words: 私聊, @我, 转账…), and whether and where a reply goes.
 */
@Serializable
data class InboxConnectorRef(
    val thread: String,
    val label: String? = null,
    val canReply: Boolean = false,
    val replyTo: String? = null,
)

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

/** [ai]: the text came from AI (the AI 分身, or AI 草拟); sending it asks first, sending the user''s kind, from its inbox row; null when the inbox does not list it (read receipts go by the counts then). */
    val isGroup: Boolean? = null,
    /** A topic group (话题群): Pin 消息 resolves threads with it. */
    val isTopic: Boolean = false,
)

enum class LarkFailure(@androidx.annotation.StringRes val text: Int) {
    NotPaired(R.string.inbox_lark_not_paired),
    Incomplete(R.string.inbox_lark_incomplete),
    LoadFailed(R.string.inbox_lark_messages_failed),
}

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
    /** False for what is not the sender''s main conversation. [operationId] stays the same while the same
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
