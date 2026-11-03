package persona.feature.agents

import androidx.navigation3.runtime.NavKey
import persona.core.contracts.SessionKey
import persona.core.navigation.ChatRoute
import persona.core.navigation.InteractionRoute
import persona.core.navigation.TerminalRoute

/** Route metadata does not change the identity of the selected session. */
internal fun isSessionSelected(detail: NavKey?, session: SessionKey): Boolean = when (detail) {
    is ChatRoute -> detail.machineId == session.machineId && detail.sessionId == session.sessionId
    is InteractionRoute -> isSessionSelected(detail.chat, session)
    is TerminalRoute -> detail.machineId == session.machineId && detail.sessionId == session.sessionId
    else -> false
}
