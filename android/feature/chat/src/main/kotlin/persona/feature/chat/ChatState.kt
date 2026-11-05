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
    is ChatLine.UserImages -> images.lastOrNull()?.item
    is ChatLine.Work -> steps.mapNotNull { it.lastItem() }.maxWithOrNull(chatItemOrder)
    is ChatLine.Step -> step.lastItem()
    is ChatLine.Wakes -> turn.wakes.lastOrNull()?.item
    is ChatLine.ApiError -> item.item
    is ChatLine.Recaps -> items.lastOrNull()?.item
    else -> null
}

/** The transcript item a visible line stands for when read receipts count it: a row, or the answer an error card is. */
internal fun ChatLine.seenItem(): Item? = when (this) {
    is ChatLine.Row -> row.item?.item
    is ChatLine.ApiError -> item.item
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
    private var shownLiveId: String? = null
    private val expanded = mutableMapOf<String, Pair<Item, List<Item>>>()

    /** A frozen window keeps the turn that was running when it froze, even after a newer one starts. */
    @Synchronized fun freeze(items: List<Item>) {
        if (!window.isFrozen) { window.freeze(items); frozenLiveId = shownLiveId }
    }
    @Synchronized fun follow() { window.follow(); frozenLiveId = null }
    @Synchronized fun newMessageCount(items: List<Item>, below: Item?, lastRead: Item?) = window.newMessageCount(items, below, lastRead)
    @Synchronized fun unfold(fold: Item, items: List<Item>) { expanded[fold.id] = fold to items }

    /**
     * A frozen window keeps the fold it froze with and also takes in the work the store put in its place when the fold
     * was opened: the fold's own items then come twice, and every step of the open work line counted twice.
     */
    @Synchronized fun searchItems(items: List<Item>): List<Item> = window.items(items).flatMap { item ->
        expanded[item.id]?.takeIf { it.first.fold == item.fold }?.second ?: listOf(item)
    }.distinctBy { it.id }

    /** [live]: the agent is at work; the running turn is then the last one (`chatLines`). */
    @Synchronized fun lines(items: List<Item>, live: Boolean, open: Set<String>): persona.core.domain.lines.ChatSnapshot {
        val options = if (window.isFrozen) LineOptions(liveTurnId = frozenLiveId, openWork = open)
            else LineOptions(openWork = open, live = live)
        return chatLines(searchItems(items).filter { it.kind != "turn_duration" }.map(Item::asLineItem), options)
            .also { if (!window.isFrozen) shownLiveId = it.liveTurnId }
    }
}

internal fun ChatLine.recordedDuration(durations: Map<String, Long>): Long? = when (this) {
    is ChatLine.Work -> durations[id.removePrefix("w-").removePrefix("dw-")]
    else -> null
}

internal fun recordedDurationLabel(res: android.content.res.Resources, seconds: Long?): String? =
    seconds?.takeIf { it >= 0 }?.let { res.getString(R.string.chat_state_took_seconds, it) }

internal typealias StatusKind = StatusLineKind

internal fun chatStatusState(
    res: android.content.res.Resources,
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
    requestSummary = requests.singleOrNull()?.headline?.shown,
    stopping = snapshot.stopping,
    working = session?.status == "working",
    waiting = session?.loop != null || !session?.background.isNullOrEmpty() || session?.runningWorkflows?.isNotEmpty() == true,
    waitingDetail = when {
        session?.loop != null -> res.getString(R.string.chat_state_looping)
        // a running workflow is in background too: counted once (#2935)
        (session?.backgroundCount ?: 0) > 0 -> session?.backgroundCount?.let { res.getQuantityString(R.plurals.chat_state_background, it, it) }
        else -> null
    },
)

internal fun chatStatus(
    res: android.content.res.Resources,
    snapshot: persona.core.contracts.api.ChatSnapshot,
    session: Session?, machine: Machine?, messages: List<OutboxMessage>,
    requests: List<Interaction>, answering: Boolean,
) = resolveStatusLine(chatStatusState(res, snapshot, session, machine, messages, requests, answering))

internal fun ChatLine.contentType(): String = when (this) {
    is ChatLine.Row -> row.type
    is ChatLine.Work -> "work"
    is ChatLine.Step -> when (step) { is WorkItem.Tool -> "tool"; is WorkItem.Folded -> "fold"; else -> "thought" }
    is ChatLine.End -> "work_end"
    is ChatLine.UserImages -> "images"
    is ChatLine.Wakes -> "wakes"
    is ChatLine.ApiError -> "api_error"
    is ChatLine.Recaps -> "recaps"
    is ChatLine.Extra -> "extra"
}

internal fun interactionSource(res: android.content.res.Resources, interaction: Interaction, machine: Machine?): String = listOfNotNull(
    interaction.sessionTitle?.takeIf { it.isNotBlank() },
    machine?.takeIf { it.id == interaction.machineId }?.name?.takeIf { it.isNotBlank() } ?: res.getString(R.string.chat_state_unknown_machine),
).joinToString(" · ")

/** Where a request comes from, in the page's language. */
@androidx.compose.runtime.Composable
internal fun interactionSource(interaction: Interaction, machine: Machine?): String =
    interactionSource(androidx.compose.ui.platform.LocalResources.current, interaction, machine)

internal fun interactionDeadline(expires: java.time.Instant, now: java.time.Instant): String {
    return requireNotNull(persona.core.domain.answer.RequestExpiry.label(expires.toString(), now)).shown
}
