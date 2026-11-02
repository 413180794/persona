package jarvis.android.ui.settings

import androidx.compose.runtime.Composable
import jarvis.android.data.Hub
import jarvis.android.data.SessionKey
import jarvis.android.ui.machines.EngineScreen
import jarvis.android.ui.machines.MachineDetailScreen
import jarvis.android.ui.machines.MachineMonitorScreen
import jarvis.android.ui.machines.MachinesScreen

/**
 * The pages pushed over 设置, as plain strings so the stack survives in a saved state: "machines", "machine/<id>",
 * "monitor/<id>", "engine/<id>", "claude", "devices", "notifications", "storage", "style".
 */
sealed interface SettingsRoute {
    val path: String

    data object Machines : SettingsRoute { override val path = "machines" }
    data class Machine(val id: String) : SettingsRoute { override val path = "machine/$id" }
    data class Monitor(val id: String) : SettingsRoute { override val path = "monitor/$id" }
    data class Engine(val id: String) : SettingsRoute { override val path = "engine/$id" }
    data object Claude : SettingsRoute { override val path = "claude" }
    data object Models : SettingsRoute { override val path = "model-config" }
    data class ModelPurpose(val id: String) : SettingsRoute { override val path = "model-purpose/$id" }
    data object Devices : SettingsRoute { override val path = "devices" }
    data object Notifications : SettingsRoute { override val path = "notifications" }
    data object Storage : SettingsRoute { override val path = "storage" }
    data object Style : SettingsRoute { override val path = "style" }

    companion object {
        fun of(path: String): SettingsRoute? {
            val (head, rest) = path.substringBefore('/') to path.substringAfter('/', "")
            return when (head) {
                "machines" -> Machines
                "machine" -> rest.takeIf { it.isNotEmpty() }?.let(::Machine)
                "monitor" -> rest.takeIf { it.isNotEmpty() }?.let(::Monitor)
                "engine" -> rest.takeIf { it.isNotEmpty() }?.let(::Engine)
                "claude" -> Claude
                "model-config" -> Models
                "model-purpose" -> rest.takeIf { it.isNotEmpty() }?.let(::ModelPurpose)
                "devices" -> Devices
                "notifications" -> Notifications
                "storage" -> Storage
                "style" -> Style
                else -> null
            }
        }
    }
}

/** Draws the top page of the 设置 stack; [push] / [pop] move through it, [onOpenSession] leaves 设置 for a chat. */
@Composable
fun SettingsPage(
    hub: Hub,
    route: SettingsRoute,
    push: (SettingsRoute) -> Unit,
    pop: () -> Unit,
    onOpenSession: (SessionKey) -> Unit,
) {
    when (route) {
        SettingsRoute.Machines -> MachinesScreen(hub, pop, onOpenMachine = { push(SettingsRoute.Machine(it)) })
        is SettingsRoute.Machine -> MachineDetailScreen(
            hub, route.id, pop, onOpenSession = onOpenSession, onOpenMonitor = { push(SettingsRoute.Monitor(route.id)) },
            onOpenEngine = { push(SettingsRoute.Engine(route.id)) },
        )
        is SettingsRoute.Monitor -> MachineMonitorScreen(hub, route.id, pop)
        is SettingsRoute.Engine -> EngineScreen(hub, route.id, onDone = pop)
        SettingsRoute.Claude -> ClaudeSettingsScreen(hub, pop)
        SettingsRoute.Models -> ModelConfigurationScreen(hub, pop, onPurpose = { push(SettingsRoute.ModelPurpose(it)) })
        is SettingsRoute.ModelPurpose -> ModelPurposeScreen(hub, route.id, pop)
        SettingsRoute.Devices -> DevicesScreen(hub, pop)
        SettingsRoute.Notifications -> NotificationSettingsScreen(hub, pop)
        SettingsRoute.Storage -> StorageScreen(pop)
        SettingsRoute.Style -> jarvis.android.ui.StylePickerScreen(onBack = pop)
    }
}
