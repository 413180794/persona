package jarvis.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.Serializable

@Serializable data object MachinesRoute : NavKey
@Serializable data class MachineDetailRoute(val machineId: String) : NavKey
