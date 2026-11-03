package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable @SerialName("jarvis.core.navigation.MachinesRoute") data object MachinesRoute : NavKey
@Serializable @SerialName("jarvis.core.navigation.MachineDetailRoute") data class MachineDetailRoute(val machineId: String) : NavKey
