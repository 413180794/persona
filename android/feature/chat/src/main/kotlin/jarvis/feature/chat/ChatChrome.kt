package jarvis.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import jarvis.core.contracts.*
import jarvis.core.contracts.api.*
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import jarvis.core.domain.sort.SessionRename

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
    val canRename = state.canRename && online && machine?.online != false && !readOnly && session != null
    LaunchedEffect(session?.key, menu) { if (session != null && menu) state.loadMute(session) }
    LaunchedEffect(menu, dialog) { onOverlay(menu || dialog != null) }
    DisposableEffect(Unit) { onDispose { onOverlay(false) } }
    val line = resolveStatusLine(statusState)
    val status = line?.status ?: Status.entries.firstOrNull { it.name == session?.agentState?.name } ?: Status.Unknown
    ChatTopBar(
        title = session?.displayTitle ?: "对话", onBack = onBack,
        status = status, statusText = when (line?.kind) {
            StatusLineKind.Ended -> if (statusState.replaced) "已换会话" else "已结束"
            StatusLineKind.MachineOffline -> "电脑离线"
            StatusLineKind.Submitting -> "回传中"
            StatusLineKind.Unconfirmed -> "待核对"
            StatusLineKind.NeedsYou -> "等你处理"
            StatusLineKind.Stopping -> "中断中"
            else -> status.label
        },
        source = listOfNotNull(machine?.displayName, session?.cwd?.takeIf(String::isNotBlank)).joinToString(" / ").takeIf(String::isNotBlank),
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
        add(JAction("同伴 ${peers.size}", enabled = session != null) { dialog = "peers" })
        add(JAction("换会话", enabled = session != null) { dialog = "peers" })
        children.forEach { child -> add(JAction("子 Agent：${child.description?.takeIf(String::isNotBlank) ?: child.type}",
            enabled = onOpenSession != null) { onOpenSession?.invoke(SessionKey(session!!.machineId, child.sessionId)) }) }
        if (linkTitle != null) add(JAction(linkTitle) { dialog = "connection" })
        add(JAction("中断", enabled = canAct && session?.status in listOf("working", "blocked")) { state.run(session!!, ChatAction.Interrupt) })
        add(JAction("重命名", PhIcons.NotePencil, enabled = canRename) {
            title = session!!.title; tabName = session.slotName.orEmpty(); dialog = "rename"
        })
        add(JAction(if (state.muted == true) "开启通知" else "屏蔽通知", enabled = canAct && state.muted != null) {
            state.run(session!!, ChatAction.Mute(state.muted != true))
        })
        add(JAction("复制恢复命令", enabled = session != null && !readOnly) {
            clipboard.setText(AnnotatedString(resumeCommand(session!!))); state.notice = "已复制恢复命令"
        })
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
        "close" -> JDialog(onDismiss = { dialog = null }, title = "关闭 Agent？",
            text = "将退出「${session?.displayTitle.orEmpty()}」并关闭它的窗格。正在进行的工作会中断，对话记录会保留。",
            confirm = "关闭 Agent" to { dialog = null; state.run(session!!, ChatAction.Close) }, destructive = true, confirmEnabled = canAct)
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

@Composable
internal fun ChatModeBar(session: Session?, state: ChatChromeState, online: Boolean, onOverlay: (Boolean) -> Unit = {}) {
    var open by remember { mutableStateOf(false) }
    LaunchedEffect(open) { onOverlay(open) }
    DisposableEffect(Unit) { onDispose { onOverlay(false) } }
    val editable = session != null && state.available && online && session.isLive &&
        (session.agent == null || session.agent == "claude") && !session.assistant
    JIconButton(PhIcons.SlidersHorizontal, "模式", { open = true })
    if (open) JSheet(onDismiss = { open = false }, title = "对话模式") {
        Column(Modifier.heightIn(max = 520.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            if (!editable) Text("当前会话暂时不能调整模式", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
            if (session != null) {
                Text("文件权限", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
                permissionLabels.forEach { (value, label) ->
                    DecisionOption(label, { open = false; if (value != session.permissionMode) state.run(session, ChatAction.Modes(permissionMode = value)) },
                        selected = value == session.permissionMode, enabled = editable)
                }
                Spacer(Modifier.height(12.dp))
                Text("思考强度", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
                effortLabels.forEach { (value, label) ->
                    JRow(label, trailing = { JRadio(value == session.effort, null) }, enabled = editable,
                        onClick = { open = false; if (value != session.effort) state.run(session, ChatAction.Modes(effort = value)) })
                }
            }
        }
    }
}

internal fun chatModeHint(session: Session?): String? = listOfNotNull(
    session?.permissionMode?.takeIf { it != "default" }?.let { permissionLabels[it] ?: if (it == "bypassPermissions") "全部放行" else it },
    session?.effort?.takeIf { it != "high" }?.let { "思考${effortLabels[it] ?: it}" },
).takeIf { it.isNotEmpty() }?.joinToString("，")

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
