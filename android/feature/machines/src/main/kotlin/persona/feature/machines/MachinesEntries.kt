package persona.feature.machines

import androidx.compose.runtime.*
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.core.contracts.api.MachineMetricsApi
import persona.core.contracts.api.SessionsRepository
import persona.core.navigation.*

/** Supply the same hub-scoped repositories used by Agents; replacing a pairing replaces this identity. */
fun EntryProviderScope<NavKey>.machinesEntries(
    navigator: Navigator,
    metrics: MachineMetricsApi,
    sessions: SessionsRepository,
) {
    entry<MachinesRoute> { MachinesRouteContent(null, navigator, metrics, sessions) }
    entry<MachineDetailRoute> { MachinesRouteContent(it.machineId, navigator, metrics, sessions) }
}

@Composable
private fun MachinesRouteContent(id: String?, navigator: Navigator, api: MachineMetricsApi, sessions: SessionsRepository) {
    key(api, sessions) {
        val model = viewModel(key = "machines:${System.identityHashCode(api)}:${id.orEmpty()}") {
            MachinesViewModel(api, sessions, id)
        }
        val lifecycle = LocalLifecycleOwner.current.lifecycle
        LaunchedEffect(model, lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) { model.poll() } }
        MachinesScreen(id, model, navigator)
    }
}
