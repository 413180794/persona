package persona.platform.notify

import persona.core.contracts.AgentState
import persona.core.contracts.Session
import persona.core.contracts.SessionKey

/**
 * Finds the sessions whose result has been seen somewhere (the app, a computer, the web, "Mark as seen"): the hub moves them
 * from unread to idle. Their "Finished" / assistant notifications are then stale (#3567). A session missing from a list is
 * neither seen nor forgotten: the list can lack it for a moment while reconnecting or regrouping, and it may come back
 * idle because it was seen meanwhile. Closing a session retracts its own notifications.
 */
internal class SeenSessions {
    private val unread = mutableSetOf<SessionKey>()

    /** The sessions seen since the previous list; the first list only records what is unread. */
    fun update(sessions: List<Session>): List<SessionKey> {
        val seen = sessions.filter { it.key in unread && it.agentState == AgentState.Idle }.map { it.key }
        sessions.forEach { if (it.agentState == AgentState.Unread) unread += it.key else unread -= it.key }
        return seen
    }
}
