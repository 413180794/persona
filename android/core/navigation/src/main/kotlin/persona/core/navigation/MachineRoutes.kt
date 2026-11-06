package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable @SerialName("jarvis.core.navigation.MachinesRoute") data object MachinesRoute : NavKey
/**
 * Machines › "Add computer" (docs/onboarding.md 4.2); with [confirm], "Confirm computer" for that computer ("" for the first one waiting).
 */
@Serializable @SerialName("persona.core.navigation.AddComputerRoute") data class AddComputerRoute(val confirm: String? = null) : NavKey
@Serializable @SerialName("jarvis.core.navigation.MachineDetailRoute") data class MachineDetailRoute(val machineId: String) : NavKey

/** Which terminal engine a machine uses (docs/engines.md), opened from its page. */
@Serializable @SerialName("jarvis.core.navigation.EngineRoute") data class EngineRoute(val machineId: String) : NavKey
