package persona.platform.capsule

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import persona.core.designsystem.CapsuleTokens

/** The host's main-thread completion presentation; it never changes the manual card or its selection. */
class CapsuleCompletionQueue(
    private val scope: CoroutineScope,
    private val displayMillis: Long = CapsuleTokens.completionMillis,
) : AutoCloseable {
    private val mutableCurrent = MutableStateFlow<CapsuleCompletion?>(null)
    val current = mutableCurrent.asStateFlow()
    private val pending = ArrayDeque<CapsuleCompletion>()
    private val seen = mutableSetOf<String>()
    private var enabled = false
    private var manual = false
    private var closed = false
    private var generation = 0L
    private var timer: Job? = null

    fun setVisible(value: Boolean) {
        if (closed) return
        enabled = value
        if (!value) clear()
    }

    fun setManual(value: Boolean) {
        if (closed) return
        manual = value
        if (value) clear()
    }

    fun offer(completion: CapsuleCompletion) {
        if (closed || !seen.add(completion.id) || !enabled || manual) return
        pending.addLast(completion)
        if (mutableCurrent.value == null) advance()
    }

    /** A stale click cannot open the next completion's target. Clear the queue before handing off navigation. */
    fun take(id: String): CapsuleCompletion? {
        val completion = mutableCurrent.value?.takeIf { it.id == id } ?: return null
        clear()
        return completion
    }

    fun clear() {
        generation++
        timer?.cancel()
        timer = null
        pending.clear()
        mutableCurrent.value = null
    }

    private fun advance() {
        val next = pending.removeFirstOrNull()
        mutableCurrent.value = next
        if (next == null) { timer = null; return }
        val ticket = ++generation
        timer = scope.launch {
            delay(displayMillis)
            if (!closed && ticket == generation && enabled && !manual) advance()
        }
    }

    override fun close() {
        closed = true
        enabled = false
        clear()
    }
}
