package persona.feature.machines

import androidx.compose.runtime.*
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.core.contracts.api.MachineManagement
import persona.core.contracts.api.MachineMetricsApi
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.LocalAppFeedback
import persona.core.navigation.*

/**
 * Supply the same hub-scoped repositories used by Agents; replacing a pairing replaces this identity. Without
 * [management] (体验模式) the pages only watch: no launchers, 重新连接, 重新部署 or engine choice.
 */
fun EntryProviderScope<NavKey>.machinesEntries(
    navigator: Navigator,
    metrics: MachineMetricsApi,
    sessions: SessionsRepository,
    management: MachineManagement? = null,
) {
    entry<MachinesRoute> { MachinesRouteContent(null, navigator, metrics, sessions, management) }
    entry<MachineDetailRoute> { MachinesRouteContent(it.machineId, navigator, metrics, sessions, management) }
}

@Composable
private fun MachinesRouteContent(id: String?, navigator: Navigator, api: MachineMetricsApi, sessions: SessionsRepository, management: MachineManagement?) {
    key(api, sessions, management) {
        val model = viewModel(key = "machines:${System.identityHashCode(api)}:${id.orEmpty()}") {
            MachinesViewModel(api, sessions, id)
        }
        val actions = management?.let {
            viewModel(key = "machine-actions:${System.identityHashCode(it)}:${id.orEmpty()}") { MachineActionsViewModel(it, sessions) }
        }
        val lifecycle = LocalLifecycleOwner.current.lifecycle
        LaunchedEffect(model, lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) { model.poll() } }
        val feedback = LocalAppFeedback.current
        LaunchedEffect(actions, feedback) { actions?.notices?.collect { feedback?.notice(it) } }
        MachinesScreen(id, model, navigator, actions)
    }
}
