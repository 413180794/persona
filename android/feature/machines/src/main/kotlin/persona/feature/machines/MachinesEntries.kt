package persona.feature.machines

import androidx.compose.foundation.lazy.LazyListScope
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
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf

/**
 * Supply the same hub-scoped repositories used by Agents; replacing a pairing replaces this identity. Without
 * [management] (体验模式) the pages only watch: no launchers, 重新连接, 重新部署 or engine choice.
 */
fun EntryProviderScope<NavKey>.machinesEntries(
    navigator: Navigator,
    metrics: MachineMetricsApi,
    sessions: SessionsRepository,
    management: MachineManagement? = null,
    /** Whether a launcher's row opens its page (the hub manages plugins, which may draw a section there). */
    launcherPages: Flow<Boolean> = flowOf(false),
    /**
     * The sections plugins draw on a machine's page (`machine_section`, docs/plugins.md「UI 插槽」), as the page's
     * list items after the machine's own; built by the settings module, which draws plugins' views.
     */
    pluginSections: (@Composable (String) -> (LazyListScope.() -> Unit))? = null,
) {
    entry<MachinesRoute> { MachinesRouteContent(null, navigator, metrics, sessions, management, launcherPages, null) }
    entry<MachineDetailRoute> { MachinesRouteContent(it.machineId, navigator, metrics, sessions, management, launcherPages, pluginSections) }
}

@Composable
private fun MachinesRouteContent(
    id: String?, navigator: Navigator, api: MachineMetricsApi, sessions: SessionsRepository, management: MachineManagement?,
    launcherPages: Flow<Boolean>, pluginSections: (@Composable (String) -> (LazyListScope.() -> Unit))?,
) {
    val launchers by launcherPages.collectAsState(false)
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
        val plugins = if (id != null && pluginSections != null) pluginSections(id) else null
        MachinesScreen(id, model, navigator, actions, launcherPages = launchers, pluginSections = plugins)
    }
}
