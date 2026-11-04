package persona.core.designsystem

import persona.core.contracts.api.ConnectionState
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.map

class FeedbackGate(
    private val connection: () -> ConnectionState,
    private val clock: () -> Long = { System.nanoTime() / 1_000_000 },
) : AppFeedback {
    private val queue = MutableSharedFlow<FeedbackNotice>(extraBufferCapacity = 1, onBufferOverflow = BufferOverflow.DROP_OLDEST)
    /** What to show, with its button (撤销) when it has one. */
    val notices = queue.asSharedFlow()
    val messages = notices.map { it.message }
    private val lastOffline = mutableMapOf<String, Long>()
    private val lastNotices = mutableMapOf<String, Long>()

    @Synchronized
    override fun failure(error: Throwable, operation: Boolean): Boolean {
        if (error is CancellationException || !operation) return true
        val feedback = operationFeedback(error)
        if (connection() != ConnectionState.Online) {
            val now = clock()
            if (lastOffline[feedback.message]?.let { now - it < 4_000 } == true) return true
            lastOffline[feedback.message] = now
        }
        return notice(feedback.message)
    }

    @Synchronized
    override fun notice(message: String): Boolean {
        val now = clock()
        lastNotices.entries.removeAll { now - it.value >= 4_000 }
        if (lastNotices[message]?.let { now - it < 4_000 } == true) return true
        lastNotices[message] = now
        queue.tryEmit(FeedbackNotice(message))
        return true
    }

    /** Never folded into an earlier one: each carries its own 撤销. */
    @Synchronized
    override fun notice(message: String, action: FeedbackAction): Boolean {
        queue.tryEmit(FeedbackNotice(message, action))
        return true
    }
}
