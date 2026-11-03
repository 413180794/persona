package persona.feature.assistant

import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.designsystem.PersonaState
import org.junit.Assert.assertEquals
import org.junit.Test

class AssistantPersonaTest {
    private val online = listOf(Machine("m", online = true))
    private fun session(status: String = "idle", state: String? = null, pending: Int = 0) =
        Session(machineId = "m", sessionId = "s", status = status, state = state, pendingInteractions = pending)

    @Test
    fun faceFollowsTheConversation() {
        assertEquals(PersonaState.Idle, personaState(null, hubOnline = true, online))
        assertEquals(PersonaState.Idle, personaState(session(), hubOnline = true, online))
        assertEquals(PersonaState.Working, personaState(session("working", "working"), hubOnline = true, online))
        assertEquals(PersonaState.NeedsYou, personaState(session("blocked", "needs_you"), hubOnline = true, online))
        // A question waiting on the user wins over a running turn.
        assertEquals(PersonaState.NeedsYou, personaState(session("working", "working", pending = 1), hubOnline = true, online))
        assertEquals(PersonaState.Idle, personaState(session("done", "unread"), hubOnline = true, online))
    }

    @Test
    fun outOfReachIsOffline() {
        assertEquals(PersonaState.Offline, personaState(session("working", "working"), hubOnline = false, online))
        assertEquals(PersonaState.Offline, personaState(null, hubOnline = false, online))
        assertEquals(PersonaState.Offline, personaState(session("blocked", "needs_you"), hubOnline = true, listOf(Machine("m", online = false))))
        // The new-conversation page has no conversation: Persona's own machine decides.
        assertEquals(PersonaState.Offline, personaState(null, hubOnline = true, listOf(Machine("m", online = false)), assistantMachine = "m"))
        assertEquals(PersonaState.Idle, personaState(null, hubOnline = true, online, assistantMachine = "m"))
        // A machine the phone has not heard of yet does not make Persona look offline.
        assertEquals(PersonaState.Working, personaState(session("working", "working"), hubOnline = true, emptyList()))
    }
}
