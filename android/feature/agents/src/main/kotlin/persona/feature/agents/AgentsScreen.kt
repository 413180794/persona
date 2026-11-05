package persona.feature.agents

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.sort.*
import persona.core.navigation.*
import kotlinx.coroutines.delay
import java.time.Instant

@Composable
internal fun AgentsScreen(
    model: AgentsViewModel,
    navigator: Navigator,
    metadata: AgentsMetadata,
    creationModel: NewAgentViewModel? = null,
    onMachineSelected: (String) -> Unit = {},
    groups: List<persona.core.contracts.api.HubGroupInfo> = emptyList(),
) {
    val page by model.page.collectAsStateWithLifecycle()
    val connection by model.connection.state.collectAsStateWithLifecycle()
    val group by model.group.collectAsStateWithLifecycle()
    val everyRequest by model.interactions.pending.collectAsStateWithLifecycle()
    val pending = remember(everyRequest, group) { if (group == null) everyRequest else everyRequest.filter { group.admits(it.machineId) } }
    val outbox by model.sender.outbox.collectAsStateWithLifecycle()
    val delivery by model.delivery.collectAsStateWithLifecycle()
    val requestHistory by model.requestHistory.collectAsStateWithLifecycle()
    val refreshing by model.refreshing.collectAsStateWithLifecycle()
    val notice by model.notice.collectAsStateWithLifecycle()
    val hubNotice by model.hubNotice.collectAsStateWithLifecycle()
    val herdrStates by (model.herdr?.states ?: kotlinx.coroutines.flow.MutableStateFlow(emptyMap<String, HerdrState>())).collectAsStateWithLifecycle()
    val startingHerdr by (model.herdr?.starting ?: kotlinx.coroutines.flow.MutableStateFlow(emptySet<String>())).collectAsStateWithLifecycle()
    val operations by model.operationState.collectAsStateWithLifecycle()
    var operationDialog by remember { mutableStateOf<AgentOperationDialog?>(null) }
    var pickingTodoForSession by remember { mutableStateOf<Session?>(null) }
    val todos by (model.tasks?.todos ?: kotlinx.coroutines.flow.MutableStateFlow(emptyList())).collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val feedback = persona.core.designsystem.LocalAppFeedback.current
    var menu by remember { mutableStateOf(false) }
    var newAgent by rememberSaveable { mutableStateOf(false) }
    var historyMachine by rememberSaveable { mutableStateOf<String?>(null) }
    var historySession by rememberSaveable { mutableStateOf<String?>(null) }
    val creation = creationModel?.state?.collectAsStateWithLifecycle()?.value
    LaunchedEffect(creation?.created) {
        creation?.created?.let { session ->
            newAgent = false
            historyMachine = null
            historySession = null
            navigator.navigate(ChatRoute(session.machineId, session.sessionId))
            creationModel.opened()
            model.refresh()
        }
    }
    var allRequests by rememberSaveable { mutableStateOf(false) }
    var requestDetail by remember(model) { mutableStateOf<Interaction?>(null) }
    var now by remember { mutableStateOf(Instant.now()) }
    val lifecycle = LocalLifecycleOwner.current
    LaunchedEffect(lifecycle) {
        lifecycle.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) { now = Instant.now(); delay(1_000) }
        }
    }
    LaunchedEffect(lifecycle, model.herdr, page.machines, connection) {
        lifecycle.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) { model.herdr?.refresh(); delay(5_000) }
        }
    }
    val failure by model.failure.collectAsStateWithLifecycle()
    LaunchedEffect(failure) {
        failure?.let {
            if (feedback?.failure(it) != true) snackbar.showSnackbar(persona.core.designsystem.operationFeedback(it).message)
            model.clearFailure()
        }
    }
    LaunchedEffect(notice) { notice?.let { if (feedback?.notice(it) != true) snackbar.showSnackbar(it); model.clearNotice() } }
    val names = remember(page.machines, groups) { groups.machineNames(page.machines) }
    val requests = remember(pending, page.sessions, page.preferences, page.settings, operations.sessions) {
        visibleAgentRequests(pending, page.sessions.map { it.listSession(page.preferences).session }, page.preferences.mutes, page.settings)
            .filter { operations.sessions[it.sessionKey]?.closed != true }
    }
    val messagesBySession = remember(outbox) { outbox.groupBy { it.session } }
    val unknownRequests = remember(pending, delivery) {
        pending.filter { delivery[it.id] == RequestDelivery.Unknown }.map { it.sessionKey }.toSet()
    }
    fun open(session: SessionKey) { navigator.navigate(ChatRoute(session.machineId, session.sessionId)) }
    fun connectionSettings() { navigator.navigate(ConnectionSettingsRoute) }
    fun openRow(row: AgentSession) {
        if (!row.session.isLive) {
            historyMachine = row.session.machineId
            historySession = row.session.sessionId
        } else { model.opened(row); open(row.session.key) }
    }
    fun newSession(space: AgentSpacePreset? = null) { creationModel?.startIn(space); newAgent = true }
    val spaces = remember(page.sessions, page.preferences) { agentSpaces(page.sessions.map { it.listSession(page.preferences) }) }
    // The list rows and the space page share one session menu and one space menu.
    val rowMenu: @Composable (AgentSession) -> List<JAction> = { row ->
        if (!row.session.isLive) listOfNotNull(JAction("只读打开", onClick = { openRow(row) }),
            JAction("删除对话", destructive = true, enabled = !operations.busy) { operationDialog = AgentOperationDialog.Delete(row.session) }
                .takeIf { row.session.canDelete })
        else AgentSessionMenuItems(row, row.id in page.preferences.pins.sessions, page.preferences.mutes.mutes(row), operations.busy,
            onOpen = { openRow(row) }, onPin = { model.pin(row) }, onMute = { model.mute(row) },
            onRead = { model.markRead(listOf(row)) }, onRename = { operationDialog = AgentOperationDialog.Rename(row.session) },
            onStopLoop = { model.stopLoop(row.session) }, onHandoff = {
                model.handoff(row.session, onQueued = { navigator.selectTab(AppTab.Jarvis) }, onBound = {
                    navigator.navigate(ChatRoute(it.machineId, it.sessionId, kind = ConversationKind.Assistant))
                })
            },
            onClose = { operationDialog = AgentOperationDialog.Close(row.session) },
            // A Codex app-server thread the phone may join live (iOS 实时接入).
            onAttach = if (row.session.canAttach) ({ model.attach(row.session) }) else null,
            onLinkTodo = { pickingTodoForSession = row.session },
            onUnlinkTodo = row.session.todo?.let { todo -> { model.unlinkTodo(todo.id, row.session) } },
            onDelete = { operationDialog = AgentOperationDialog.Delete(row.session) })
    }
    // In the list the space menu also opens the space page; the space page itself puts it in its bar.
    val spaceMenu: @Composable (AgentSpace, Boolean) -> Unit = { space, inList ->
        val session = space.members.first().session
        val unread = space.members.filter { it.state == AgentState.Unread }
        AgentSpaceMenu(session, space.preferenceKey in page.preferences.pins.spaces, space.preferenceKey in page.preferences.mutes.spaces,
            operations.busy, onPin = { model.spacePin(session) }, onMute = { model.spaceMute(session) },
            onRename = { operationDialog = AgentOperationDialog.RenameSpace(session) },
            onMarkRead = unread.takeIf { it.isNotEmpty() }?.let { rows -> { model.markRead(rows) } },
            onOpen = if (inList) ({ navigator.navigate(SpaceRoute(space.id, space.name)) }) else null, compact = inList)
    }
    val tree = remember(page.list, page.settings.machineFilter) { agentTreeEntries(page.list, page.settings.machineFilter) }
    val spacesById = remember(spaces, page.preferences.grouping, page.list) {
        when (page.preferences.grouping) {
            AgentGrouping.MachineSpace -> spaces.associateBy { it.id }
            // A repository whose sessions are all in one herdr space (the usual case) takes that space's marks and ⋯ (iOS).
            AgentGrouping.Repo -> {
                val byId = spaces.associateBy { it.id }
                page.list.groups.filter { it.repo == RepoLevel.Repository }.mapNotNull { repo ->
                    val members = repo.rows + page.list.groups.filter { it.under == repo.id }.flatMap { it.rows }
                    agentSpaces(members).singleOrNull()?.takeIf { it.members.first().session.spaceId != null }
                        ?.let { byId[it.id] }?.let { repo.id to it }
                }.toMap()
            }
            else -> emptyMap()
        }
    }

    val working = page.list.counts[AgentQuickFilter.Working] ?: 0
    val unread = page.list.counts[AgentQuickFilter.Unread] ?: 0
    val summary = when {
        requests.isNotEmpty() -> "有 ${requests.size} 件事等你定"
        working > 0 -> "$working 个 Agent 在干活"
        else -> "都安静着"
    }
    val subtitle = buildList {
        if (requests.isNotEmpty() && working > 0) add("$working 个 Agent 在干活")
        if (unread > 0) add("$unread 个刚做完")
    }.takeIf { it.isNotEmpty() }?.joinToString("，", postfix = "。")
    Box(Modifier.fillMaxSize()) {
        JListPage(
            title = names[page.settings.machineFilter] ?: "Agents", summary = summary, subtitle = subtitle, greet = true,
            titleBusy = connection in setOf(ConnectionState.Connecting, ConnectionState.Updating),
            onTitleClick = ::connectionSettings, refreshing = refreshing, onRefresh = model::refresh,
            actions = {
                JIconButton(PhIcons.ListChecks, "待办", { navigator.navigate(persona.core.navigation.TodosRoute) })
                JIconButton(PhIcons.Plus, "新建会话", { newSession() })
                JIconButton(PhIcons.SlidersHorizontal, "列表设置", { menu = true })
            },
        ) {
            item("search") {
                JSearchField(page.settings.query, model::query, "搜索空间、会话或最新消息", Modifier.padding(bottom = Tokens.Space.xs))
            }
            if (groups.size > 1) item("groups") {
                GroupFilterRow(groups, page.groupCounts, group, model::group)
            }
            item("machines") {
                // With more than one group a computer's chip says its group too (two groups can both have a MacBook).
                val chips = if (groups.size > 1) page.machineCounts.map { it.copy(machine = it.machine.copy(name = names[it.machine.id] ?: it.machine.displayName)) }
                    else page.machineCounts
                MachineFilterRow(chips, page.machineTotal, page.settings.machineFilter, { id -> model.machine(id); onMachineSelected(id) })
            }
            item("quick-filters") {
                QuickFilterBar(AgentQuickFilter.entries.map { filter ->
                    Bay(filter.label, if (filter == AgentQuickFilter.All) page.allCount else page.list.counts[filter] ?: 0, filter.state?.designStatus)
                }, AgentQuickFilter.entries.indexOf(page.settings.quick), { model.filter(AgentQuickFilter.entries[it]) }, Modifier.padding(vertical = Tokens.Space.s))
            }
            val currentNotice = hubNotice
            if (currentNotice != null && page.settings.query.isEmpty() && page.preferences.range != AgentRange.History) {
                item("hub-notice") {
                    HubNoticeRow(
                        notice = currentNotice,
                        onClick = { navigator.navigate(SettingsPageRoute(SettingsPage.HubUpdate)) },
                        onAutoOff = { model.setAutoUpdate(false) },
                        onNoticeSeen = model::noticeSeen,
                        onUpdate = model::startHubUpdate,
                        onDismiss = model::dismissHubNotice,
                        modifier = Modifier.padding(bottom = Tokens.Space.xs),
                    )
                }
            }
            if (connection != ConnectionState.Online) item("connection") { ConnectionNotice(connection, ::connectionSettings) }
            if (model.herdr != null && connection == ConnectionState.Online && page.preferences.range != AgentRange.History) {
                items(page.machines.filter { it.herdrStopped(herdrStates[it.id]) && (page.settings.machineFilter.isEmpty() || page.settings.machineFilter == it.id) },
                    key = { "herdr:${it.id}" }) { machine ->
                    HerdrNotice(names[machine.id] ?: machine.displayName, machine.id in startingHerdr) { model.herdr.start(machine.id) }
                }
            }
            if (page.preferences.range != AgentRange.History && requests.isNotEmpty()) {
                item("requests-title") { JSectionHeader("等你定", count = requests.size) }
                items(requests.take(3), key = { "request:${it.id}" }) { request ->
                    val owner = page.sessions.firstOrNull { it.key == request.sessionKey }
                    RequestCard(request, names[request.machineId], metadata, now, connection == ConnectionState.Online,
                        delivery[request.id], { requestDetail = request }, model::answer,
                        space = owner?.spaceName?.takeIf { it.isNotBlank() }, approvable = owner?.canApprove ?: true)
                }
                if (requests.size > 3) item("more-requests") {
                    JButton("还有 ${requests.size - 3} 个", { allRequests = true }, kind = JButtonKind.Secondary, size = JButtonSize.Small)
                }
            }
            if (page.preferences.range != AgentRange.History) item("request-history") {
                JTextButton("请求列表与历史", { allRequests = true })
            }
            if (page.settings.quick == AgentQuickFilter.Unread && page.list.unreadShown.isNotEmpty()) item("mark-read") {
                JTextButton("全部标为已读", { model.markRead(page.list.groups.flatMap { it.rows }.filter { it.state == AgentState.Unread }) })
            }
            if (page.list.groups.isEmpty()) item("empty") {
                JEmptyState(if (page.settings.query.isNotEmpty()) "没有匹配的会话" else "暂无会话", "可以切换筛选，或下拉刷新", icon = PhIcons.Robot,
                    action = { JButton("新建会话", { newSession() }) })
            }
            tree.forEach { entry ->
                when (entry) {
                    is TreeMachineEntry -> item(entry.key, contentType = "machine") {
                        val machine = entry.machine
                        AgentTreeHeader(machine.machine.displayName, machine.folded, { model.fold(machine) }, first = entry.first,
                            icon = machine.machine.deviceIcon, online = machine.machine.online,
                            detail = machine.repoCount?.let { "$it 个仓库，${machine.sessionCount} 个会话" }
                                ?: "${machine.spaceCount} 个空间，${machine.sessionCount} 个会话", counts = machine.headingCounts,
                            onMonitor = { navigator.navigate(MachineDetailRoute(machine.machine.id)) }, tag = groups.tagFor(machine.machine.id))
                    }
                    is TreeGroupEntry -> item(entry.key, contentType = if (entry.space) "space" else "section") {
                        val group = entry.group
                        val space = spacesById[group.id]
                        if (entry.space) AgentSpaceHeader(group.title, group.folded, { model.fold(group) }, depth = entry.depth, first = entry.first,
                            quiet = entry.quiet, sessions = group.rows.size, counts = group.headingCounts,
                            flags = buildList {
                                if (space != null && space.preferenceKey in page.preferences.pins.spaces) add(RowFlag("空间已置顶", PhIcons.PushPin))
                                if (space != null && space.preferenceKey in page.preferences.mutes.spaces) add(RowFlag("空间已屏蔽", PhIcons.Prohibit))
                            }, menu = space?.let { { spaceMenu(it, true) } }, icon = group.repo?.icon,
                            kind = group.repo?.kind ?: "空间")
                        else if (group.id.startsWith("todo/") && group.id != "todo/none") {
                            val todoId = group.id.removePrefix("todo/")
                            AgentTreeHeader(group.title, group.folded, { model.fold(group) }, first = entry.first,
                                icon = PhIcons.ListChecks, counts = group.headingCounts,
                                onAction = { navigator.navigate(persona.core.navigation.TodoDetailRoute(todoId)) },
                                actionIcon = PhIcons.CaretRight, actionLabel = "查看待办")
                        } else AgentTreeHeader(group.title, group.folded, { model.fold(group) }, first = entry.first,
                            icon = PhIcons.PushPin.takeIf { group.id == "pinned" }, counts = group.headingCounts)
                    }
                    is TreeRowEntry -> item(entry.key, contentType = "session") {
                        val row = entry.row
                        val info = metadata.rows[row.session.key] ?: AgentRowMetadata()
                        val operationUncertain = operations.uncertain.any {
                            it.endsWith(":${row.id}") || it == "space:${row.session.machineId}/${row.session.operationWorkspaceId}"
                        }
                        val infoWithDelivery = info.withDelivery(messagesBySession[row.session.key].orEmpty(), row.session.key in unknownRequests)
                            .let { if (operationUncertain) it.copy(needsConfirmation = true) else it }
                        SlotRow(row, names[row.session.machineId], infoWithDelivery, now, page.preferences.compact,
                            row.id in page.preferences.pins.sessions, page.preferences.mutes.mutes(row),
                            depth = entry.depth, position = GroupPosition(entry.first, entry.last), located = entry.located,
                            onOpen = { openRow(row) }, menuItems = { rowMenu(row) },
                            onClose = closeSwipe(row, operations.busy) { operationDialog = AgentOperationDialog.Close(row.session) })
                    }
                    is TreeIdleEntry -> item(entry.key, contentType = "idle") {
                        AgentIdleRow(entry.hidden, onClick = { model.showIdle(entry.group) }, depth = entry.depth)
                    }
                }
            }
        }
        SnackbarHost(snackbar, Modifier.align(Alignment.BottomCenter).navigationBarsPadding())
    }
    if (menu) JActionSheet(buildList {
        AgentRange.entries.forEach { range -> add(JAction("范围：${range.label}", checked = page.preferences.range == range, onClick = { model.range(range) })) }
        AgentGrouping.selectable.forEach { grouping -> add(JAction(grouping.label, checked = page.preferences.grouping == grouping, onClick = { model.grouping(grouping) })) }
        add(JAction("细列表", checked = page.preferences.compact, onClick = { if (!page.preferences.compact) model.compact() }))
        add(JAction("详细列表", checked = !page.preferences.compact, onClick = { if (page.preferences.compact) model.compact() }))
        add(JAction("全部折叠", checked = page.list.groups.isNotEmpty() && page.list.groups.all { it.folded } && page.list.machines.all { it.folded }, onClick = { model.foldAll(true) }))
        add(JAction("全部展开", checked = page.list.groups.isNotEmpty() && page.list.groups.none { it.folded } && page.list.machines.none { it.folded }, onClick = { model.foldAll(false) }))
        add(JAction("机器监控", icon = PhIcons.Gauge, onClick = { navigator.navigate(MachinesRoute) }))
    }, { menu = false }, title = "列表设置", note = "置顶和屏蔽同步到 hub；已读仅影响本机列表")
    if (historyMachine != null && historySession != null) {
        val target = page.sessions.firstOrNull { it.machineId == historyMachine && it.sessionId == historySession }
        JFullScreen({ historyMachine = null; historySession = null }) {
            persona.feature.chat.ChatDestination(ChatRoute(requireNotNull(historyMachine), requireNotNull(historySession)), navigator,
                historyReadOnly = true, onHistoryBack = { historyMachine = null; historySession = null },
                historyFooter = {
                    Column(Modifier.fillMaxWidth().navigationBarsPadding().padding(Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        creation?.error?.let { Text(it, style = PersonaTheme.type.caption) }
                        // A Codex history thread or a runtime's session is not resumed in a terminal from the phone.
                        val sourced = target?.takeIf { it.hasSource }
                        if (sourced != null) Text(sourced.readOnlyNotice, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                        else JButton(if (creation?.pending?.request?.resumeSessionId == historySession) "核对继续结果" else "继续这个会话",
                            loading = creation?.starting == true,
                            enabled = creationModel != null && creation?.initialized == true && creation.starting != true &&
                                target != null && (creation.pending != null ||
                                (connection == ConnectionState.Online && page.machines.any { it.id == target.machineId && it.online })),
                            onClick = { target?.let {
                                if (it.isLive && creation?.pending == null) {
                                    historyMachine = null
                                    historySession = null
                                    open(it.key)
                                } else creationModel?.resume(it)
                            } }, modifier = Modifier.fillMaxWidth())
                    }
                })
        }
    }
    operationDialog?.let { AgentOperationDialog(it, model, names[it.session.machineId]) { operationDialog = null } }
    pickingTodoForSession?.let { targetSession ->
        TodoPickerSheet(
            todos = todos,
            sessionLookup = { k -> page.sessions.firstOrNull { it.key == k } },
            onDismiss = { pickingTodoForSession = null },
            onPick = { todo ->
                model.linkTodo(todo.id, targetSession)
                pickingTodoForSession = null
            }
        )
    }
    if (newAgent) {
        if (creationModel != null) NewAgentDialog(creationModel, page.machines, connection == ConnectionState.Online) { newAgent = false }
        else JDialog(onDismiss = { newAgent = false }, title = "新建会话",
            text = "新建会话服务尚未接入，请完成 App 接线后再试。", confirm = "关闭" to { newAgent = false }, dismiss = null)
    }
    if (allRequests) RequestsDialog(requests, names, metadata, now, connection == ConnectionState.Online, delivery,
        onDismiss = { allRequests = false }, onOpen = { request ->
            allRequests = false
            requestDetail = request
        }, onAnswer = model::answer,
        history = visibleAgentRequests(requestHistory, page.sessions.map { it.listSession(page.preferences).session },
            page.preferences.mutes, page.settings))
    requestDetail?.let { selected ->
        val current = pending.firstOrNull { it.id == selected.id && it.sessionKey == selected.sessionKey }
        val request = current ?: selected
        RequestDetail(request, metadata, now, connection == ConnectionState.Online, delivery[request.id],
            onDismiss = { requestDetail = null }, onOpenChat = {
                requestDetail = null
                open(it.sessionKey)
            }, onAnswer = model::answer, readOnly = current == null || delivery[request.id] == RequestDelivery.Sent,
            onTerminal = if (page.sessions.firstOrNull { it.key == request.sessionKey }?.canTerminal == false) null else ({
                requestDetail = null
                navigator.navigate(TerminalRoute(request.machineId, request.sessionId))
            }), machine = page.machines.firstOrNull { it.id == request.machineId },
            blockedReason = page.sessions.firstOrNull { it.key == request.sessionKey }?.takeIf { !it.canApprove }
                ?.let { "这个会话不允许在手机上审批，请打开会话或到电脑上处理。" },
            terminalHelp = page.sessions.firstOrNull { it.key == request.sessionKey }?.canTerminal != false)
    }
}

@Composable
private fun ConnectionNotice(state: ConnectionState, onSettings: () -> Unit) {
    if (state in listOf(ConnectionState.CertificateMismatch, ConnectionState.Revoked, ConnectionState.NeedsLocalNetworkPermission)) {
        JNotice(title = state.title, text = when (state) {
            ConnectionState.CertificateMismatch -> "hub 的证书和配对时不一样，不会自动重连。确认是自己换了证书后，请重新扫码配对。"
            ConnectionState.Revoked -> "配对已失效，请重新扫码配对。"
            else -> "需要本地网络权限，授权后再连接。"
        }, status = Status.NeedsYou,
            actionLabel = if (state == ConnectionState.NeedsLocalNetworkPermission) "去授权" else "连接诊断与重新扫码", onAction = onSettings)
    } else Text("显示的是本机缓存", Modifier.padding(vertical = 8.dp), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
}

@Composable
internal fun SectionTitle(text: String) { JSectionHeader(text) }

/**
 * A session row of the tree and the space page. [located] rows sit outside the computer tree, so they say which
 * computer and space they run in; rows under a space do not repeat it.
 */
@Composable
internal fun SlotRow(
    row: AgentSession, machine: String?, metadata: AgentRowMetadata, now: Instant, compact: Boolean, pinned: Boolean, muted: Boolean,
    depth: Int, position: GroupPosition, located: Boolean, onOpen: () -> Unit, menuItems: @Composable () -> List<JAction>,
    slotLabel: String? = null, requestCount: Int = 0,
    /** Swiping the row left offers 关闭 (iOS); null where the agent cannot be closed from here ([closeSwipe]). */
    onClose: (() -> Unit)? = null,
) {
    var menu by remember { mutableStateOf(false) }
    val session = row.session
    val source = session.source
    JSwipeActions(end = onClose?.let { SwipeAction("关闭", PhIcons.X, Status.NeedsYou, it, destructive = true) }) {
    AgentRow(
        title = session.displayTitle, status = row.state.designStatus, depth = depth, position = position,
        onClick = onOpen, onLongClick = { menu = true }, compact = compact, muted = muted,
        selected = isSessionSelected(LocalDetailKey.current, session.key),
        // Where its work stands once the hub triaged its turn (docs/agent-work.md), else its last words.
        preview = session.workLine(now) ?: session.lastText?.takeIf { it.isNotBlank() }, time = relativeTime(row.updatedAt, now),
        // Codex, TRAEX or what the hub calls a runtime's agent ("DSH"); a session with a source names it in its facts instead.
        agent = session.agentLabel.takeIf { !session.isClaude && source == null },
        statusText = row.state.takeIf { it != AgentState.Idle && it != AgentState.Unknown }?.label,
        facts = buildList {
            // Where a Codex thread or a runtime's session comes from, and when the phone can only read it.
            source?.let { add(RowFact(listOfNotNull(it.label, it.agentName).joinToString(" / "))) }
            if (source?.readOnly == true) add(RowFact("仅查看", caution = true))
            if (located) listOfNotNull(machine, session.spaceName?.takeIf { it.isNotBlank() } ?: session.folderName.takeIf { it.isNotBlank() })
                .joinToString(" / ").takeIf { it.isNotBlank() }?.let { add(RowFact(it)) }
            session.todo?.let { add(RowFact("待办 · ${it.title}")) }
            slotLabel?.let { add(RowFact(it)) }
            if (requestCount > 0) add(RowFact("$requestCount 件事等你定", caution = true))
            session.loop?.let { add(RowFact(it.nextRunLabel(now).replaceFirst("循环 · ", "循环，"))) }
            session.background?.size?.takeIf { it > 0 }?.let { add(RowFact("后台 $it")) }
            session.subagents?.count { it.running == true }?.takeIf { it > 0 }?.let { add(RowFact("子 Agent $it")) }
            if (metadata.needsConfirmation) add(RowFact(if (metadata.verifying) PendingStatus.Verifying.label else PendingStatus.Unconfirmed.label, caution = true))
            if (metadata.pendingCount > 0) add(RowFact("待发 ${metadata.pendingCount}", caution = true))
            if (metadata.sending) add(RowFact(PendingStatus.Sending.label))
            if (metadata.draftLabel != null) add(RowFact(buildString {
                append("草稿")
                if (metadata.hasAttachments) append("含附件")
                if (metadata.draft.isNotBlank()) append("：${metadata.draft.replace('\n', ' ').take(20)}")
            }, caution = true))
        },
        flags = buildList {
            if (pinned) add(RowFlag("已置顶", PhIcons.PushPin))
            if (muted) add(RowFlag("已屏蔽", PhIcons.Prohibit))
        },
    )
    }
    if (menu) JActionSheet(menuItems(), { menu = false }, title = session.displayTitle)
}

/** Swiping a row left closes the agent (iOS): only a live agent in a terminal pane, while nothing else is being saved. */
internal fun closeSwipe(row: AgentSession, busy: Boolean, close: () -> Unit): (() -> Unit)? =
    close.takeIf { row.session.isLive && row.session.canTerminal && !row.session.assistant && !busy }

/** The session actions are shared by the home list and the space page. */
@Suppress("DEPRECATION")
@Composable
internal fun AgentSessionMenuItems(
    row: AgentSession, pinned: Boolean, muted: Boolean, busy: Boolean,
    onOpen: () -> Unit, onPin: () -> Unit, onMute: () -> Unit, onRead: () -> Unit,
    onRename: () -> Unit, onStopLoop: () -> Unit, onHandoff: () -> Unit, onClose: () -> Unit,
    onAttach: (() -> Unit)? = null,
    onLinkTodo: (() -> Unit)? = null,
    onUnlinkTodo: (() -> Unit)? = null,
    onDelete: (() -> Unit)? = null,
): List<JAction> {
    val clipboard = LocalClipboardManager.current
    val session = row.session
    return buildList {
        add(JAction(if (pinned) "取消置顶" else "置顶", PhIcons.PushPin, enabled = !busy, onClick = onPin))
        add(JAction(if (muted) "取消屏蔽" else "屏蔽", PhIcons.Prohibit, enabled = !busy, onClick = onMute))
        if (session.todo != null && onUnlinkTodo != null) {
            add(JAction("解除关联待办", PhIcons.ListChecks, enabled = !busy, onClick = onUnlinkTodo))
        } else if (onLinkTodo != null) {
            add(JAction("关联到待办…", PhIcons.ListChecks, enabled = !busy, onClick = onLinkTodo))
        }
        onAttach?.let { add(JAction("实时接入", PhIcons.PlugsConnected, enabled = !busy, onClick = it)) }
        // Renaming changes the terminal tab and Claude's /rename; a session without a terminal has neither.
        if (session.canTerminal) add(JAction("重命名", PhIcons.NotePencil, enabled = !busy, onClick = onRename))
        if (session.loop != null && session.canSend) add(JAction("停止循环", enabled = !busy, onClick = onStopLoop))
        add(JAction("交给 Persona", onClick = onHandoff))
        if (row.state == AgentState.Unread) add(JAction("标为已读", PhIcons.Check, onClick = onRead))
        // Only a session in a terminal resumes there; a Codex thread by its own thread id.
        session.resumeCommand()?.let { command -> add(JAction("拷贝恢复命令", onClick = { clipboard.setText(AnnotatedString(command)) })) }
        add(JAction("拷贝目录", enabled = session.cwd.isNotBlank(), onClick = { clipboard.setText(AnnotatedString(session.cwd)) }))
        add(JAction("打开会话", onClick = onOpen))
        if (session.isLive && session.canTerminal) add(JAction("关闭 Agent", destructive = true, enabled = !busy, onClick = onClose))
        if (session.canDelete && onDelete != null) add(JAction("删除对话", destructive = true, enabled = !busy, onClick = onDelete))
    }
}

private val ConnectionState.title: String get() = when (this) {
    ConnectionState.Online -> "Agents"
    ConnectionState.Connecting -> "连接中…"
    ConnectionState.Updating -> "更新中…"
    ConnectionState.WaitingForNetwork -> "等待网络"
    ConnectionState.NeedsLocalNetworkPermission -> "需要本地网络权限"
    ConnectionState.CertificateMismatch -> "hub 的证书变了"
    ConnectionState.Revoked -> "配对已失效"
}
/** What TalkBack calls the level (iOS WorktreeRow: 主工作树 / 工作树). */
internal val RepoLevel.kind get() = when (this) {
    RepoLevel.Repository -> "仓库"
    RepoLevel.MainCheckout -> "主工作树"
    RepoLevel.Worktree -> "工作树"
}
internal val RepoLevel.icon get() = when (this) {
    RepoLevel.Repository -> PhIcons.Folder
    RepoLevel.MainCheckout -> PhIcons.House
    RepoLevel.Worktree -> PhIcons.GitBranch
}
private val AgentGrouping.label: String get() = when (this) {
    AgentGrouping.Repo -> "按仓库"
    AgentGrouping.State -> "按状态"
    AgentGrouping.Todo -> "按待办"
    else -> "按电脑和空间"
}
private val AgentQuickFilter.label: String get() = when (this) { AgentQuickFilter.All -> "全部"; AgentQuickFilter.NeedsYou -> "需要你"; AgentQuickFilter.Unread -> "未读"; AgentQuickFilter.Working -> "工作中" }
internal val AgentState.designStatus: Status get() = when (this) {
    AgentState.NeedsYou -> Status.NeedsYou
    AgentState.Unread -> Status.Unread
    AgentState.Working -> Status.Working
    AgentState.Idle -> Status.Idle
    AgentState.Unknown -> Status.Unknown
    AgentState.Offline -> Status.Offline
}
internal val AgentState.label: String get() = designStatus.label
@Composable
internal fun StateDot(state: AgentState, breathe: Boolean = false) {
    JStatusMark(state.designStatus, animated = breathe)
}
