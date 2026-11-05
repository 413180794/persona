package persona.android.capsule

import persona.core.contracts.AgentState
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.SessionsEvent

internal fun completionId(session: Session) =
    "${capsuleId(session.key)}:${session.updatedAt.orEmpty().length}:${session.updatedAt.orEmpty()}:${session.rev ?: ""}"

/** A fresh online working round stays armed through requests; versions merely deduplicate its completion. */
internal class CapsuleCompletionTracker {
    private val working = mutableMapOf<String, MutableSet<SessionKey>>()
    private val consumed = mutableSetOf<String>()

    fun reset(machineId: String) { working.remove(machineId) }

    fun update(event: SessionsEvent, muted: (Session) -> Boolean): List<Session> {
        val armed = working.getOrPut(event.machineId) { mutableSetOf() }
        armed.retainAll(event.sessions.map { it.key }.toSet())
        return event.sessions.filter { current ->
            when (current.agentState) {
                AgentState.Working -> { armed.add(current.key); false }
                AgentState.NeedsYou -> false
                AgentState.Unread -> armed.remove(current.key) && consumed.add(completionId(current)) &&
                    !current.interrupted && !current.quiet && !muted(current)
                else -> { armed.remove(current.key); false }
            }
        }
    }
}
