package persona.feature.agents

import android.os.SystemClock
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import persona.core.navigation.RequestRef

/**
 * Asks the Agents page to open one request card: a notification about a card outside any session (`personad ask` in a
 * plain terminal, #4808) has no conversation to land in, so its tap opens the card's sheet on Agents. The app raises a
 * request when it applies such a link; [AgentsScreen] takes it once the card is in its list. One nobody takes in time
 * (the card was answered elsewhere, the list never loaded) lapses, so the sheet cannot pop up long after the tap.
 */
class CardOpenRequests(private val clock: () -> Long = SystemClock::elapsedRealtime) {
    data class Request(val card: RequestRef, val at: Long)

    private val requested = MutableStateFlow<Request?>(null)

    /** The pending request; null when there is none. */
    val requests: StateFlow<Request?> = requested.asStateFlow()

    fun request(card: RequestRef) { requested.value = Request(card, clock()) }

    /** Takes [request]: true when it is still the pending one and fresh enough to act on. */
    fun take(request: Request): Boolean = requested.compareAndSet(request, null) && clock() - request.at <= MAX_AGE_MILLIS

    private companion object { const val MAX_AGE_MILLIS = 15_000L }
}

/** The process-wide requests the app raises and the Agents page takes. */
val AgentsCardOpen = CardOpenRequests()
