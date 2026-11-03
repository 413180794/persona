package persona.feature.agents

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import java.util.UUID

/** A new tab in an existing herdr workspace: machine and directory are fixed. */
internal data class AgentSpacePreset(val machineId: String, val workspaceId: String, val cwd: String, val name: String)

internal data class NewAgentState(
    val initialized: Boolean = false,
    val space: AgentSpacePreset? = null,
    val machineId: String = "",
    val selection: AgentDefaults = AgentDefaults(),
    val launchers: List<AgentLauncher> = emptyList(),
    val recent: List<AgentRecentDirectory> = emptyList(),
    val browser: AgentDirectories? = null,
    val browsing: Boolean = false,
    val browsePath: String = "",
    val prompt: String = "",
    val loading: Boolean = false,
    val starting: Boolean = false,
    val pending: AgentStartOperation? = null,
    val created: Session? = null,
    val error: String? = null,
) {
    val locked get() = !initialized || starting || pending != null || created != null
}

internal class NewAgentViewModel(
    private val service: AgentCreationService,
    private val store: AgentCreationStore,
    private val machines: StateFlow<List<Machine>>,
) : ViewModel() {
    private val mutable = MutableStateFlow(NewAgentState())
    val state = mutable.asStateFlow()
    private var machineJob: Job? = null
    private var browseJob: Job? = null
    init {
        viewModelScope.launch {
            try {
                val pending = store.pending(service.scope)
                if (pending != null) {
                    val r = pending.request
                    mutable.value = NewAgentState(initialized = true, machineId = r.machineId,
                        selection = AgentDefaults(r.cwd, r.launcher, r.permissionMode.orEmpty(), r.effort.orEmpty()),
                        prompt = r.prompt.orEmpty(), pending = pending, error = "上次启动的结果待确认，请核对结果")
                } else {
                    val last = store.lastMachine(service.scope)
                    mutable.update { it.copy(initialized = true) }
                    machines.collect { list ->
                        if (state.value.machineId.isEmpty() && !state.value.locked) {
                            val space = state.value.space
                            (if (space != null) list.firstOrNull { it.id == space.machineId && it.online }
                            else list.firstOrNull { it.id == last && it.online } ?: list.firstOrNull { it.online })?.let { selectMachine(it.id) }
                        }
                    }
                }
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { mutable.update { it.copy(error = "无法读取启动记录，请关闭后重试") } }
        }
    }
    /** Opens the form for a new session, or for a new tab in [space]; an unconfirmed start keeps its own form. */
    fun startIn(space: AgentSpacePreset?) {
        val current = state.value
        if (current.pending != null || current.created != null || current.starting || current.space == space) return
        machineJob?.cancel()
        browseJob?.cancel()
        // The space's machine is the only choice; until it is online nothing is selected and nothing can start.
        mutable.update { it.copy(space = space, machineId = if (space != null) "" else it.machineId,
            browsing = false, browser = null, loading = false, error = null) }
        val machine = space?.machineId ?: current.machineId
        if (current.initialized && machine.isNotEmpty()) selectMachine(machine)
    }
    fun selectMachine(id: String) {
        if (state.value.locked || machines.value.none { it.id == id && it.online }) return
        state.value.space?.let { if (it.machineId != id) return }
        machineJob?.cancel()
        browseJob?.cancel()
        mutable.update { it.copy(machineId = id, selection = AgentDefaults(), launchers = emptyList(), recent = emptyList(),
            browser = null, browsing = false, loading = true, error = null) }
        machineJob = viewModelScope.launch {
            try {
                val defaults = store.defaults(service.scope, id)
                val (launchers, dirs) = coroutineScope {
                    val l = async { service.launchers(id) }
                    val d = async { service.directories(id) }
                    l.await() to d.await()
                }
                mutable.update { it.copy(loading = false, launchers = launchers, recent = dirs.recent.take(6),
                    selection = defaults.copy(cwd = it.space?.cwd ?: defaults.cwd.ifEmpty { dirs.recent.firstOrNull()?.path ?: dirs.path },
                        launcher = defaults.launcher.takeIf { name -> launchers.any { l -> l.name == name } } ?: launchers.firstOrNull()?.name.orEmpty())) }
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { mutable.update { it.copy(loading = false, error = "未能加载目录和启动器，请重试") } }
        }
    }
    fun selection(value: AgentDefaults) {
        if (!state.value.locked && !state.value.loading) mutable.update { it.copy(selection = value, error = null) }
    }
    fun prompt(value: String) { if (!state.value.locked) mutable.update { it.copy(prompt = value) } }
    fun browse(path: String = state.value.selection.cwd) {
        if (state.value.locked || state.value.space != null) return
        browseJob?.cancel()
        val machine = state.value.machineId
        mutable.update { it.copy(browsing = true, browser = null, browsePath = path, loading = true, error = null) }
        browseJob = viewModelScope.launch {
            try {
                val dirs = service.directories(machine, path)
                mutable.update { it.copy(browser = dirs, loading = false) }
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { mutable.update { it.copy(loading = false, error = "目录没有打开，请重试或返回") } }
        }
    }
    fun closeBrowser(use: Boolean = false) {
        browseJob?.cancel()
        mutable.update { it.copy(browsing = false, browser = null, loading = false, error = null,
            selection = if (use && it.browser != null) it.selection.copy(cwd = it.browser.path) else it.selection) }
    }
    fun resume(session: Session) {
        val current = state.value
        if (!current.initialized || current.starting || current.created != null) return
        current.pending?.let {
            if (it.request.machineId == session.machineId && it.request.resumeSessionId == session.sessionId) submit()
            else mutable.update { it.copy(error = "另一个启动结果待确认，请先到新建会话中核对") }
            return
        }
        if (session.cwd.isBlank() || machines.value.none { it.id == session.machineId && it.online }) {
            mutable.update { it.copy(error = "机器上线且目录可用后才能继续") }
            return
        }
        machineJob?.cancel()
        browseJob?.cancel()
        mutable.update { it.copy(starting = true, loading = false, error = null) }
        viewModelScope.launch {
            try {
                val type = session.agent ?: "claude"
                val available = service.launchers(session.machineId)
                val launcher = session.launcher?.takeIf { name -> available.any { it.name == name && it.agentType == type } }
                    ?: available.firstOrNull { it.agentType == type && session.launcher.isNullOrBlank() }?.name
                if (launcher == null) {
                    mutable.update { it.copy(error = "原启动器不可用，请先在机器上检查") }
                    return@launch
                }
                execute(AgentStartOperation(UUID.randomUUID().toString(), AgentStartRequest(
                    session.machineId, session.cwd, launcher,
                    agentType = type.takeIf { it != "claude" }, resumeSessionId = session.sessionId,
                )), fresh = true)
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { mutable.update { it.copy(error = if (it.pending == null) "无法准备继续会话，请重试" else "继续结果待确认，请核对结果") } }
            finally { mutable.update { it.copy(starting = false) } }
        }
    }
    fun submit() {
        val current = state.value
        if (!current.initialized || current.starting || current.loading || current.created != null) return
        val previous = current.pending
        val agentType = current.launchers.firstOrNull { it.name == current.selection.launcher }?.agentType ?: "claude"
        if (previous == null && (current.selection.cwd.isBlank() || current.selection.launcher.isBlank() ||
                machines.value.none { it.id == current.machineId && it.online } ||
                (current.space != null && current.space.machineId != current.machineId))) return
        mutable.update { it.copy(starting = true, error = null) }
        viewModelScope.launch {
            try {
                val operation = previous ?: AgentStartOperation(UUID.randomUUID().toString(), AgentStartRequest(
                    current.machineId, current.selection.cwd, current.selection.launcher,
                    current.prompt.trim().takeIf { it.isNotEmpty() }, current.selection.mode.takeIf { it.isNotEmpty() && agentType == "claude" },
                    current.selection.effort.takeIf { it.isNotEmpty() && agentType == "claude" }, current.space?.workspaceId,
                    agentType.takeIf { it != "claude" },
                ))
                execute(operation, fresh = previous == null)
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { mutable.update { it.copy(error = if (it.pending == null) "启动记录未保存，请重试" else "未收到启动结果，请重试核对；不会重复创建") } }
            finally { mutable.update { it.copy(starting = false) } }
        }
    }
    private suspend fun execute(operation: AgentStartOperation, fresh: Boolean) {
        if (fresh) {
            store.pending(service.scope, operation)
            mutable.update { it.copy(pending = operation) }
        }
        val result = if (fresh) service.start(operation.request, operation.id) else service.operation(operation.id)
        when (result) {
            is AgentStartResult.Created -> {
                require(result.session.machineId == operation.request.machineId && result.session.sessionId.isNotBlank())
                val request = operation.request
                // A tab in an existing space must not replace the machine's remembered directory.
                if (request.workspaceId == null && request.resumeSessionId == null) store.saveDefaults(service.scope, request.machineId, AgentDefaults(request.cwd,
                    request.launcher, request.permissionMode.orEmpty(), request.effort.orEmpty()))
                // Keep the receipt until navigation is acknowledged, including across process death.
                mutable.update { it.copy(created = result.session) }
            }
            is AgentStartResult.Rejected -> {
                store.pending(service.scope, null)
                mutable.update { it.copy(pending = null, error = result.message) }
            }
            AgentStartResult.Pending -> mutable.update { it.copy(error = "正在启动，可以稍后核对结果") }
            AgentStartResult.Unknown -> mutable.update { it.copy(error = "启动结果待确认。为避免重复创建，请核对结果或到机器上查看") }
        }
    }
    fun opened() {
        viewModelScope.launch {
            try {
                store.pending(service.scope, null)
                mutable.update { it.copy(pending = null, created = null, prompt = "") }
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { mutable.update { it.copy(error = "启动记录未清理，下次会先核对结果") } }
        }
    }
}
