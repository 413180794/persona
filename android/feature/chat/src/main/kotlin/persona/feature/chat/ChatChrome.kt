package persona.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.sort.SessionRename
import persona.core.domain.sort.canAttach
import persona.core.domain.sort.canInterrupt
import persona.core.domain.sort.canTerminal
import persona.core.domain.sort.closeConfirmation
import persona.core.domain.sort.resumeCommand
import persona.core.domain.sort.source

@Composable
internal fun ChatHeader(
    session: Session?, machine: Machine?, sessions: List<Session>, state: ChatChromeState,
    connection: ConnectionState?, readOnly: Boolean, onBack: () -> Unit,
    onOpenSession: ((SessionKey) -> Unit)?, onConnectionDetails: (() -> Unit)?,
    onOverlay: (Boolean) -> Unit,
    onFiles: (() -> Unit)? = null, onGit: (() -> Unit)? = null, gitLabel: String = "Git",
    onSearch: (() -> Unit)? = null,
    statusState: StatusLineState = StatusLineState(), onDetails: () -> Unit = {},
    actions: @Composable RowScope.() -> Unit = {},
) {
    var menu by remember { mutableStateOf(false) }
    var dialog by rememberSaveable { mutableStateOf<String?>(null) }
    var title by rememberSaveable { mutableStateOf("") }
    var tabName by rememberSaveable { mutableStateOf("") }
    val clipboard = LocalClipboardManager.current
    val peers = remember(session, sessions) { spaceCompanions(session, sessions) }
    val children = session?.subagents.orEmpty().filter { it.running && it.sessionId.isNotBlank() }
    val linkTitle = connectionTitle(connection)
    val online = connection == null || connection == ConnectionState.Online
    val canAct = state.available && online && machine?.online != false && !readOnly && session != null
    // Renaming changes the terminal tab and Claude's /rename: a session without a terminal has neither.
    val canRename = state.canRename && online && machine?.online != false && !readOnly && session != null && session.canTerminal
    LaunchedEffect(session?.key, menu) { if (session != null && menu) state.loadMute(session) }
    LaunchedEffect(menu, dialog) { onOverlay(menu || dialog != null) }
    DisposableEffect(Unit) { onDispose { onOverlay(false) } }
    val line = resolveStatusLine(statusState)
    val status = line?.status ?: Status.entries.firstOrNull { it.name == session?.agentState?.name } ?: Status.Unknown
    // While the hub is out of reach the status line under the title says so (等待网络 / 连接中… / 更新中…) instead of
    // the session's state, which may be stale; it is back the moment the connection is.
    ChatTopBar(
        title = session?.displayTitle ?: "对话", onBack = onBack,
        status = if (linkTitle != null) null else status,
        statusText = linkTitle ?: when (line?.kind) {
            StatusLineKind.Ended -> if (statusState.replaced) "已换会话" else "已结束"
            StatusLineKind.MachineOffline -> "电脑离线"
            StatusLineKind.Submitting -> "回传中"
            StatusLineKind.Unconfirmed -> "待核对"
            StatusLineKind.NeedsYou -> "等你处理"
            StatusLineKind.Stopping -> "中断中"
            else -> status.label
        },
        // A session from a Codex app-server, Codex history or an agent runtime names its source first.
        source = listOfNotNull(session?.source?.label, machine?.displayName, session?.cwd?.takeIf(String::isNotBlank))
            .joinToString(" / ").takeIf(String::isNotBlank),
        busy = connection in setOf(ConnectionState.Connecting, ConnectionState.Updating),
        onTitleClick = onDetails,
    ) {
        actions()
        JIconButton(PhIcons.DotsThree, "更多操作", { menu = true })
    }
    if (menu) ChatActionsSheet(title = "会话操作", onDismiss = { menu = false }, actions = buildList {
        onSearch?.let { add(JAction("搜索", PhIcons.MagnifyingGlass, onClick = it)) }
        onFiles?.let { add(JAction("文件", PhIcons.FolderSimple, onClick = it)) }
        onGit?.let { add(JAction(gitLabel, onClick = it)) }
        add(JAction("会话详情", onClick = onDetails))
        add(JAction("会话信息", PhIcons.Info, enabled = session != null) { dialog = "info" })
        // A Codex app-server thread the phone may join live (iOS 实时接入); joined, it can answer its approvals.
        session?.takeIf { it.canAttach }?.let { target ->
            add(JAction("实时接入", PhIcons.PlugsConnected, enabled = canAct) { state.run(target, ChatAction.Attach) })
        }
        add(JAction("同伴 ${peers.size}", enabled = session != null) { dialog = "peers" })
        add(JAction("换会话", enabled = session != null) { dialog = "peers" })
        children.forEach { child -> add(JAction("子 Agent：${child.description?.takeIf(String::isNotBlank) ?: child.type}",
            enabled = onOpenSession != null) { onOpenSession?.invoke(SessionKey(session!!.machineId, child.sessionId)) }) }
        if (linkTitle != null) add(JAction(linkTitle) { dialog = "connection" })
        add(JAction("中断", enabled = canAct && session?.status in listOf("working", "blocked") && session?.canInterrupt != false) {
            state.run(session!!, ChatAction.Interrupt)
        })
        add(JAction("重命名", PhIcons.NotePencil, enabled = canRename) {
            title = session!!.title; tabName = session.slotName.orEmpty(); dialog = "rename"
        })
        add(JAction(if (state.muted == true) "开启通知" else "屏蔽通知", enabled = canAct && state.muted != null) {
            state.run(session!!, ChatAction.Mute(state.muted != true))
        })
        // Only a session in a terminal resumes there; a Codex thread by its own thread id.
        session?.resumeCommand()?.let { command ->
            add(JAction("复制恢复命令", enabled = !readOnly) {
                clipboard.setText(AnnotatedString(command)); state.notice = "已复制恢复命令"
            })
        }
        // Persona's conversations are not agents in a pane, and a session without a terminal has no pane to close.
        if (session?.assistant != true && session?.canTerminal != false)
            add(JAction("关闭 Agent", destructive = true, enabled = canAct && session?.isLive == true) { dialog = "close" })
    })
    val feedback = LocalAppFeedback.current
    LaunchedEffect(state.notice) {
        state.notice?.let { if (feedback?.notice(it) == true && state.pending == null) state.notice = null }
    }
    state.notice?.let { notice ->
        JNotice(notice, actionLabel = if (state.pending != null && session != null) "核对" else "关闭", onAction = {
            if (state.pending != null && session != null) {
                if (!state.busy && online) state.run(session, state.pending!!, verify = true)
            } else state.notice = null
        })
    }
    when (dialog) {
        "rename" -> JDialog(onDismiss = { dialog = null }, title = "重命名",
            text = "标签名同步到 herdr；会话名用 Claude 的 /rename，Mac 上 claude --resume 也看得到。",
            confirm = "保存" to { dialog = null; state.rename(session!!, title, tabName) },
            confirmEnabled = canRename && session?.let { SessionRename.changes(it, title, tabName).changed } == true,
            content = {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                    if (!session?.paneId.isNullOrBlank()) JTextField(tabName, { tabName = it }, label = "标签名（herdr tab）", singleLine = true)
                    JTextField(title, { title = it }, label = "会话名", singleLine = true)
                }
            })
        // The same words as the Agents list's long press and swipe (core:domain closeConfirmation).
        "close" -> session?.let { target ->
            val confirmation = target.closeConfirmation(machine?.displayName)
            JDialog(onDismiss = { dialog = null }, title = confirmation.title, text = confirmation.message,
                confirm = "关闭 Agent" to { dialog = null; state.run(target, ChatAction.Close) }, destructive = true, confirmEnabled = canAct)
        }
        "info" -> session?.let { target -> ChatSessionInfoSheet(target, machine, state, onDismiss = { dialog = null }) }
        "peers" -> JSheet(onDismiss = { dialog = null }, title = "同空间的 Agent") {
            if (peers.isEmpty()) JEmptyState("没有同伴会话", "这个空间暂时没有其他 Agent。")
            LazyColumn(Modifier.fillMaxWidth().heightIn(max = 400.dp), contentPadding = PaddingValues(bottom = 16.dp)) {
                items(peers, key = { it.key.path }) { peer ->
                    JRow(peer.displayTitle, subtitle = peer.slotName ?: peer.agentLabel,
                        leading = { JStatusDot(Status.entries.firstOrNull { it.name == peer.agentState.name } ?: Status.Unknown) },
                        enabled = onOpenSession != null, onClick = { dialog = null; onOpenSession?.invoke(peer.key) })
                }
            }
        }
        "connection" -> JDialog(onDismiss = { dialog = null }, title = linkTitle ?: "已连接", text = when (connection) {
            ConnectionState.CertificateMismatch -> "请检查 hub 的证书，并重新扫码配对。"
            ConnectionState.Revoked -> "请重新扫码配对。"
            ConnectionState.NeedsLocalNetworkPermission -> "请在系统设置中允许本地网络访问。"
            else -> "连接恢复后会自动更新对话。当前显示的内容可能尚未同步。"
        }, confirm = (if (onConnectionDetails != null) "连接诊断" else "知道了") to { dialog = null; onConnectionDetails?.invoke() }, dismiss = null)
    }
}

/** A scrollable sheet until JActionSheet supports long action lists. */
@Composable
internal fun ChatActionsSheet(actions: List<JAction>, onDismiss: () -> Unit, title: String) {
    JSheet(onDismiss = onDismiss, title = title) {
        LazyColumn(Modifier.fillMaxWidth(), contentPadding = PaddingValues(bottom = 16.dp)) {
            items(actions) { action ->
                JRow(action.label, leadingIcon = action.icon, enabled = action.enabled, destructive = action.destructive,
                    chevron = false, onClick = { onDismiss(); action.onClick() })
            }
        }
    }
}
