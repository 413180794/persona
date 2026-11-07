package persona.core.navigation

import android.os.SystemClock
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Asks the to-do list to open "New to-do" (the launcher's shortcut, [NewTodoLink]). [restore] raises a request each time
 * the link is applied to the shell's stacks, also when [NewTodoRoute] is still on the stack and its page keeps its state
 * (#5718); that page takes it once. The lapse only guards against a list that never shows: a request nobody takes in
 * time is not acted on later.
 */
class NewTodoRequests(private val clock: () -> Long = SystemClock::elapsedRealtime) {
    private val requested = MutableStateFlow<Long?>(null)

    /** When the pending request was made (the clock's milliseconds); null when there is none. */
    val requests: StateFlow<Long?> = requested.asStateFlow()

    fun request() { requested.value = clock() }

    /** Takes the request made at [at]: true when it is still the pending one and fresh enough to act on. */
    fun take(at: Long): Boolean = requested.compareAndSet(at, null) && clock() - at <= MAX_AGE_MILLIS

    private companion object { const val MAX_AGE_MILLIS = 15_000L }
}

/** The process-wide requests the app's links raise and the to-do list takes. */
val NewTodoOpen = NewTodoRequests()
