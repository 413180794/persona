package persona.core.data

import persona.core.contracts.AgentState
import persona.core.contracts.Interaction
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.SessionsRepository
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest

/** A card answered here for an agent, with the hub's `state_since` for that agent when it was answered. */
class AnsweredMark(val stateSince: String?)

/**
 * [sessions] as the pages show them (#4525, as the web's answered-lookup.ts for #4186). Answering a card takes it out of
 * the pending list at once, but the agent's own state says needs_you until the node's next sessions push. So an agent
 * that still has the state the answer found (same `state_since`) and no other card waiting is shown at work. A new card,
 * or a needs_you that started after the answer (a TUI dialog), makes it wait again at once. Returns the same list when
 * nothing changes.
 */
fun answeredSessions(sessions: List<Session>, pending: List<Interaction>, answered: Map<SessionKey, AnsweredMark>): List<Session> {
    if (answered.isEmpty()) return sessions
    val waiting = pending.mapTo(HashSet()) { it.sessionKey }
    var changed = false
    val shown = sessions.map { session ->
        val mark = answered[session.key]
        if (mark == null || session.agentState != AgentState.NeedsYou || session.key in waiting || session.stateSince != mark.stateSince) session
        else {
            changed = true
            // Only a host `blocked` becomes `working` on purpose: that is what the chat's status line and composer read
            // for "waiting on you". Any other host status (idle, done…) is the host's own word and stays as it is.
            session.copy(state = AgentState.Working.wire, pendingInteractions = 0,
                status = if (session.status == "blocked") "working" else session.status)
        }
    }
    return if (changed) shown else sessions
}

/** The hub's [sessions] with [interactions]' answers applied; the hub's own flow while none is recent. */
@OptIn(ExperimentalCoroutinesApi::class)
fun answeredSessions(sessions: Flow<List<Session>>, interactions: HubInteractionCenter): Flow<List<Session>> =
    interactions.answered.flatMapLatest { marks ->
        if (marks.isEmpty()) sessions
        else combine(sessions, interactions.pending) { list, pending -> answeredSessions(list, pending, marks) }
    }

/** This repository with [shown] as its sessions (#4525); refreshing, adopting and machines are still its own. */
fun SessionsRepository.showing(shown: StateFlow<List<Session>>): SessionsRepository {
    val base = this
    return object : SessionsRepository by base {
        override val sessions = shown
    }
}
