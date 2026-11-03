package persona.feature.assistant

import persona.core.contracts.AgentState
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.designsystem.PersonaState

/**
 * What Jarvis's face shows for the conversation on screen (iOS `AppModel.personaState`, docs/persona.md「状态」):
 * 离线 when the hub or Jarvis's machine is out of reach (the conversation's, or [assistantMachine] on the
 * new-conversation page, where [session] is null), 等你处理 when the conversation waits on the user, 工作中 while a
 * turn runs, else 空闲. iOS tells 思考中 from 工作中 by the chat's last item, which this page does not hold: a
 * running turn is 工作中 here.
 */
internal fun personaState(session: Session?, hubOnline: Boolean, machines: List<Machine>, assistantMachine: String = ""): PersonaState = when {
    !hubOnline -> PersonaState.Offline
    machines.firstOrNull { it.id == (session?.machineId ?: assistantMachine) }?.online == false -> PersonaState.Offline
    session == null -> PersonaState.Idle
    session.agentState == AgentState.NeedsYou || session.pendingInteractions > 0 -> PersonaState.NeedsYou
    session.agentState == AgentState.Working -> PersonaState.Working
    else -> PersonaState.Idle
}
