package jarvis.core.designsystem

import jarvis.core.contracts.api.ConnectionState
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow

class FeedbackGate(
    private val connection: () -> ConnectionState,
    private val clock: () -> Long = { System.nanoTime() / 1_000_000 },
) : AppFeedback {
    private val queue = MutableSharedFlow<String>(extraBufferCapacity = 1, onBufferOverflow = BufferOverflow.DROP_OLDEST)
    val messages = queue.asSharedFlow()
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
        queue.tryEmit(message)
        return true
    }
}
