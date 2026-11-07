package persona.feature.assistant

import persona.core.contracts.AgentState
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.designsystem.PersonaState

/**
 * What Persona's face shows for the conversation on screen (iOS `AppModel.personaState`, docs/apple/persona.md "States"):
 * "Offline" when the hub or Persona's machine is out of reach (the conversation's, or [assistantMachine] on the
 * new-conversation page, where [session] is null), "Needs you" when the conversation waits on the user, "Working" while a
 * turn runs, else "Idle". iOS tells "Thinking" from "Working" by the chat's last item, which this page does not hold: a
 * running turn is "Working" here.
 */
internal fun personaState(session: Session?, hubOnline: Boolean, machines: List<Machine>, assistantMachine: String = ""): PersonaState = when {
    !hubOnline -> PersonaState.Offline
    machines.firstOrNull { it.id == (session?.machineId ?: assistantMachine) }?.online == false -> PersonaState.Offline
    session == null -> PersonaState.Idle
    session.agentState == AgentState.NeedsYou || session.pendingInteractions > 0 -> PersonaState.NeedsYou
    session.agentState == AgentState.Working -> PersonaState.Working
    else -> PersonaState.Idle
}
