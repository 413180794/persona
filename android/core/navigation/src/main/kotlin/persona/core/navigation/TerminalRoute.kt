package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable @SerialName("jarvis.core.navigation.TerminalRoute")
data class TerminalRoute(val machineId: String, val sessionId: String) : NavKey
