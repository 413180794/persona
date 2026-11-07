package persona.feature.chat

import persona.core.contracts.CoreText
import persona.core.contracts.Interaction
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.Todo
import persona.core.domain.answer.askLine
import persona.core.domain.answer.waitingCard

/** The "Today" card names at most this many waiting tasks under its "Needs you" row and counts the rest (the web's TODAY_WAITING_MAX). */
internal const val TODAY_WAITING_MAX = 3

/** A task the "Today" card says waits on you: its title, and what its agent's card asks when the store has that card. */
internal data class TodayWaitingLine(val title: String, val ask: CoreText?)

/** The lines under the "Today" card's "Needs you" row (#4752; the web's todayWaitingLines, #4478) and how many tasks they leave out. */
internal data class TodayWaiting(val lines: List<TodayWaitingLine>, val more: Int)

/**
 * What each task in [waiting] (id to title, as the hub's `today` body lists them) asks: its [waitingCard]'s [askLine],
 * from its [lead] agent's card among [cards] (a card just answered here is no longer among them). One task: its ask
 * alone, since the row's title already names it; nothing when it has no card. Several: up to [TODAY_WAITING_MAX] of
 * them, each with its title and ask (the title alone without a card), and how many follow.
 */
internal fun todayWaiting(
    waiting: List<Pair<String, String>>,
    todos: List<Todo>,
    lead: (Todo) -> Session?,
    cards: Map<SessionKey, Interaction>,
): TodayWaiting {
    fun askOf(id: String): CoreText? = todos.firstOrNull { it.id == id }?.let { it.waitingCard(lead(it), cards)?.askLine() }
    if (waiting.size == 1) {
        val (id, title) = waiting[0]
        val ask = askOf(id) ?: return TodayWaiting(emptyList(), 0)
        return TodayWaiting(listOf(TodayWaitingLine(title, ask)), 0)
    }
    val shown = waiting.take(TODAY_WAITING_MAX).map { (id, title) -> TodayWaitingLine(title, askOf(id)) }
    return TodayWaiting(shown, waiting.size - shown.size)
}
