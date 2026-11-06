package persona.core.domain.fleet

import persona.core.contracts.CoreText
import persona.core.contracts.CoreText.Key
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

    /** Agents left out when only the first [showing] items are drawn ("+2"). */
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

/**
 * The words, in one place (iOS FleetText), so the notification, widget and tile say the same thing. They are
 * [CoreText]: core/designsystem turns them into the shown language (`resolve` / `text()`).
 */
object FleetText {
    fun headline(state: FleetState): CoreText = when (FleetSummary.phase(state)) {
        FleetSummary.NEEDS_YOU -> CoreText.of(Key.FleetNeedsYou, state.needsYou)
        FleetSummary.WORKING -> CoreText.of(Key.FleetWorking, state.working)
        FleetSummary.DONE -> CoreText.of(Key.FleetDone, state.unread)
        else -> CoreText.of(Key.FleetAllDone)
    }

    /** The other counts after the headline: "3 working · 1 done"; empty when there are none. */
    fun rest(state: FleetState): CoreText {
        val phase = FleetSummary.phase(state)
        return CoreText.joined(" · ", buildList {
            if (phase != FleetSummary.WORKING && state.working > 0) add(CoreText.of(Key.FleetWorking, state.working))
            if (phase != FleetSummary.DONE && state.unread > 0) add(CoreText.of(Key.FleetDone, state.unread))
        })
    }

    /** What one agent is doing: "Permission: Bash: go test" / "Plan waiting for your approval" / the question … */
    fun line(item: FleetItem): CoreText = when (item.kind) {
        "permission" -> if (item.detail.isEmpty()) CoreText.of(Key.FleetPermissionWaiting) else CoreText.of(Key.FleetPermissionDetail, item.detail)
        "plan" -> if (item.detail.isEmpty()) CoreText.of(Key.FleetPlanWaiting) else CoreText.of(Key.FleetPlanDetail, item.detail)
        "question" -> if (item.detail.isEmpty()) CoreText.of(Key.FleetQuestionWaiting) else CoreText.plain(item.detail)
        "working" -> CoreText.of(Key.FleetItemWorking)
        "unread" -> if (item.detail.isEmpty()) CoreText.of(Key.FleetItemDone) else CoreText.plain(item.detail)
        else -> if (item.detail.isEmpty()) CoreText.of(Key.FleetScreenWaiting) else CoreText.plain(item.detail)
    }

    /** "Name · what it is doing" */
    fun row(item: FleetItem): CoreText =
        if (item.title.isEmpty()) line(item) else CoreText.joined(" · ", listOf(CoreText.plain(item.title), line(item)))

    /** "Name · what it is doing (machine · space › tab)" */
    fun rowWithPlace(item: FleetItem): CoreText =
        if (item.place.isEmpty()) row(item) else CoreText.of(Key.Parenthesized, row(item), item.place)

    /** What a locked screen says instead of the agent's name and content. */
    fun anonymous(item: FleetItem): CoreText = when (item.kind) {
        "working" -> CoreText.of(Key.FleetItemWorking)
        "unread" -> CoreText.of(Key.FleetItemDone)
        else -> anonymousCount(item.kind, 1)
    }

    /**
     * Requests without names or content, for a notification that may show on the lock screen ("Show details on lock screen" off):
     * "A permission is waiting for you", "2 plans waiting for your approval" …, one line per kind in the order they came.
     */
    fun anonymousRequests(items: List<FleetItem>): List<CoreText> =
        items.filter(FleetSummary::needsYou).groupBy { requestKind(it.kind) }.map { (kind, group) -> anonymousCount(kind, group.size) }

    /** "A permission is waiting for you" for one, "3 permissions waiting for you" for more. */
    fun anonymousCount(kind: String, count: Int): CoreText = when (requestKind(kind)) {
        "permission" -> if (count == 1) CoreText.of(Key.FleetOnePermission) else CoreText.of(Key.FleetPermissions, count)
        "plan" -> if (count == 1) CoreText.of(Key.FleetOnePlan) else CoreText.of(Key.FleetPlans, count)
        "question" -> if (count == 1) CoreText.of(Key.FleetOneQuestion) else CoreText.of(Key.FleetQuestions, count)
        else -> if (count == 1) CoreText.of(Key.FleetOneScreen) else CoreText.of(Key.FleetScreens, count)
    }

    /** A kind this client does not know waits on a screen (iOS decodes it as screen). */
    private fun requestKind(kind: String) = if (kind == "permission" || kind == "plan" || kind == "question") kind else "screen"

    fun more(count: Int): CoreText = CoreText.of(Key.FleetMore, count)
}
