package jarvis.android.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.NotificationsOff
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.JarvisApp
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.ChoiceChip
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.IconAction
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.design.StatusDot
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.background
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.verticalScroll
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.HubException
import jarvis.android.settings.modelPurpose
import jarvis.android.ui.settings.KitOption
import jarvis.android.ui.settings.KitPickerRow
import kotlinx.coroutines.CancellationException
import jarvis.android.data.Launcher
import jarvis.android.data.Machine
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import kotlinx.coroutines.launch

/**
 * Long press on an agent (iOS's row menu): pin, mute, 实时接入, rename, close. Changes show at once and are put back if
 * the hub refuses. Closing asks first ([CloseAgentDialog]).
 */
@Composable
fun AgentActionsSheet(
    hub: Hub, s: Session, pinned: Boolean, muted: Boolean, machine: String?,
    onDismiss: () -> Unit, onError: (String) -> Unit,
) {
    var renaming by remember { mutableStateOf(false) }
    var closing by remember { mutableStateOf(false) }
    var pickingTodo by remember { mutableStateOf(false) }
    val place = listOfNotNull(machine, s.spaceName?.takeIf { it.isNotBlank() }?.let { sp -> listOfNotNull(sp, s.slotName?.takeIf { it.isNotBlank() }).joinToString(" › ") })
        .joinToString(" · ")

    // The app scope, not the sheet's: dismissing the sheet would cancel its own scope halfway through the call.
    fun act(what: String, block: suspend () -> Unit) {
        onDismiss()
        JarvisApp.instance.scope.launch { runCatching { block() }.onFailure { onError("$what 没成功：${it.message}") } }
    }

    if (!renaming && !closing && !pickingTodo) JSheet(onDismiss) {
        Row(Modifier.fillMaxWidth().padding(horizontal = JSpace.page + JSpace.xs, vertical = JSpace.s), verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
            StatusDot(s.agentState, muted = muted)
            Column(Modifier.weight(1f)) {
                Text(s.displayTitle, style = JTheme.type.heading, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (place.isNotEmpty()) Text(place, style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        GroupCard(Modifier.padding(top = JSpace.s)) {
            ActionRow(if (pinned) "取消置顶" else "置顶", JIcons.pin) { act(if (pinned) "取消置顶" else "置顶") { hub.togglePin(s) } }
            RowDivider(ActionTextColumn)
            ActionRow(if (muted) "开启通知" else "屏蔽通知", if (muted) JIcons.unmute else JIcons.mute,
                subtitle = if (!muted) "不再弹提问和完成的通知，列表里弱化显示" else null) { act(if (muted) "开启通知" else "屏蔽通知") { hub.toggleMute(s) } }
            // 待办关联 / 解除关联 (umbrella.final.md)
            RowDivider(ActionTextColumn)
            if (s.todo != null) {
                ActionRow("解除关联", JIcons.todo, subtitle = "从待办「${s.todo.title}」解除关联") {
                    act("解除关联") { hub.unlinkTodoAgent(s.todo.id, s.key) }
                }
            } else {
                ActionRow("关联到待办…", JIcons.todo) { pickingTodo = true }
            }
            // 实时接入: a session an agent runtime holds read-only (DSH…) can be taken over live.
            if (s.canAttach) {
                RowDivider(ActionTextColumn)
                ActionRow("实时接入", JIcons.terminal) { act("实时接入") { hub.api.attach(s.key); hub.refresh() } }
            }
            if (s.isLive && s.canTerminal) {
                RowDivider(ActionTextColumn)
                ActionRow("重命名…", JIcons.edit, subtitle = "同时改 herdr 的标签页名和 Claude 的会话名") { renaming = true }
            }
        }
        if (s.isLive && s.canTerminal) GroupCard(Modifier.padding(top = JSpace.m)) {
            ActionRow("关闭 Agent…", JIcons.close, danger = true) { closing = true }
        }
    }

    if (pickingTodo) {
        TodoPickerSheet(
            hub = hub,
            onDismiss = { pickingTodo = false; onDismiss() },
            onPick = { todo ->
                pickingTodo = false
                act("关联待办") { hub.linkTodoAgent(todo.id, s.key) }
            }
        )
    }

    if (renaming) {
        RenameDialog(
            title = "重命名", current = s.displayTitle, note = null,
            onDismiss = { renaming = false; onDismiss() },
            onSave = { name -> renaming = false; act("重命名") { hub.api.rename(s.key, name) } },
        )
    }

    if (closing) CloseAgentDialog(hub, s, machine, onDismiss = { closing = false; onDismiss() }, onError = onError)
}

/**
 * 关闭 Agent's confirmation (iOS `CloseAgentSheet`, the same from the row's swipe, its long-press menu and the chat's ⋯):
 * which agent, what closing does — it quits the agent and closes its herdr tab; the transcript stays, so it can be
 * resumed from 最近 7 天.
 */
@Composable
fun CloseAgentDialog(
    hub: Hub, s: Session, machine: String?, onDismiss: () -> Unit, onError: (String) -> Unit,
    /** After 关闭 Agent is tapped. */
    onConfirmed: () -> Unit = {},
) {
    val place = listOfNotNull(machine, s.spaceName?.takeIf { it.isNotBlank() }?.let { sp -> listOfNotNull(sp, s.slotName?.takeIf { it.isNotBlank() }).joinToString(" › ") })
        .joinToString(" · ")
    JSheet(onDismiss) {
        Column(Modifier.fillMaxWidth().padding(horizontal = JSpace.page), horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
            Box(Modifier.size(JSize.box56).clip(CircleShape).background(JTheme.colors.danger.copy(alpha = 0.12f)), contentAlignment = Alignment.Center) {
                Icon(JIcons.close, null, Modifier.size(JSize.well), tint = JTheme.colors.danger)
            }
            Text("关闭这个 Agent？", style = JTheme.type.title, color = JTheme.colors.label)
            Text(listOf(s.displayTitle, place).filter { it.isNotEmpty() }.joinToString(" · "), style = JTheme.type.secondaryMedium, color = JTheme.colors.label,
                maxLines = 2, overflow = TextOverflow.Ellipsis, textAlign = androidx.compose.ui.text.style.TextAlign.Center)
            Text(
                "会退出 ${machine ?: "电脑"} 上的 ${s.agentLabel}，并关掉它在 herdr 里的标签页" +
                    (s.slotName?.takeIf { it.isNotBlank() }?.let { "「$it」" } ?: "") +
                    "（分屏的标签页只关它这一格）。正在做的事会中断。对话记录还在，之后可以在「最近 7 天」里发消息恢复。",
                style = JTheme.type.secondary, color = JTheme.colors.secondaryText, textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
            PillButton("关闭 Agent", {
                // It may have stopped being closable (closed elsewhere) since the sheet opened.
                if (hub.session(s.key)?.canTerminal != true) { onDismiss(); return@PillButton }
                onDismiss(); onConfirmed()
                // The app scope: the sheet's own would be cancelled as it closes.
                JarvisApp.instance.scope.launch { runCatching { hub.api.close(s.key) }.onFailure { onError("关闭 没成功：${it.message}") } }
            }, Modifier.fillMaxWidth().padding(top = JSpace.s), kind = ButtonKind.DestructiveProminent, large = true)
            PillButton("取消", onDismiss, Modifier.fillMaxWidth(), kind = ButtonKind.Neutral, large = true)
        }
    }
}

/** Where an action row's words start (after its symbol). */
private val ActionTextColumn = JSpace.l + JSize.iconS + JSpace.l

@Composable
private fun ActionRow(title: String, icon: androidx.compose.ui.graphics.vector.ImageVector, subtitle: String? = null, danger: Boolean = false, onClick: () -> Unit) {
    val color = if (danger) JTheme.colors.danger else JTheme.colors.label
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.box50).clickable(onClick = onClick).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.l),
    ) {
        Icon(icon, null, Modifier.size(JSize.iconS), tint = if (danger) color else JTheme.colors.accent)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(title, style = JTheme.type.body, color = color, maxLines = 1)
            subtitle?.let { Text(it, style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 2) }
        }
    }
}

/**
 * Starts claude on one of the computers (iOS `NewChatView`): pick the machine, a folder (recent ones first, or
 * browse from home), the launcher and the permission mode, and maybe a first message. The hub opens a new herdr tab
 * there (docs/protocol.md「REST」).
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun NewSessionSheet(
    hub: Hub, machines: List<Machine>, onDismiss: () -> Unit, onStarted: (SessionKey) -> Unit,
    /** From a space page: start a new tab in this workspace, in its folder (machine and folder can't change). */
    preset: SpacePreset? = null,
    /** Prefilled first message (the knowledge base's 开 Agent puts the project's profile here). */
    initialPrompt: String = "",
    /** (machine id, folder) to start in, picked but changeable (a project's code directory). */
    initialDir: Pair<String, String>? = null,
) {
    val scope = rememberCoroutineScope()
    var machine by remember {
        mutableStateOf(machines.firstOrNull { it.id == (preset?.machineId ?: initialDir?.first) } ?: machines.firstOrNull())
    }
    var dir by remember { mutableStateOf<ApiClient.DirList?>(null) }
    var picked by remember { mutableStateOf<String?>(null) }
    var launchers by remember { mutableStateOf<List<Launcher>>(emptyList()) }
    var launcher by remember { mutableStateOf<String?>(null) }
    var unifiedModels by remember { mutableStateOf(false) }
    var loadingModels by remember { mutableStateOf(true) }
    var prompt by remember { mutableStateOf(initialPrompt) }
    var mode by remember { mutableStateOf("") } // "" = whatever the launcher uses
    var loading by remember { mutableStateOf(false) }
    var starting by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    fun browse(path: String) {
        val m = machine ?: return
        loading = true; error = null
        scope.launch {
            runCatching { hub.api.dirs(m.id, path) }.onSuccess { dir = it }.onFailure { error = it.message }
            loading = false
        }
    }

    LaunchedEffect(machine?.id) {
        val m = machine ?: return@LaunchedEffect
        dir = null; picked = preset?.cwd ?: initialDir?.second?.takeIf { initialDir.first == m.id }; launcher = null
        if (preset == null) browse("")
        loadingModels = true
        try {
            launchers = hub.api.launchers(m.id)
            unifiedModels = try {
                hub.api.modelPurpose("agent.new")
                true
            } catch (e: HubException) {
                if (e.status != 404) throw e
                false
            }
            launcher = if (unifiedModels) null else launchers.firstOrNull()?.name
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = "读不到新 Agent 模型配置：${e.message}"
        } finally {
            loadingModels = false
        }
    }

    JSheet({ if (!starting) onDismiss() }, title = if (preset != null) "在「${preset.spaceName}」开新标签" else "新开会话", onClose = { if (!starting) onDismiss() }) {
        Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            if (preset != null) Text(tailOf(preset.cwd), style = JTheme.type.code, color = JTheme.colors.secondaryText,
                modifier = Modifier.padding(horizontal = JSpace.page + JSpace.xs))
            if (machines.size > 1 && preset == null) {
                SectionHeader("电脑")
                FlowRow(Modifier.padding(horizontal = JSpace.page), horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    machines.forEach { m ->
                        ChoiceChip(m.displayName, machine?.id == m.id, { if (!starting) machine = m },
                            lead = { Icon(JIcons.machine, null, Modifier.size(JSize.markM), tint = if (machine?.id == m.id) JTheme.colors.accent else JTheme.colors.secondaryText) })
                    }
                }
            }

            val d = dir
            if (preset == null) {
                SectionHeader("目录") { if (loading) CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule, color = JTheme.colors.secondaryText) }
                GroupCard {
                    Row(Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget).padding(start = JSpace.xs, end = JSpace.l), verticalAlignment = Alignment.CenterVertically) {
                        if (d?.parent != null) IconAction(JIcons.back, "上一级", { browse(d.parent) }, enabled = !starting, size = JSize.markL)
                        else Spacer(Modifier.width(JSpace.m))
                        Text(tailOf(picked ?: d?.path ?: ""), style = JTheme.type.code, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f))
                    }
                    val recent = d?.recent.orEmpty()
                    Column(Modifier.heightIn(max = JSize.box300).verticalScroll(rememberScrollState())) {
                        // The hub sends recent directories only with the home listing (an empty path).
                        if (recent.isNotEmpty()) {
                            Text("最近用过", style = JTheme.type.sectionHeader, color = JTheme.colors.secondaryText, modifier = Modifier.padding(start = JSpace.l, top = JSpace.s, bottom = JSpace.xs))
                            recent.forEach { r -> DirRow(r.name.ifBlank { r.path.substringAfterLast('/') }, r.path, recent = true, selected = picked == r.path) { picked = r.path } }
                            Text("浏览", style = JTheme.type.sectionHeader, color = JTheme.colors.secondaryText, modifier = Modifier.padding(start = JSpace.l, top = JSpace.m, bottom = JSpace.xs))
                        }
                        d?.entries.orEmpty().forEach { e ->
                            DirRow(e.name, if (e.isGit) "git 仓库" else null, recent = false, selected = picked == e.path) {
                                // A tap picks the folder; a second tap on the picked one goes inside it.
                                if (picked == e.path) { picked = null; browse(e.path) } else picked = e.path
                            }
                        }
                    }
                }
            }

            if (unifiedModels) {
                // Models are configured by purpose (设置 › 模型配置): the launcher follows it unless one is picked here.
                SectionHeader("启动器")
                GroupCard {
                    KitPickerRow(
                        "本次启动器", launcher.orEmpty(),
                        listOf(KitOption("", "跟随模型配置")) + launchers.map { KitOption(it.name, it.name) },
                        onChange = { launcher = it.takeIf(String::isNotEmpty) }, enabled = !starting,
                    )
                }
            } else if (launchers.size > 1) {
                SectionHeader("启动器")
                FlowRow(Modifier.padding(horizontal = JSpace.page), horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    launchers.forEach { l -> ChoiceChip(l.name, launcher == l.name, { if (!starting) launcher = l.name },
                        lead = { Icon(JIcons.model, null, Modifier.size(JSize.markM), tint = if (launcher == l.name) JTheme.colors.accent else JTheme.colors.secondaryText) }) }
                }
            }
            // The same choices as iOS (SessionModes.permissionModes); bypass can only be picked when starting.
            SectionHeader("权限模式")
            Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = JSpace.page), horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                PERMISSION_MODES.forEach { (id, title) ->
                    ChoiceChip(title, mode == id, { if (!starting) mode = id }, lead = if (id.isEmpty()) null else ({
                        Icon(modeIcon(id), null, Modifier.size(JSize.markM), tint = if (id == "bypassPermissions") JTheme.colors.danger else if (mode == id) JTheme.colors.accent else JTheme.colors.secondaryText)
                    }))
                }
            }
            SectionHeader("第一条消息")
            JTextField(prompt, { prompt = it }, "可以不填，进了对话再说", Modifier.padding(horizontal = JSpace.page), minLines = 2, maxLines = 5,
                enabled = !starting, fill = JTheme.colors.card)
            error?.let {
                Row(Modifier.padding(horizontal = JSpace.page + JSpace.xs, vertical = JSpace.xs), horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    Icon(JIcons.error, null, Modifier.size(JSize.markS), tint = JTheme.colors.danger)
                    Text(it, color = JTheme.colors.danger, style = JTheme.type.caption)
                }
            }
            val cwd = picked ?: d?.path
            PillButton(
                if (starting) "正在启动…" else "在这里启动",
                onClick = {
                    val m = machine ?: return@PillButton
                    starting = true; error = null
                    val panesBefore = hub.livePanes(m.id)
                    scope.launch {
                        val first = prompt.trim()
                        runCatching { hub.api.start(m.id, cwd!!, launcher ?: if (unifiedModels) "" else "claude", first.ifEmpty { null }, mode.ifEmpty { null }, preset?.workspaceId) }
                            .onSuccess { s0 ->
                                // The hub answers at once and starts claude in the background: open the chat now, and
                                // send the first message as an ordinary one (it waits on the node until claude is ready).
                                val s = s0.copy(machineId = m.id)
                                hub.adopt(s)
                                if (first.isNotEmpty()) jarvis.android.chat.Outbox.send(hub, s.key, first)
                                onStarted(s.key)
                            }
                            .onFailure { e ->
                                // Often claude did start and the node only gave up on it:
                                //  - it stopped on a dialog (trust this folder, the bypass warning…), which the hub turns
                                //    into a card the user can answer in the session;
                                //  - its footer is one the node doesn't recognise yet ("did not become ready").
                                // Either way, if a session came up in that directory since we pressed start, open it.
                                val msg = e.message.orEmpty()
                                val maybeUp = "agent_blocked" in msg || "did not become ready" in msg
                                val fresh = if (maybeUp) { hub.refresh(); hub.startedIn(m.id, cwd!!, panesBefore) } else null
                                if (fresh != null) onStarted(fresh.key)
                                else { error = "没启动起来：${msg.lineSequence().firstOrNull()}"; starting = false }
                            }
                    }
                },
                modifier = Modifier.fillMaxWidth().padding(horizontal = JSpace.page, vertical = JSpace.m),
                kind = ButtonKind.Prominent, large = true, icon = if (starting) null else JIcons.run,
                enabled = !starting && !loadingModels && machine != null && !cwd.isNullOrBlank(),
            )
        }
    }
}

/** A folder to start in: picked, it is on the accent's soft tint with a check; tapped again, it opens. */
@Composable
private fun DirRow(name: String, detail: String?, recent: Boolean, selected: Boolean, onClick: () -> Unit) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().background(if (selected) c.accent.copy(alpha = 0.1f) else androidx.compose.ui.graphics.Color.Transparent)
            .clickable(onClick = onClick).heightIn(min = JSize.tapTarget).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Icon(if (recent) JIcons.recent else JIcons.folder, null, Modifier.size(JSize.iconXS), tint = if (selected) c.accent else c.secondaryText)
        Column(Modifier.weight(1f)) {
            Text(name, style = JTheme.type.body, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            detail?.let { Text(tailOf(it), style = JTheme.type.caption, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis) }
        }
        if (selected) Icon(JIcons.done, "已选", Modifier.size(JSize.iconXS), tint = c.accent)
    }
}

/** The end of a long path ("…/code/foo/bar"): the part that tells folders apart (no start-ellipsis in this Compose). */
fun tailOf(path: String, keep: Int = 3): String {
    val parts = path.trimEnd('/').split('/')
    return if (parts.size <= keep + 1) path else "…/" + parts.takeLast(keep).joinToString("/")
}

/** Permission modes to start with (iOS SessionModes.permissionModes); "" leaves it to the launcher. */
val PERMISSION_MODES = listOf(
    "" to "默认",
    "default" to "手动确认",
    "acceptEdits" to "自动改文件",
    "plan" to "只做计划",
    "auto" to "自动",
    "bypassPermissions" to "全部放行",
)

/** Rename with the current name selected and focused: typing replaces it, a tap puts the cursor in it. */
@Composable
fun RenameDialog(title: String, current: String, note: String?, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var name by remember { mutableStateOf(TextFieldValue(current, TextRange(0, current.length))) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    val trimmed = name.text.trim()
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, singleLine = true, modifier = Modifier.fillMaxWidth().focusRequester(focus))
                note?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
        },
        confirmButton = { TextButton(onClick = { onSave(trimmed) }, enabled = trimmed.isNotEmpty() && trimmed != current) { Text("保存", maxLines = 1) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消", maxLines = 1) } },
    )
}

/** Where the space page's "在这个空间开新标签" starts claude. */
data class SpacePreset(val machineId: String, val cwd: String, val workspaceId: String, val spaceName: String)
