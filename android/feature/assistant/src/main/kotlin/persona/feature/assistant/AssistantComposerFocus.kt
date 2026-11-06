package persona.feature.assistant

import android.os.SystemClock
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Asks the Persona page to put the cursor in its composer and bring up the keyboard (the launcher's "Ask Persona",
 * `persona://open?target=tab&tab=persona&focus=composer`). The app raises a request when it opens the Persona tab for that
 * link; [AssistantScreen] takes it once. A request nobody takes in time (not paired yet, the tab never shown) lapses,
 * so the keyboard cannot pop up long after the tap.
 */
class ComposerFocusRequests(private val clock: () -> Long = SystemClock::elapsedRealtime) {
    private val requested = MutableStateFlow<Long?>(null)

    /** When the pending request was made (the clock's milliseconds); null when there is none. */
    val requests: StateFlow<Long?> = requested.asStateFlow()

    fun request() { requested.value = clock() }

    /** Takes the request made at [at]: true when it is still the pending one and fresh enough to act on. */
    fun take(at: Long): Boolean = requested.compareAndSet(at, null) && clock() - at <= MAX_AGE_MILLIS

    private companion object { const val MAX_AGE_MILLIS = 15_000L }
}

/** The process-wide requests the app raises and the Persona page takes. */
val AssistantComposerFocus = ComposerFocusRequests()
