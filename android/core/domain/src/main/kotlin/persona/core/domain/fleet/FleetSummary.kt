package persona.core.domain.fleet

import persona.core.contracts.FleetItem
import persona.core.contracts.FleetState
import persona.core.contracts.SessionKey

/**
 * Reading the hub's fleet summary (server/internal/hub/liveactivity.go buildFleet: needs you first, oldest request
 * first, then working, longest first, then finished unseen, newest first, at most three items). The ongoing
 * notification, the home-screen widget and the Quick Settings tile all read it through here, so they agree.
 */
object FleetSummary {
    const val NEEDS_YOU = "needsYou"
    const val WORKING = "working"
    const val DONE = "done"

    /** With only finished agents left, the summary gives way to the connection line after this long (hub fleetDoneLinger). */
    const val DONE_LINGER_MILLIS = 5 * 60_000L

    /** The phase, worked out from the counts when a newer hub sends one this client does not know. */
    fun phase(state: FleetState): String = when (state.phase) {
        NEEDS_YOU, WORKING, DONE -> state.phase
        else -> when {
            state.needsYou > 0 -> NEEDS_YOU
            state.working > 0 -> WORKING
            state.unread > 0 -> DONE
            else -> ""
        }
    }

    fun isEmpty(state: FleetState): Boolean = phase(state).isEmpty()

    fun total(state: FleetState): Int = state.needsYou.coerceAtLeast(0) + state.working.coerceAtLeast(0) + state.unread.coerceAtLeast(0)

    /** Agents left out when only the first [showing] items are drawn (「+2 个」). */
    fun hidden(state: FleetState, showing: Int): Int = maxOf(0, total(state) - minOf(showing, state.items.size))

    /** The one agent to open on a tap, when there is exactly one; otherwise the tap opens the Agents list. */
    fun single(state: FleetState): SessionKey? = state.items.firstOrNull()?.let(::key)?.takeIf { total(state) == 1 }

    /** The first request a button can settle: a permission or a plan with its interaction. */
    fun firstActionable(state: FleetState): FleetItem? =
        state.items.firstOrNull { (it.kind == "permission" || it.kind == "plan") && !it.interaction.isNullOrEmpty() }

    /** Machine and session from the item's id ("machine/session", the session id may hold slashes). */
    fun key(item: FleetItem): SessionKey? {
        val slash = item.id.indexOf('/')
        return if (slash <= 0 || slash == item.id.lastIndex) null else SessionKey(item.id.substring(0, slash), item.id.substring(slash + 1))
    }

    /** Waiting on the user (iOS Item.needsYou); kinds this client does not know are requests too (iOS reads them as screen). */
    fun needsYou(item: FleetItem): Boolean = item.kind != "working" && item.kind != "unread"
}

/** The words, in one place (iOS FleetText), so the notification, widget and tile say the same thing. */
object FleetText {
    fun headline(state: FleetState): String = when (FleetSummary.phase(state)) {
        FleetSummary.NEEDS_YOU -> "${state.needsYou} 个需要你"
        FleetSummary.WORKING -> "${state.working} 个在工作"
        FleetSummary.DONE -> "${state.unread} 个做完了"
        else -> "都处理完了"
    }

    /** The other counts after the headline: 「3 个在工作 · 1 个做完了」. */
    fun rest(state: FleetState): String {
        val phase = FleetSummary.phase(state)
        return buildList {
            if (phase != FleetSummary.WORKING && state.working > 0) add("${state.working} 个在工作")
            if (phase != FleetSummary.DONE && state.unread > 0) add("${state.unread} 个做完了")
        }.joinToString(" · ")
    }

    /** What one agent is doing: 「授权：Bash: go test」/「计划等你批准」/ the question … */
    fun line(item: FleetItem): String = when (item.kind) {
        "permission" -> if (item.detail.isEmpty()) "等你授权" else "授权：" + item.detail
        "plan" -> if (item.detail.isEmpty()) "计划等你批准" else "计划：" + item.detail
        "question" -> item.detail.ifEmpty { "在问你" }
        "working" -> "工作中"
        "unread" -> item.detail.ifEmpty { "做完了" }
        else -> item.detail.ifEmpty { "终端等你处理" }
    }

    /** 「名字 · 在做什么」 */
    fun row(item: FleetItem): String = listOf(item.title, line(item)).filter { it.isNotEmpty() }.joinToString(" · ")

    /** 「名字 · 在做什么（机器 · 空间 › 标签）」 */
    fun rowWithPlace(item: FleetItem): String = row(item) + if (item.place.isNotEmpty()) "（${item.place}）" else ""

    /** What a locked screen says instead of the agent's name and content. */
    fun anonymous(item: FleetItem): String = when (item.kind) {
        "permission" -> "有个授权等你"
        "plan" -> "有个计划等你批准"
        "question" -> "有个提问等你回答"
        "working" -> "工作中"
        "unread" -> "做完了"
        else -> "有个终端等你处理"
    }

    /**
     * Requests without names or content, for a notification that may show on the lock screen (锁屏显示详情 off):
     * 「有个授权等你」, 「2 个计划等你批准」 …, one line per kind in the order they came.
     */
    fun anonymousRequests(items: List<FleetItem>): List<String> =
        items.filter(FleetSummary::needsYou).groupBy { requestKind(it.kind) }.map { (kind, group) -> anonymousCount(kind, group.size) }

    /** 「有个授权等你」 for one, 「3 个授权等你」 for more. */
    fun anonymousCount(kind: String, count: Int): String {
        val what = when (requestKind(kind)) {
            "permission" -> "授权等你"
            "plan" -> "计划等你批准"
            "question" -> "提问等你回答"
            else -> "终端等你处理"
        }
        return if (count == 1) "有个$what" else "$count 个$what"
    }

    /** A kind this client does not know waits on a screen (iOS decodes it as screen). */
    private fun requestKind(kind: String) = if (kind == "permission" || kind == "plan" || kind == "question") kind else "screen"

    fun more(count: Int): String = "+$count 个"
}
