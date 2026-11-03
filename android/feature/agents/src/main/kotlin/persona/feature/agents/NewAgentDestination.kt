package persona.feature.agents

import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dagger.hilt.android.EntryPointAccessors
import persona.core.contracts.api.ConnectionState
import persona.core.designsystem.JListPage
import persona.core.designsystem.JNotice
import persona.core.navigation.ChatRoute
import persona.core.navigation.ConversationKind
import persona.core.navigation.Navigator
import persona.core.navigation.NewAgentRoute

/**
 * The new agent page opened by key (`NewAgentRoute`, the knowledge base's 开 Agent): the form of Agents' 新建会话,
 * prefilled. Once the hub answers, the page closes and the new conversation opens in Agents.
 */
@Composable
internal fun NewAgentDestination(route: NewAgentRoute, navigator: Navigator, dependenciesOverride: AgentsDependencies?) {
    val context = LocalContext.current.applicationContext
    val dependencies = remember(context, dependenciesOverride) {
        dependenciesOverride ?: EntryPointAccessors.fromApplication(context, AgentsDependencies::class.java)
    }
    val wiring by dependencies.wiring().state.collectAsStateWithLifecycle()
    val service = wiring.creation ?: LocalAgentCreationService.current
    if (service == null) {
        JListPage(title = "新建会话", onBack = navigator::back) { item { JNotice("连上 hub 后才能新建会话。") } }
        return
    }
    val prefill = NewAgentPrefill(route.machineId, route.cwd, route.prompt)
    val model = viewModel(key = "new-agent-page:${service.scope}:${System.identityHashCode(service)}:${route.hashCode()}") {
        NewAgentViewModel(service, wiring.creationPreferences?.let { AgentCreationPreferences(context, it) } ?: AgentCreationPreferences(context),
            dependencies.sessions().machines, sender = dependencies.sender(), adopt = dependencies.sessions()::adopt, prefill = prefill)
    }
    val state by model.state.collectAsStateWithLifecycle()
    val machines by dependencies.sessions().machines.collectAsStateWithLifecycle()
    val connection by dependencies.connection().state.collectAsStateWithLifecycle()
    LaunchedEffect(state.created) {
        state.created?.let { session ->
            // Before leaving: popping this page clears the model's scope.
            model.acknowledge()
            navigator.back()
            // An agent, so it opens in Agents whichever tab this page was on.
            navigator.navigate(ChatRoute(session.machineId, session.sessionId, kind = ConversationKind.Agent))
        }
    }
    NewAgentPage(model, machines, connection == ConnectionState.Online, onBack = navigator::back)
}
