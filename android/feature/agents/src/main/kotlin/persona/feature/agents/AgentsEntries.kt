package persona.feature.agents

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import persona.core.contracts.api.*
import persona.core.contracts.SessionKey
import persona.core.contracts.Todo
import persona.core.contracts.api.AgentStartRequest
import persona.core.contracts.api.AgentStartResult
import persona.core.designsystem.PlaceholderScreen
import persona.core.navigation.AgentsRoute
import persona.core.navigation.ChatRoute
import persona.core.navigation.ConnectionSettingsRoute
import persona.core.navigation.Navigator
import persona.core.navigation.NewAgentRoute
import persona.core.navigation.TodoDetailRoute
import persona.core.navigation.TodosRoute
import persona.core.navigation.NewTodoRoute
import persona.core.domain.answer.requestSourceLabel
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch

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
    fun groups(): persona.core.contracts.api.HubGroups = persona.core.contracts.api.HubGroups.None
    fun tasks(): persona.core.domain.tasks.TasksStore? = null
    fun hubUpdates(): persona.core.contracts.api.HubUpdates? = null
    fun machines(): MachineManagement?
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
    connectionSettingsContent: @Composable () -> Unit = { PlaceholderScreen(stringResource(R.string.agents_connection_settings)) },
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
            val tasksStore = dependencies.tasks() ?: wiring.tasks
            val model = viewModel(key = "agents:${wiring.scope}:${operations?.operations?.scope.orEmpty()}") {
                AgentsViewModel(dependencies.sessions(), dependencies.interactions(), dependencies.connection(), dependencies.sender(), dependencies.preferences(), operations, wiring.mainSender,
                    wiring.scope.ifEmpty { operations?.operations?.scope.orEmpty() }, renamer, tasksStore, hubUpdates = dependencies.hubUpdates(), machineManagement = dependencies.machines())
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
                requests.mapNotNull { request -> request.requestSourceLabel()?.let { request.id to it.shown } }.toMap())
            val rows = metadata.rows.toMutableMap()
            drafts.forEach { (key, draft) ->
                val row = rows[key] ?: AgentRowMetadata()
                rows[key] = row.copy(draft = draft.text, hasAttachments = row.hasAttachments || draft.hasAttachments)
            }
            val creationService = LocalAgentCreationService.current
            val creationModel = creationService?.let { service ->
                viewModel(key = "new-agent:${service.scope}:${System.identityHashCode(service)}") {
                    NewAgentViewModel(service, wiring.creationPreferences?.let { AgentCreationPreferences(context, it) }
                        ?: AgentCreationPreferences(context), dependencies.sessions().machines,
                        sender = dependencies.sender(), adopt = dependencies.sessions()::adopt)
                }
            }
            val groups by dependencies.groups().groups.collectAsStateWithLifecycle()
            AgentsScreen(model, navigator, metadata.copy(rows = rows), creationModel, onMachineSelected, groups)
        }
    }
    entry<ConnectionSettingsRoute> { connectionSettingsContent() }
    entry<NewAgentRoute> { NewAgentDestination(it, navigator, dependenciesOverride) }
    // 新建待办 from the launcher shortcut is the same page with its sheet open.
    val todosPage: @Composable (Boolean) -> Unit = { creating ->
        val context = LocalContext.current.applicationContext
        val dependencies = remember(context, dependenciesOverride) {
            dependenciesOverride ?: EntryPointAccessors.fromApplication(context, AgentsDependencies::class.java)
        }
        val wiring by dependencies.wiring().state.collectAsStateWithLifecycle()
        val tasksStore = dependencies.tasks() ?: wiring.tasks
        val sessions by dependencies.sessions().sessions.collectAsStateWithLifecycle()
        val todos by (tasksStore?.todos ?: flowOf(emptyList())).collectAsStateWithLifecycle(emptyList())
        TodosScreen(
            todos = todos,
            sessions = sessions,
            tasks = tasksStore,
            onBack = { navigator.back() },
            onOpenTodo = { todoId -> navigator.navigate(TodoDetailRoute(todoId)) },
            startCreating = creating,
        )
    }
    entry<TodosRoute> { todosPage(false) }
    entry<NewTodoRoute> { todosPage(true) }
    entry<TodoDetailRoute> { route ->
        val context = LocalContext.current.applicationContext
        val dependencies = remember(context, dependenciesOverride) {
            dependenciesOverride ?: EntryPointAccessors.fromApplication(context, AgentsDependencies::class.java)
        }
        val wiring by dependencies.wiring().state.collectAsStateWithLifecycle()
        val tasksStore = dependencies.tasks() ?: wiring.tasks
        val sessions by dependencies.sessions().sessions.collectAsStateWithLifecycle()
        val machines by dependencies.sessions().machines.collectAsStateWithLifecycle()
        val todos by (tasksStore?.todos ?: flowOf(emptyList())).collectAsStateWithLifecycle(emptyList())
        val creationService = wiring.creation ?: LocalAgentCreationService.current
        var handOffTodo by remember { mutableStateOf<Todo?>(null) }
        val scope = rememberCoroutineScope()
        TodoDetailScreen(
            todoId = route.todoId,
            todos = todos,
            machines = machines,
            sessions = sessions,
            tasks = tasksStore,
            onBack = { navigator.back() },
            onOpenSession = { key -> navigator.navigate(ChatRoute(key.machineId, key.sessionId)) },
            onHandOff = { todo -> handOffTodo = todo }
        )
        handOffTodo?.let { targetTodo ->
            val todoNameHint = stringResource(R.string.agents_row_todo, targetTodo.title)
            TodoHandOffSheet(
                todo = targetTodo,
                machines = machines,
                sessions = sessions,
                onDismiss = { handOffTodo = null },
                onHandOffNew = { machineId, cwd, launcher, permissionMode, prompt ->
                    scope.launch {
                        try {
                            val opId = java.util.UUID.randomUUID().toString()
                            val res = creationService?.start(
                                AgentStartRequest(
                                    machineId = machineId,
                                    cwd = cwd,
                                    launcher = launcher,
                                    permissionMode = permissionMode,
                                    prompt = null,
                                    nameHint = todoNameHint,
                                    async = true,
                                    todoId = targetTodo.id
                                ),
                                operationId = opId
                            )
                            if (res is AgentStartResult.Created) {
                                dependencies.sessions().adopt(res.session)
                                dependencies.sender().send(res.session.key, prompt, java.util.UUID.randomUUID().toString())
                                handOffTodo = null
                                navigator.navigate(ChatRoute(res.session.machineId, res.session.sessionId))
                            }
                        } catch (_: Exception) {}
                    }
                },
                onHandOffExisting = { target, prompt ->
                    scope.launch {
                        try {
                            tasksStore?.linkTodoAgent(targetTodo.id, target.machineId, target.sessionId)
                            dependencies.sender().send(target, prompt, java.util.UUID.randomUUID().toString())
                            handOffTodo = null
                            navigator.navigate(ChatRoute(target.machineId, target.sessionId))
                        } catch (_: Exception) {}
                    }
                }
            )
        }
    }
}
