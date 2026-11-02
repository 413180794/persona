package jarvis.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.Serializable

@Serializable
data class TerminalRoute(val machineId: String, val sessionId: String) : NavKey
