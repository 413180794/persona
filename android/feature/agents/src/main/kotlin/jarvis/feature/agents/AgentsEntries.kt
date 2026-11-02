package jarvis.feature.agents

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import jarvis.core.contracts.api.*
import jarvis.core.contracts.SessionKey
import jarvis.core.designsystem.PlaceholderScreen
import jarvis.core.navigation.AgentsRoute
import jarvis.core.navigation.ConnectionSettingsRoute
import jarvis.core.navigation.Navigator
import jarvis.core.domain.answer.requestSourceLabel
import kotlinx.coroutines.flow.flowOf

@EntryPoint
@InstallIn(SingletonComponent::class)
interface AgentsDependencies {
    fun sessions(): SessionsRepository
    fun interactions(): InteractionCenter
    fun connection(): HubConnection
    fun sender(): MessageSender
    fun preferences(): AgentsPreferences
    fun wiring(): AgentsWiring = AgentsWiring.Empty
    /** The phone's hub groups: the list labels and filters by them when there is more than one. */
    fun groups(): jarvis.core.contracts.api.HubGroups = jarvis.core.contracts.api.HubGroups.None
}

/** Supply one immutable pairing scope; close callbacks synchronize navigation and notifications. */
data class AgentsOperationsBindings(
    val operations: AgentOperations,
    val assistant: AssistantConversations,
    val onSessionClosed: (SessionKey) -> Unit = {},
)
val LocalAgentsOperations = staticCompositionLocalOf<AgentsOperationsBindings?> { null }

/** The composition root can supply shared draft and source metadata without importing app implementations. */
val LocalAgentsMetadata = staticCompositionLocalOf { AgentsMetadata() }

/** Supply an adapter to the active shared DraftStore from the app composition root. */
val LocalAgentsDraftSource = staticCompositionLocalOf<AgentsDraftSource?> { null }

/** App supplies a service bound to the active pairing and pinned HTTP route. */
val LocalAgentCreationService = staticCompositionLocalOf<AgentCreationService?> { null }

@Suppress("UNUSED_PARAMETER")
fun EntryProviderScope<NavKey>.agentsEntries(
    navigator: Navigator,
    dependenciesOverride: AgentsDependencies? = null,
    initialMachine: String = "",
    onMachineSelected: (String) -> Unit = {},
    // Retained for source compatibility; the migrated route owns its screen now.
    agentsContent: @Composable () -> Unit = {},
    connectionSettingsContent: @Composable () -> Unit = { PlaceholderScreen("连接设置") },
) {
    entry<AgentsRoute> {
        val context = LocalContext.current.applicationContext
        val dependencies = remember(context, dependenciesOverride) {
            dependenciesOverride ?: EntryPointAccessors.fromApplication(context, AgentsDependencies::class.java)
        }
        val wiring by dependencies.wiring().state.collectAsStateWithLifecycle()
        val renamer = remember(context) {
            if (context is dagger.hilt.internal.GeneratedComponentManager<*>)
                EntryPointAccessors.fromApplication(context, AgentsRenameDependencies::class.java).renamer()
            else null
        }
        androidx.compose.runtime.CompositionLocalProvider(
            LocalAgentsOperations provides (wiring.operations ?: LocalAgentsOperations.current),
            LocalAgentCreationService provides (wiring.creation ?: LocalAgentCreationService.current),
            LocalAgentsDraftSource provides (wiring.drafts ?: LocalAgentsDraftSource.current),
        ) {
            val operations = LocalAgentsOperations.current
            val model = viewModel(key = "agents:${wiring.scope}:${operations?.operations?.scope.orEmpty()}") {
                AgentsViewModel(dependencies.sessions(), dependencies.interactions(), dependencies.connection(), dependencies.sender(), dependencies.preferences(), operations, wiring.mainSender,
                    wiring.scope.ifEmpty { operations?.operations?.scope.orEmpty() }, renamer)
                    .also { it.machine(initialMachine) }
            }
            val source = LocalAgentsDraftSource.current
            val draftsFlow = remember(dependencies, source) {
                source?.let { observeAgentDrafts(dependencies.sessions().sessions, it) } ?: flowOf(emptyMap())
            }
            val drafts by draftsFlow.collectAsStateWithLifecycle(emptyMap())
            val outbox by dependencies.sender().outbox.collectAsStateWithLifecycle()
            val requests by dependencies.interactions().pending.collectAsStateWithLifecycle()
            val suppliedMetadata = LocalAgentsMetadata.current
            val metadata = suppliedMetadata.withOutbox(outbox).copy(requestSources = suppliedMetadata.requestSources +
                requests.mapNotNull { request -> request.requestSourceLabel()?.let { request.id to it } }.toMap())
            val rows = metadata.rows.toMutableMap()
            drafts.forEach { (key, draft) ->
                val row = rows[key] ?: AgentRowMetadata()
                rows[key] = row.copy(draft = draft.text, hasAttachments = row.hasAttachments || draft.hasAttachments)
            }
            val creationService = LocalAgentCreationService.current
            val creationModel = creationService?.let { service ->
                viewModel(key = "new-agent:${service.scope}:${System.identityHashCode(service)}") {
                    NewAgentViewModel(service, wiring.creationPreferences?.let { AgentCreationPreferences(context, it) }
                        ?: AgentCreationPreferences(context), dependencies.sessions().machines)
                }
            }
            val groups by dependencies.groups().groups.collectAsStateWithLifecycle()
            AgentsScreen(model, navigator, metadata.copy(rows = rows), creationModel, onMachineSelected, groups)
        }
    }
    entry<ConnectionSettingsRoute> { connectionSettingsContent() }
}
