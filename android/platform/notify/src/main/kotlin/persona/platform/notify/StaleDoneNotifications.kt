package persona.platform.notify

import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.collectLatest
import persona.core.contracts.AgentState
import persona.core.contracts.Session

/**
 * Takes down "Finished" notifications left by an earlier process (the app force-stopped or killed by the system) whose
 * session has been seen since (#4088). [SeenSessions] only notices unread turning idle, so after a restart a session
 * already idle in the first list kept its old notification. Only lists verified against the current connection count,
 * never a restored cache: while online it looks every [settle] for [window] (verification can land late, and it does
 * not always change the sessions list), then stops. A session not listed, or still unread, keeps its notification;
 * one still unread is [SeenSessions]' once it turns idle, as are the notifications this process posts.
 */
internal class StaleDoneNotifications(
    /** Tags of the "Finished" notifications showing now, with the wall-clock time each was posted. */
    private val showing: () -> Map<String, Long>,
    /** The sessions in lists verified against the current connection. */
    private val verified: () -> List<Session>,
    /** Cancels the tag's notification if it was still posted before the given time. */
    private val retract: (tag: String, before: Long) -> Unit,
    /** Wall-clock start of this process: notifications posted before it are the old ones. */
    private val since: Long,
    private val settle: Long = InteractionNotificationReconciler.SETTLE_MILLIS,
    private val window: Long = WINDOW_MILLIS,
) {
    suspend fun run(online: Flow<Boolean>) {
        var left = window
        online.collectLatest { up ->
            while (up && left > 0) {
                delay(settle)
                left -= settle
                val old = showing().filterValues { it < since }.keys
                if (old.isEmpty()) { left = 0; return@collectLatest }
                stale(old, verified()).forEach { retract(it, since) }
            }
        }
    }

    companion object {
        const val WINDOW_MILLIS = 30_000L
        const val DONE_PREFIX = "done:"

        fun stale(old: Set<String>, verified: List<Session>): Set<String> {
            val seen = verified.filter { it.agentState == AgentState.Idle }.mapTo(HashSet()) { "$DONE_PREFIX${it.key.path}" }
            return old.filterTo(HashSet()) { it in seen }
        }
    }
}
