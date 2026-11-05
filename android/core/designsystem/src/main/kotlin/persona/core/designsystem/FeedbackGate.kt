package persona.core.designsystem

import persona.core.contracts.api.ConnectionState
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.map

/**
 * Folds repeated failure notices. [localize] turns an [OperationFeedback.text] into the app's language (the app
 * passes `{ it.resolve(context) }`); without it the notices are [OperationFeedback.message]'s Simplified Chinese.
 */
class FeedbackGate(
    private val connection: () -> ConnectionState,
    private val clock: () -> Long = { System.nanoTime() / 1_000_000 },
    private val localize: ((UiText) -> String)? = null,
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
        val message = localize?.invoke(feedback.text) ?: feedback.message
        if (connection() != ConnectionState.Online) {
            val now = clock()
            if (lastOffline[message]?.let { now - it < 4_000 } == true) return true
            lastOffline[message] = now
        }
        return notice(message)
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
