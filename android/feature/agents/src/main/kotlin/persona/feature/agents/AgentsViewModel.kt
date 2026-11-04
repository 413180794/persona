package persona.feature.agents

import persona.core.designsystem.PendingStatus
import persona.core.designsystem.StatusLineState
import persona.core.designsystem.resolveStatusLine
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.answer.*
import persona.core.domain.sort.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import java.time.Instant
import java.util.UUID

internal data class AgentsPageState(
    val sessions: List<Session> = emptyList(),
    val machines: List<Machine> = emptyList(),
    val preferences: AgentsPreferencesSnapshot = AgentsPreferencesSnapshot(),
    val settings: AgentListSettings = AgentListSettings(),
    val list: AgentListResult = AgentListResult(emptyMap(), emptyList(), emptyList()),
    val allCount: Int = 0,
    /** Every computer for the filter strip, with how many sessions the list would show for it. */
    val machineCounts: List<MachineCount> = emptyList(),
    /** What the list would show for all computers (the 全部机器 chip). */
    val machineTotal: Int = 0,
    /** Sessions of each hub group by its namespace, whichever group the list shows (AgentGroups.kt). */
    val groupCounts: Map<String?, Int> = emptyMap(),
)

internal enum class RequestDelivery(val label: String) {
    Sending(requireNotNull(resolveStatusLine(StatusLineState(answering = true))).text), Sent("已回传你的选择"), Unknown(PendingStatus.Unconfirmed.label),
}

internal class AgentsViewModel(
    private val repository: SessionsRepository,
    val interactions: InteractionCenter,
    val connection: HubConnection,
    val sender: MessageSender,
    private val preferences: AgentsPreferences,
    private val bindings: AgentsOperationsBindings? = null,
    private val mainConversationSender: MainConversationSender? = null,
    private val pairingScope: String = bindings?.operations?.scope.orEmpty(),
    private val renamer: SessionRenamer? = null,
    val tasks: persona.core.domain.tasks.TasksStore? = null,
) : ViewModel() {
    private val operations = bindings?.operations?.let { AgentOperationsState(it, preferences.hubCache) }
    val operationState = operations?.state ?: MutableStateFlow(AgentOperationsSnapshot())
    private val sending = mutableSetOf<String>()
    private val messageIds = mutableMapOf<String, String>()
    private var changing = false
    private val settings = MutableStateFlow(AgentListSettings())
    private val groupFilter = MutableStateFlow<GroupFilter?>(null)
    /** The hub group the list shows; null is every group (AgentGroups.kt). */
    val group = groupFilter.asStateFlow()
    private var previous: AgentListResult? = null
    private var previousContext: List<Any>? = null
    private val _delivery = MutableStateFlow<Map<String, RequestDelivery>>(emptyMap())
    val delivery = _delivery.asStateFlow()
    private val _requestHistory = MutableStateFlow<List<Interaction>>(emptyList())
    val requestHistory = _requestHistory.asStateFlow()
    private val _refreshing = MutableStateFlow(false)
    val refreshing = _refreshing.asStateFlow()
    private val _notice = MutableStateFlow<String?>(null)
    val notice = _notice.asStateFlow()
    private val _failure = MutableStateFlow<Throwable?>(null)
    val failure = _failure.asStateFlow()
    fun clearFailure() { _failure.value = null }
    private val todosFlow = tasks?.todos ?: MutableStateFlow(emptyList())
    val page = combine(renamer?.project(repository.sessions) ?: repository.sessions, repository.machines, preferences.snapshots(pairingScope),
        settings.combine(groupFilter, ::Pair), combine(operationState, todosFlow, ::Pair)) { everySession, everyMachine, localPrefs, (settings, group), (ops, todos) ->
        val source = if (group == null) everySession else everySession.filter { group.admits(it.machineId) }
        val machines = if (group == null) everyMachine else everyMachine.filter { group.admits(it.id) }
        val sessions = ops.project(source)
        val prefs = localPrefs.copy(pins = ops.pins?.domain() ?: localPrefs.pins, mutes = ops.mutes?.domain() ?: localPrefs.mutes)
        val rows = sessions.map { it.listSession(prefs) }
        // A partial machine snapshot must not hide sessions in machine grouping.
        val knownMachines = machines + sessions.map { it.machineId }.distinct().filter { id -> machines.none { it.id == id } }
            .map { Machine(it, "机器信息待更新") }
        val effective = settings.copy(grouping = prefs.grouping)
        val context = listOf(prefs.range, effective.grouping, effective.quick, effective.query, effective.machineFilter, prefs.pins, todos)
        if (context != previousContext) previous = null
        previousContext = context
        val sorted = scopedAgentList(rows, knownMachines, prefs, effective, todos)
        val result = retainOpenedRows(sorted, previous, effective.kept).also { previous = it }
        val all = scopedAgentList(rows, knownMachines, prefs,
            effective.copy(quick = AgentQuickFilter.All, kept = emptySet()), todos)
        // The chips count what each computer would show under the current range, search and quick filter.
        val everywhere = scopedAgentList(rows, knownMachines, prefs, effective.copy(machineFilter = "", kept = emptySet()), todos)
            .groups.flatMap { it.rows }
        AgentsPageState(sessions, knownMachines, prefs, effective, result, all.groups.sumOf { it.rows.size },
            machineCounts(knownMachines, everywhere), everywhere.size, groupCounts(everySession, everyMachine, prefs, effective, ops))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AgentsPageState())

    init {
        viewModelScope.launch {
            var previous = emptyList<Interaction>()
            interactions.pending.collect { pending ->
                val removed = previous.filter { old -> old.secret != true && pending.none { it.id == old.id && it.sessionKey == old.sessionKey } }
                _requestHistory.update { history ->
                    (removed.asReversed() + history).distinctBy { it.sessionKey to it.id }
                        .filter { old -> pending.none { it.id == old.id && it.sessionKey == old.sessionKey } }.take(50)
                }
                previous = pending
            }
        }
        if (operations != null) {
            localChange { operations.refresh() }
            viewModelScope.launch {
                repository.sessions.collect { sessions ->
                    operations.reconcile(sessions).forEach { bindings?.onSessionClosed?.invoke(it) }
                    messageIds.keys.removeAll { operation ->
                        operation.startsWith("loop:") && sessions.none { "loop:${it.key.path}" == operation && it.loop != null }
                    }
                }
            }
        }
    }

    fun filter(filter: AgentQuickFilter) { previous = null; settings.update { it.copy(quick = filter, kept = emptySet()) } }
    fun range(range: AgentRange) = localChange {
        previous = null
        settings.update { it.copy(quick = AgentQuickFilter.All, kept = emptySet(), folded = emptySet()) }
        preferences.range(pairingScope, range)
    }
    fun query(query: String) { previous = null; settings.update { it.copy(query = query, kept = emptySet(), folded = emptySet()) } }
    /** What the list would show for each group (the group chips), like the computer chips count. */
    private fun groupCounts(all: List<Session>, machines: List<Machine>, prefs: AgentsPreferencesSnapshot,
        settings: AgentListSettings, ops: AgentOperationsSnapshot): Map<String?, Int> {
        // One group (no suffixed ids anywhere): no group chips, nothing to count.
        if (machines.none { GroupKeys.namespaceOf(it.id) != null } && all.none { GroupKeys.namespaceOf(it.machineId) != null }) return emptyMap()
        val rows = ops.project(all).map { it.listSession(prefs) }
        return scopedAgentList(rows, machines, prefs, settings.copy(machineFilter = "", kept = emptySet()))
            .groups.flatMap { it.rows }.distinctBy { it.id }
            .groupingBy { GroupKeys.namespaceOf(it.session.machineId) }.eachCount()
    }

    fun machine(id: String) { previous = null; settings.update { it.copy(machineFilter = id, kept = emptySet()) } }
    /** Shows one hub group (or every group); a computer chip of another group is let go. */
    fun group(value: GroupFilter?) {
        previous = null
        groupFilter.value = value
        settings.update { if (value.admits(it.machineFilter) || it.machineFilter.isEmpty()) it else it.copy(machineFilter = "", kept = emptySet()) }
    }
    fun grouping(group: AgentGrouping) = localChange {
        previous = null
        settings.update { it.copy(kept = emptySet(), folded = emptySet()) }
        preferences.grouping(group)
    }
    fun compact() = localChange { preferences.compact(!page.value.preferences.compact) }
    fun pin(row: AgentSession) = change { it.preference(pin = true, space = false, key = row.id) }
    fun mute(row: AgentSession) = change { it.preference(pin = false, space = false, key = row.id, spaceKey = row.spacePreferenceKey) }
    fun spacePin(session: Session) = change { it.preference(true, true, "${session.machineId}/${session.spaceName}") }
    fun spaceMute(session: Session) = change { it.preference(false, true, "${session.machineId}/${session.spaceName}") }
    fun rename(session: Session, title: String?, tab: String?) {
        val edit = SessionRename.changes(session, title, tab)
        if (!edit.changed) return
        change("已重命名") {
            if (renamer != null) renamer.rename(session, edit.title, edit.tabName)
            else it.session(session, AgentSessionEdit(title = edit.title, tab = edit.tabName))
        }
    }
    fun renameSpace(session: Session, name: String) {
        val trimmed = name.trim()
        if (trimmed.isEmpty() || trimmed == session.spaceName) return
        change("已重命名") { it.renameSpace(session, trimmed) }
    }
    fun close(session: Session) = change {
        it.session(session, AgentSessionEdit(closed = true))
        bindings?.onSessionClosed?.invoke(session.key)
    }
    /** Still running in a terminal pane (not closed elsewhere since a confirmation opened). */
    fun closable(session: Session): Boolean = page.value.sessions.firstOrNull { it.key == session.key }
        ?.let { it.isLive && it.canTerminal } ?: page.value.sessions.isEmpty()
    /** 实时接入: join a Codex app-server thread live; the list refreshes after it. */
    fun attach(session: Session) = change("已实时接入") { it.attach(session) }
    fun stopLoop(session: Session) = sendOperation("loop:${session.key.path}") {
        if (connection.state.value != ConnectionState.Online) { _notice.value = "连上 hub 后才能停止循环"; return@sendOperation }
        sendOnce("loop:${session.key.path}", session.key, STOP_LOOP_PROMPT)
        _notice.value = "已提交停止请求，循环状态以 hub 更新为准"
    }
    fun handoff(session: Session, onQueued: () -> Unit, onBound: (SessionKey) -> Unit) = sendOperation("handoff:${session.key.path}") {
        val main = mainConversationSender ?: run { _notice.value = "会话操作尚未接入"; return@sendOperation }
        val operation = "handoff:${session.key.path}"
        val id = messageIds.getOrPut(operation) { UUID.randomUUID().toString() }
        handoffToMain(main, id, session, onQueued, onBound)
        messageIds.remove(operation)
    }
    private suspend fun sendOnce(operation: String, target: SessionKey, text: String) {
        val previous = messageIds[operation]?.let { id -> sender.outbox.value.firstOrNull { it.clientMessageId == id } }
        if (previous != null && previous.state !in setOf(DeliveryState.Rejected, DeliveryState.Refused)) {
            _notice.value = "请求已提交，请先到对话里核对"
            throw AlreadySubmitted()
        }
        val id = UUID.randomUUID().toString()
        messageIds[operation] = id
        sender.send(target, text, id)
        if (sender.outbox.value.any { it.clientMessageId == id && it.state in setOf(DeliveryState.Rejected, DeliveryState.Refused) }) {
            _notice.value = "请求未送出，请到对话里查看原因"
            throw AlreadySubmitted()
        }
    }
    private class AlreadySubmitted : Exception()
    private fun sendOperation(key: String, action: suspend () -> Unit) {
        if (!sending.add(key)) return
        viewModelScope.launch {
            try { action() }
            catch (_: AlreadySubmitted) { }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) { _failure.value = e }
            finally { sending.remove(key) }
        }
    }
    private fun change(success: String = "已保存", action: suspend (AgentOperationsState) -> Unit) {
        val operations = operations ?: run { _notice.value = "会话操作尚未接入"; return }
        if (connection.state.value != ConnectionState.Online) { _notice.value = "连上 hub 后才能操作"; return }
        if (changing) { _notice.value = "正在保存，请稍候"; return }
        changing = true
        viewModelScope.launch {
            var succeeded = false
            try {
                action(operations)
                succeeded = true
                _notice.value = success
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) {
                _failure.value = e
            } finally { changing = false }
            try {
                repository.refresh()
                operations.reconcile(repository.sessions.value).forEach { bindings?.onSessionClosed?.invoke(it) }
                operations.refresh()
                interactions.refresh()
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { if (succeeded) _notice.value = "操作已保存，暂时无法更新列表，请下拉刷新核对" }
        }
    }
    fun opened(row: AgentSession) {
        if (page.value.settings.quick != AgentQuickFilter.All) settings.update { it.copy(kept = it.kept + row.id) }
    }
    fun markRead(rows: List<AgentSession>) = localChange {
        if (page.value.settings.quick != AgentQuickFilter.All) settings.update { it.copy(kept = it.kept + rows.map { row -> row.id }) }
        preferences.markSeen(rows.map { it.session })
    }
    fun fold(group: AgentListGroup) {
        settings.update { current ->
            if (group.parent == "quiet") current.copy(
                expandedQuiet = if (group.folded) current.expandedQuiet + group.id else current.expandedQuiet - group.id,
                folded = if (group.folded) current.folded - group.id else current.folded + group.id,
            )
            else current.copy(folded = if (group.folded) current.folded - group.id else current.folded + group.id)
        }
    }
    fun fold(machine: AgentMachineGroup) {
        settings.update { it.copy(folded = if (machine.folded) it.folded - machine.id else it.folded + machine.id) }
    }
    fun foldAll(fold: Boolean) {
        previous = null
        settings.update { it.copy(
            folded = if (fold) (page.value.list.groups.map { group -> group.id } + page.value.list.machines.map { machine -> machine.id }).toSet() else emptySet(),
            expandedQuiet = if (fold) emptySet() else page.value.list.groups.filter { group -> group.parent == "quiet" }.map { group -> group.id }.toSet(),
        ) }
    }
    fun showIdle(group: AgentListGroup) {
        val key = when {
            group.id == "state/idle" -> "state-idle"
            group.id.startsWith("machine/") -> "machine-${group.id.removePrefix("machine/")}"
            else -> group.id
        }
        settings.update { it.copy(expandedIdle = it.expandedIdle + key) }
    }
    fun clearNotice() { _notice.value = null }

    fun linkTodo(todoId: String, session: Session) {
        viewModelScope.launch {
            try {
                tasks?.linkTodoAgent(todoId, session.machineId, session.sessionId)
                _notice.value = "已关联到待办"
            } catch (e: Exception) {
                _notice.value = "关联待办失败：${e.message}"
            }
        }
    }

    fun unlinkTodo(todoId: String, session: Session) {
        viewModelScope.launch {
            try {
                tasks?.unlinkTodoAgent(todoId, session.machineId, session.sessionId)
                _notice.value = "已解除关联"
            } catch (e: Exception) {
                _notice.value = "解除关联失败：${e.message}"
            }
        }
    }
    fun refresh() {
        if (_refreshing.value) return
        viewModelScope.launch {
            _refreshing.value = true
            try {
                repository.refresh(); interactions.refresh()
                operations?.reconcile(repository.sessions.value)?.forEach { bindings?.onSessionClosed?.invoke(it) }
                operations?.refresh()
            }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { /* Background refresh retains the cached list. */ }
            finally { _refreshing.value = false }
        }
    }
    fun answer(interaction: Interaction, action: AnswerAction, draft: AnswerDraft = AnswerDraft(), fullPlanRead: Boolean = false) {
        val latest = interactions.pending.value.firstOrNull { it.id == interaction.id } ?: return
        if (latest != interaction || latest.expired(Instant.now()) || interaction.id in _delivery.value) return
        // A session whose source does not approve (a Codex thread not joined live) is answered on the computer.
        if (page.value.sessions.firstOrNull { it.key == latest.sessionKey }?.canApprove == false) {
            _notice.value = "这个会话不允许在手机上审批"; return
        }
        if (connection.state.value != ConnectionState.Online) { _notice.value = "连上 hub 后才能回传选择"; return }
        val result = AnswerBuilder.build(latest, action, draft, AnswerContext(fullPlanRead = fullPlanRead))
        val resolution = result.resolution ?: return
        _delivery.update { it + (interaction.id to RequestDelivery.Sending) }
        viewModelScope.launch {
            try {
                interactions.resolve(interaction.id, resolution)
                _delivery.update { it + (interaction.id to RequestDelivery.Sent) }
                _notice.value = "已回传你的选择"
            } catch (e: CancellationException) {
                _delivery.update { it + (interaction.id to RequestDelivery.Unknown) }
                throw e
            } catch (_: Exception) {
                // A lost acknowledgement must never cause an automatic duplicate approval.
                _delivery.update { it + (interaction.id to RequestDelivery.Unknown) }
            }
        }
    }
    private fun localChange(block: suspend () -> Unit) {
        viewModelScope.launch {
            try { block() }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) { _failure.value = e }
        }
    }
}
