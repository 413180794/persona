package persona.platform.notify

import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime

/**
 * Finds the assistant notifications whose conversation has been looked at since they were posted (#4805). An assistant
 * conversation is not always unread when it pushes (notify_user mid-turn, a task notice), so [SeenSessions]' unread →
 * idle misses it; every app's `seen` (the web, iOS, a Mac) moves the session's `seen_at` instead. A notification posted
 * before that time is stale, also one left by an earlier process: the first list counts like any other.
 */
internal class SeenAssistantNotifications {
    private val last = mutableMapOf<SessionKey, Long>()

    /**
     * Tags of the assistant conversations whose `seen_at` moved since the previous list (all of them in the first),
     * each with the time before which its notification is stale. The hub's clock is not the phone's, so [now] caps it:
     * a notification posted after this list arrived is never taken for seen.
     */
    fun update(sessions: List<Session>, now: Long): Map<String, Long> = buildMap {
        sessions.forEach { session ->
            if (!session.assistant) return@forEach
            val at = parseTime(session.seenAt)?.toEpochMilli() ?: return@forEach
            if (last.put(session.key, at) != at) put("$PREFIX${session.key.path}", minOf(at, now))
        }
    }

    companion object {
        const val PREFIX = "assistant:"

        /** Of the [showing] assistant notifications (tag → post time), those posted before their [seen] cut-off. */
        fun stale(showing: Map<String, Long>, seen: Map<String, Long>): Map<String, Long> =
            seen.filter { (tag, before) -> showing[tag]?.let { it < before } == true }
    }
}
