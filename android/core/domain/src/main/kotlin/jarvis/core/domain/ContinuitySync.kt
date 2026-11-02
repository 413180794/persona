package jarvis.core.domain

import jarvis.core.contracts.Item
import jarvis.core.domain.ContinuityReducer.Gap

/**
 * Pure request decisions corresponding to iOS AppModel.settle/fill and APIClient.foldedWork.
 * The host serializes calls, executes actions, and reports completion or cancellation. No clocks,
 * coroutines or transport live here. One instance belongs to one session and one active fold fetch.
 */
class ContinuitySync(val reducer: ContinuityReducer) {
    sealed interface Action {
        /** Configured settling delay, not a measured latency. */
        data class ScheduleFill(val delayMillis: Long = 1_500) : Action
        data class FetchAfter(val afterSeq: Int) : Action
        data class FetchLatest(val foldWork: Boolean = true) : Action
        data class Subscribe(val sinceSeq: Int?, val foldWork: Boolean = true) : Action
        data class FetchOlder(val beforeSeq: Int, val foldWork: Boolean = true) : Action
        data class FetchRange(val fromSeq: Int, val throughSeq: Int) : Action
    }

    data class Result(val gap: Gap, val actions: List<Action>)

    private var filling = false
    private var fillPages = 0
    private var loadingOlder = false
    private var opening: Item? = null
    private var rangePages = 0
    private val work = mutableListOf<Item>()

    fun settle(gap: Gap): List<Action> = when (gap) {
        Gap.None -> emptyList()
        Gap.Fill -> listOf(Action.ScheduleFill())
        Gap.Reload -> listOf(Action.FetchLatest())
    }

    /** Reads current coverage when the timer fires, even if a backlog already closed the gap. */
    fun fillTimer(): List<Action> {
        if (filling) return emptyList()
        filling = true
        fillPages = 0
        return listOf(Action.FetchAfter(reducer.covered))
    }

    /** Catch-up pages never supply older-history pagination information to the reducer. */
    fun after(items: List<Item>, history: String?, fromSeq: Int?, toSeq: Int?, moreAfter: Boolean = false): Result {
        val gap = reducer.apply(items, history, fromSeq, toSeq, moreAfter = moreAfter)
        fillPages++
        val actions = when {
            gap == Gap.Reload || gap == Gap.Fill && fillPages >= 8 -> listOf(Action.FetchLatest())
            gap == Gap.Fill -> listOf(Action.FetchAfter(reducer.covered))
            else -> {
                finishFill()
                emptyList()
            }
        }
        return Result(gap, actions)
    }

    /** Also call on failed or cancelled catch-up/reload requests to release the in-flight gate. */
    fun finishFill() {
        filling = false
        fillPages = 0
    }

    fun reload(items: List<Item>, history: String?, toSeq: Int?, hasMore: Boolean) {
        reducer.replace(items, history, toSeq, hasMore)
        finishFill()
    }

    fun reconnect(): List<Action> = listOf(Action.Subscribe(reducer.covered.takeIf { it > 0 }))

    fun loadOlder(): List<Action> {
        if (loadingOlder || reducer.hasMore != true) return emptyList()
        val first = reducer.firstSeq ?: return emptyList()
        loadingOlder = true
        return listOf(Action.FetchOlder(first))
    }

    fun older(items: List<Item>, hasMore: Boolean) {
        reducer.older(items, hasMore)
        finishOlder()
    }

    /** Also call when an older-page request fails or is cancelled. */
    fun finishOlder() { loadingOlder = false }

    fun openFold(id: String): List<Action> {
        if (opening != null) return emptyList()
        val item = reducer.items.firstOrNull { it.id == id && it.kind == "fold" } ?: return emptyList()
        val fold = item.fold ?: return emptyList()
        opening = item
        rangePages = 0
        work.clear()
        return listOf(Action.FetchRange(item.seq, fold.throughSeq))
    }

    fun range(items: List<Item>, more: Boolean = false): List<Action> {
        val item = opening ?: error("Range response without an open fold")
        work.addAll(items)
        rangePages++
        val last = items.lastOrNull()
        if (more && last != null && rangePages < 100) {
            return listOf(Action.FetchRange(last.seq + 1, requireNotNull(item.fold).throughSeq))
        }
        reducer.unfold(item, work)
        cancelFold()
        return emptyList()
    }

    /** Discards unpublished range pages on failure or cancellation. */
    fun cancelFold() {
        opening = null
        rangePages = 0
        work.clear()
    }
}
