package jarvis.feature.agents

import androidx.navigation3.runtime.NavKey
import jarvis.core.contracts.SessionKey
import jarvis.core.navigation.ChatRoute
import jarvis.core.navigation.InteractionRoute
import jarvis.core.navigation.TerminalRoute

/** Route metadata does not change the identity of the selected session. */
internal fun isSessionSelected(detail: NavKey?, session: SessionKey): Boolean = when (detail) {
    is ChatRoute -> detail.machineId == session.machineId && detail.sessionId == session.sessionId
    is InteractionRoute -> isSessionSelected(detail.chat, session)
    is TerminalRoute -> detail.machineId == session.machineId && detail.sessionId == session.sessionId
    else -> false
}
