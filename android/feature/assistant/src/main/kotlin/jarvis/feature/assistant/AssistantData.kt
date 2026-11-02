package jarvis.feature.assistant

import jarvis.core.contracts.Session
import jarvis.core.contracts.AgentState
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.AssistantRepository
import jarvis.core.contracts.api.AssistantSnapshot
import jarvis.core.contracts.api.DeliveryState
import jarvis.core.contracts.api.MessageSender
import jarvis.core.contracts.api.OutboxMessage
import jarvis.core.contracts.api.MainConversationSender
import jarvis.core.contracts.api.DraftSnapshot
import jarvis.core.contracts.api.PendingMainConversation
import jarvis.core.designsystem.PendingStatus
import jarvis.core.designsystem.Status
import jarvis.core.designsystem.StatusLineState
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.util.UUID

internal data class ConversationGroups(val main: Session?, val pinned: List<Session>, val recent: List<Session>)

internal fun conversationGroups(
    snapshot: AssistantSnapshot,
    query: String,
    pinned: Set<SessionKey>,
): ConversationGroups {
    val matches = snapshot.conversations.distinctBy { it.key }.filter {
        (query.isBlank() || it.conversationTitle().contains(query.trim(), ignoreCase = true) ||
                it.lastText.orEmpty().contains(query.trim(), ignoreCase = true))
    }.sortedWith(compareByDescending<Session> { it.updated }.thenBy { it.key.path })
    return ConversationGroups(matches.firstOrNull { it.key == snapshot.mainSession },
        matches.filter { it.key != snapshot.mainSession && it.key in pinned },
        matches.filter { it.key != snapshot.mainSession && it.key !in pinned })
}

internal fun greeting(hour: Int): String = when (hour) {
    in 5..10 -> "早上好"
    in 11..13 -> "中午好"
    in 14..17 -> "下午好"
    else -> "晚上好"
}

/** Local fixtures never submit messages to a hub. */
internal class DemoAssistantRepository : AssistantRepository, MessageSender, MainConversationSender {
    private val snapshot = MutableStateFlow(AssistantSnapshot(conversations = listOf(
        Session(machineId = "demo", sessionId = "weekly", title = "周报素材", assistant = true,
            lastText = "整理这周完成的工作", updatedAt = "2026-10-30T09:00:00Z"),
        Session(machineId = "demo", sessionId = "travel", title = "下周出差的行程", assistant = true,
            updatedAt = "2026-10-29T09:00:00Z"),
        Session(machineId = "demo", sessionId = "document", title = "文档的要点", assistant = true,
            updatedAt = "2026-10-28T09:00:00Z"),
    )))
    override val assistant = snapshot.asStateFlow()
    private val messages = MutableStateFlow<List<OutboxMessage>>(emptyList())
    override val outbox = messages.asStateFlow()
    val pinned = setOf(SessionKey("demo", "weekly"))

    override suspend fun refresh() = Unit

    override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) {
        send(assistant.value.mainSession ?: start(), text, clientMessageId)
        messages.update { all -> all.map { if (it.clientMessageId == clientMessageId) it.copy(mainTarget = true) else it } }
    }

    suspend fun start(): SessionKey {
        val key = SessionKey("demo", UUID.randomUUID().toString())
        snapshot.update {
            it.copy(mainSession = it.mainSession ?: key, conversations = listOf(
                Session(machineId = key.machineId, sessionId = key.sessionId,
                    title = "新对话", assistant = true),
            ) + it.conversations)
        }
        return key
    }

    override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
        messages.update { previous ->
            previous.filterNot { it.clientMessageId == clientMessageId } +
                OutboxMessage(clientMessageId, session, text, DeliveryState.Sent)
        }
        snapshot.update { current -> current.copy(conversations = current.conversations.map {
            if (it.key == session) it.copy(title = text.take(24), lastText = text) else it
        }) }
    }
}


internal fun Session.conversationTitle(): String = title.takeIf { it.isNotBlank() && it != "Claude Code" }
    ?: lastText?.lineSequence()?.firstOrNull { it.isNotBlank() }?.trim()?.take(30) ?: "对话"

internal fun Session.conversationStatus(): Status = when (agentState) {
    AgentState.NeedsYou -> Status.NeedsYou
    AgentState.Working -> Status.Working
    AgentState.Unread -> Status.Unread
    AgentState.Idle -> Status.Idle
    AgentState.Offline -> Status.Offline
    AgentState.Unknown -> Status.Unknown
}

/** Null targets always enter the durable main queue before any network request. */
internal suspend fun sendAssistantMessage(
    main: MainConversationSender?, sender: MessageSender, session: SessionKey?, text: String, id: String,
) {
    if (session == null || session == PendingMainConversation) {
        checkNotNull(main) { "主对话发送尚未接入" }.sendToMain(text.trim(), id)
    } else sender.send(session, text.trim(), id)
}

internal fun DeliveryState.pendingStatus(): PendingStatus? = when (this) {
    DeliveryState.Queued, DeliveryState.WaitingForNetwork -> PendingStatus.Waiting
    DeliveryState.Sending -> PendingStatus.Sending
    DeliveryState.Unknown -> PendingStatus.Unconfirmed
    DeliveryState.Verifying -> PendingStatus.Verifying
    DeliveryState.NeedsConfirmation -> PendingStatus.NeedsReview
    DeliveryState.Rejected, DeliveryState.Refused -> PendingStatus.Refused
    DeliveryState.Sent -> null
}

internal fun assistantStatus(session: Session?, messages: List<OutboxMessage>) = StatusLineState(
    ended = session?.agentState == AgentState.Offline,
    submitting = messages.any { it.state == DeliveryState.Sending },
    unconfirmed = messages.any { it.state in setOf(DeliveryState.Unknown, DeliveryState.Verifying, DeliveryState.NeedsConfirmation) },
    blocked = session?.agentState == AgentState.NeedsYou,
    working = session?.agentState == AgentState.Working,
)

internal fun conversationTime(at: java.time.Instant?, zone: java.time.ZoneId = java.time.ZoneId.systemDefault()): String? =
    at?.atZone(zone)?.format(java.time.format.DateTimeFormatter.ofPattern("M月d日 HH:mm"))

/** The composition root adapts these options to the shared chat lane's presentation parameters. */
data class AssistantChatOptions(
    val assistant: Boolean = true,
    val embedded: Boolean = true,
    val showTopBar: Boolean = false,
)

/** Cancellation must reach the owning screen instead of turning into an error message. */
internal suspend inline fun <T> assistantAttempt(block: () -> T): Result<T> = try {
    Result.success(block())
} catch (cancelled: kotlinx.coroutines.CancellationException) {
    throw cancelled
} catch (error: Exception) {
    Result.failure(error)
}
