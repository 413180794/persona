package persona.feature.chat

import persona.core.chatlist.ChatListWindow
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.lines.*
import persona.core.designsystem.StatusLineState
import persona.core.designsystem.StatusLineKind
import persona.core.designsystem.resolveStatusLine
import persona.core.domain.lines.Fold as LineFold
import persona.core.domain.lines.TaskNote as LineTask

internal fun Item.asLineItem() = LineItem(this,
    fold?.let { LineFold(it.items, it.tools, it.thoughts, it.throughSeq, it.throughId, parseTime(it.until)) },
    task?.let { LineTask(it.kind, it.title, it.status, it.toolUseId, it.taskId, it.detail, it.exitCode) })

internal val chatItemOrder = compareBy<Item> { it.seq }.thenBy { it.id.substringAfterLast('#', "").toIntOrNull() ?: 0 }

internal fun ChatLine.lastItem(): Item? = when (this) {
    is ChatLine.Row -> row.item?.item
    is ChatLine.MergedUsers -> users.lastOrNull()?.item?.item
    is ChatLine.UserImages -> images.lastOrNull()?.item
    is ChatLine.Work -> steps.mapNotNull { it.lastItem() }.maxWithOrNull(chatItemOrder)
    is ChatLine.Step -> step.lastItem()
    is ChatLine.Wakes -> turn.wakes.lastOrNull()?.item
    else -> null
}

private fun WorkItem.lastItem(): Item? = when (this) {
    is WorkItem.Tool -> (call.result ?: call.use).item
    is WorkItem.Thought -> item.item
    is WorkItem.Narration -> item.item
    is WorkItem.Folded -> item.item.copy(seq = item.fold?.throughSeq ?: item.seq)
}

/** Keep the actual item snapshot: changing turn keys cannot move a historical reading window. */
internal class ChatTimeline {
    private val window = ChatListWindow<Item>({ it.id },
        chatItemOrder,
        { it.role == "assistant" && it.kind == "text" })
    private var frozenLiveId: String? = null
    private val expanded = mutableMapOf<String, Pair<Item, List<Item>>>()

    @Synchronized fun freeze(items: List<Item>, liveId: String?) {
        if (!window.isFrozen) { window.freeze(items); frozenLiveId = liveId }
    }
    @Synchronized fun follow() { window.follow(); frozenLiveId = null }
    @Synchronized fun newMessageCount(items: List<Item>, below: Item?, lastRead: Item?) = window.newMessageCount(items, below, lastRead)
    @Synchronized fun unfold(fold: Item, items: List<Item>) { expanded[fold.id] = fold to items }

    @Synchronized fun searchItems(items: List<Item>): List<Item> = window.items(items).flatMap { item ->
        expanded[item.id]?.takeIf { it.first.fold == item.fold }?.second ?: listOf(item)
    }

    @Synchronized fun lines(items: List<Item>, liveId: String?, open: Set<String>): persona.core.domain.lines.ChatSnapshot {
        return chatLines(searchItems(items).filter { it.kind != "turn_duration" }.map(Item::asLineItem), LineOptions(
            liveTurnId = if (window.isFrozen) frozenLiveId else liveId, openWork = open))
    }
}

internal fun ChatLine.recordedDuration(durations: Map<String, Long>): Long? = when (this) {
    is ChatLine.Work -> durations[id.removePrefix("wt-").removePrefix("w-").removePrefix("dw-")]
    else -> null
}

internal fun recordedDurationLabel(seconds: Long?): String? = seconds?.takeIf { it >= 0 }?.let { "用时 $it 秒" }

internal typealias StatusKind = StatusLineKind

internal fun chatStatusState(
    snapshot: persona.core.contracts.api.ChatSnapshot,
    session: Session?, machine: Machine?, messages: List<OutboxMessage>,
    requests: List<Interaction>, answering: Boolean,
    operationUnknown: Boolean = false,
): StatusLineState = StatusLineState(
    ended = snapshot.ended,
    replaced = snapshot.replaced,
    machineOffline = machine?.online == false,
    submitting = messages.any { it.state == DeliveryState.Sending },
    answering = answering,
    unconfirmed = operationUnknown || messages.any { it.state.resultUnknown },
    pendingRequests = requests.size,
    blocked = session?.status == "blocked",
    requestSummary = requests.singleOrNull()?.headline,
    stopping = snapshot.stopping,
    working = session?.status == "working",
    waiting = session?.loop != null || !session?.background.isNullOrEmpty(),
    waitingDetail = when {
        session?.loop != null -> "循环中"
        !session?.background.isNullOrEmpty() -> "等后台任务 · ${session?.background?.size} 个"
        else -> null
    },
)

internal fun chatStatus(
    snapshot: persona.core.contracts.api.ChatSnapshot,
    session: Session?, machine: Machine?, messages: List<OutboxMessage>,
    requests: List<Interaction>, answering: Boolean,
) = resolveStatusLine(chatStatusState(snapshot, session, machine, messages, requests, answering))

internal fun ChatLine.contentType(): String = when (this) {
    is ChatLine.Row -> row.type
    is ChatLine.Work -> "work"
    is ChatLine.Step -> when (step) { is WorkItem.Tool -> "tool"; is WorkItem.Folded -> "fold"; else -> "thought" }
    is ChatLine.End -> "work_end"
    is ChatLine.MergedUsers -> "merged_users"
    is ChatLine.SplitHeader -> "split_header"
    is ChatLine.UserImages -> "images"
    is ChatLine.Wakes -> "wakes"
}

internal fun interactionSource(interaction: Interaction, machine: Machine?): String = listOfNotNull(
    interaction.sessionTitle?.takeIf { it.isNotBlank() },
    machine?.takeIf { it.id == interaction.machineId }?.name?.takeIf { it.isNotBlank() } ?: "未知机器",
).joinToString(" · ")

internal fun interactionDeadline(expires: java.time.Instant, now: java.time.Instant): String {
    return requireNotNull(persona.core.domain.answer.RequestExpiry.label(expires.toString(), now))
}
