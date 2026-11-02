package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.JarvisApp
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.Machine
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.data.Todo
import jarvis.android.data.todoHandOffPrompt
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.ChoiceChip
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SectionHeader
import kotlinx.coroutines.launch
import java.util.UUID

enum class HandOffMode(val title: String) {
    New("新开一个"), Existing("交给已有的")
}

@Composable
fun TodoHandOffSheet(
    hub: Hub,
    todo: Todo,
    onDismiss: () -> Unit,
    onStarted: (SessionKey) -> Unit,
) {
    JSheet(onDismiss, title = "交给 Agent", onClose = onDismiss) {
        TodoHandOffBody(hub, todo, onDismiss, onStarted)
    }
}

@Composable
fun TodoHandOffBody(
    hub: Hub,
    todo: Todo,
    onDismiss: () -> Unit = {},
    onStarted: (SessionKey) -> Unit = {},
) {
    var mode by remember { mutableStateOf(HandOffMode.New) }
    var instruction by remember { mutableStateOf("") }
    val machines by hub.machines.collectAsState()
    val byMachine by hub.sessions.collectAsState()
    val scope = rememberCoroutineScope()

    // 新开一个时的参数
    val onlineMachines = remember(machines) { machines.filter { it.online }.ifEmpty { machines } }
    var selectedMachine by remember(onlineMachines) { mutableStateOf(onlineMachines.firstOrNull()) }
    var cwd by remember { mutableStateOf("~/code/weather") }
    var launcher by remember { mutableStateOf("claude") }
    var permissionMode by remember { mutableStateOf("default") }
    var dirList by remember { mutableStateOf<ApiClient.DirList?>(null) }
    var showDirPicker by remember { mutableStateOf(false) }
    var showMachinePicker by remember { mutableStateOf(false) }
    var showLauncherPicker by remember { mutableStateOf(false) }
    var showModePicker by remember { mutableStateOf(false) }
    var starting by remember { mutableStateOf(false) }

    // 预填目录
    LaunchedEffect(selectedMachine?.id) {
        val mid = selectedMachine?.id ?: return@LaunchedEffect
        runCatching {
            val d = hub.api.dirs(mid, "")
            dirList = d
            if (cwd.isEmpty()) {
                cwd = d.recent?.firstOrNull()?.path ?: d.path
            }
        }
    }

    // 交给已有时的参数
    val runningAgents = remember(byMachine) {
        byMachine.values.flatten()
            .filter { !it.assistant && it.isLive }
            .sortedByDescending { it.updated ?: java.time.Instant.EPOCH }
    }
    var selectedTarget by remember(runningAgents) { mutableStateOf(runningAgents.firstOrNull()?.key) }

    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(JSpace.s)
    ) {
            // 模式分段选择
            Row(
                Modifier
                    .fillMaxWidth()
                    .padding(horizontal = JSpace.page),
                horizontalArrangement = Arrangement.spacedBy(JSpace.s)
            ) {
                HandOffMode.entries.forEach { m ->
                    ChoiceChip(
                        m.title,
                        mode == m,
                        onClick = { mode = m },
                        modifier = Modifier.weight(1f)
                    )
                }
            }

            if (mode == HandOffMode.New) {
                // 新开一个的选项卡片
                GroupCard(Modifier.padding(top = JSpace.s)) {
                    // 电脑选择
                    PickerRow(
                        title = "电脑",
                        value = selectedMachine?.displayName ?: "选择电脑",
                        onClick = { showMachinePicker = true }
                    ) {
                        DropdownMenu(showMachinePicker, onDismissRequest = { showMachinePicker = false }) {
                            machines.forEach { m ->
                                DropdownMenuItem(
                                    text = { Text(m.displayName + if (m.online) "" else "（离线）", maxLines = 1) },
                                    onClick = {
                                        selectedMachine = m
                                        showMachinePicker = false
                                    }
                                )
                            }
                        }
                    }
                    RowDivider()
                    // 目录选择
                    PickerRow(
                        title = "目录",
                        value = cwd.ifEmpty { "选择目录" },
                        onClick = { showDirPicker = true }
                    ) {
                        DropdownMenu(showDirPicker, onDismissRequest = { showDirPicker = false }) {
                            dirList?.recent?.forEach { r ->
                                DropdownMenuItem(
                                    text = { Text(r.name.ifBlank { r.path }, maxLines = 1) },
                                    onClick = {
                                        cwd = r.path
                                        showDirPicker = false
                                    }
                                )
                            }
                            dirList?.entries?.forEach { e ->
                                DropdownMenuItem(
                                    text = { Text(e.name, maxLines = 1) },
                                    onClick = {
                                        cwd = e.path
                                        showDirPicker = false
                                    }
                                )
                            }
                        }
                    }
                    RowDivider()
                    // 启动器
                    PickerRow(
                        title = "启动器",
                        value = launcher,
                        onClick = { showLauncherPicker = true }
                    ) {
                        DropdownMenu(showLauncherPicker, onDismissRequest = { showLauncherPicker = false }) {
                            listOf("claude", "codex").forEach { l ->
                                DropdownMenuItem(
                                    text = { Text(l, maxLines = 1) },
                                    onClick = {
                                        launcher = l
                                        showLauncherPicker = false
                                    }
                                )
                            }
                        }
                    }
                    RowDivider()
                    // 权限
                    PickerRow(
                        title = "权限",
                        value = if (permissionMode == "bypassPermissions") "允许一切" else "默认",
                        onClick = { showModePicker = true }
                    ) {
                        DropdownMenu(showModePicker, onDismissRequest = { showModePicker = false }) {
                            DropdownMenuItem(text = { Text("默认", maxLines = 1) }, onClick = { permissionMode = "default"; showModePicker = false })
                            DropdownMenuItem(text = { Text("允许一切 (bypassPermissions)", maxLines = 1) }, onClick = { permissionMode = "bypassPermissions"; showModePicker = false })
                        }
                    }
                }
            } else {
                // 交给已有的 Agent 列表
                SectionHeader("选择已有 Agent")
                if (runningAgents.isEmpty()) {
                    Text(
                        "当前没有正在运行的 Agent",
                        style = JTheme.type.secondary,
                        color = JTheme.colors.secondaryText,
                        modifier = Modifier.padding(horizontal = JSpace.page, vertical = JSpace.m)
                    )
                } else {
                    GroupCard {
                        runningAgents.forEachIndexed { i, s ->
                            if (i > 0) RowDivider()
                            ExistingAgentRow(
                                session = s,
                                machineName = machines.firstOrNull { it.id == s.machineId }?.displayName ?: s.machineId,
                                selected = selectedTarget == s.key,
                                onClick = { selectedTarget = s.key }
                            )
                        }
                    }
                }
            }

            // 要它做什么输入框
            SectionHeader("要它做什么")
            Column(
                Modifier.padding(horizontal = JSpace.page),
                verticalArrangement = Arrangement.spacedBy(JSpace.s)
            ) {
                JTextField(
                    instruction,
                    { instruction = it },
                    placeholder = "把待办内容交给他做，可以说说重点或要求…",
                    minLines = 3,
                    maxLines = 6,
                    fill = JTheme.colors.card
                )
            }

            // 会一起发过去的待办卡片预览 (mockup 4-light.png)
            SectionHeader("会一起发过去")
            Box(
                Modifier
                    .fillMaxWidth()
                    .padding(horizontal = JSpace.page)
                    .clip(RoundedCornerShape(JRadius.card))
                    .background(JTheme.colors.card)
                    .padding(JSpace.m)
            ) {
                Row(
                    horizontalArrangement = Arrangement.spacedBy(JSpace.m),
                    verticalAlignment = Alignment.Top
                ) {
                    Box(
                        Modifier
                            .size(width = JSize.lineMedium * 2, height = JSize.avatar)
                            .background(JTheme.colors.accent, RoundedCornerShape(JSize.lineMedium))
                    )
                    Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                        Row(
                            horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Icon(JIcons.todo, null, Modifier.size(JSize.markS), tint = JTheme.colors.secondaryText)
                            Text(
                                "待办 · ${todo.title}",
                                style = JTheme.type.bodyStrong,
                                color = JTheme.colors.label,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis
                            )
                        }
                        if (todo.notes.isNotBlank()) {
                            Text(
                                todo.notes,
                                style = JTheme.type.secondary,
                                color = JTheme.colors.secondaryText,
                                maxLines = 3,
                                overflow = TextOverflow.Ellipsis
                            )
                        }
                    }
                }
            }

            // 底部操作按钮
            PillButton(
                if (starting) "正在启动…" else "交给 Agent",
                onClick = {
                    starting = true
                    val prompt = todoHandOffPrompt(todo.title, todo.notes, instruction)
                    scope.launch {
                        if (mode == HandOffMode.New) {
                            val m = selectedMachine ?: return@launch
                            runCatching {
                                val s0 = hub.api.start(
                                    machine = m.id,
                                    cwd = cwd.ifEmpty { "~" },
                                    launcher = launcher,
                                    nameHint = "待办 · ${todo.title}",
                                    mode = permissionMode.takeIf { it != "default" },
                                    todoId = todo.id
                                )
                                val s = s0.copy(machineId = m.id)
                                hub.adopt(s)
                                jarvis.android.chat.Outbox.send(hub, s.key, prompt)
                                onDismiss()
                                onStarted(s.key)
                            }.onFailure { e ->
                                starting = false
                                Toasts.show("启动失败：${e.message}", ToastStyle.ERROR)
                            }
                        } else {
                            val target = selectedTarget ?: return@launch
                            runCatching {
                                hub.api.send(
                                    k = target,
                                    text = prompt,
                                    clientMsgId = UUID.randomUUID().toString(),
                                    todoId = todo.id
                                )
                                onDismiss()
                                onStarted(target)
                            }.onFailure { e ->
                                starting = false
                                Toasts.show("发送失败：${e.message}", ToastStyle.ERROR)
                            }
                        }
                    }
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = JSpace.page, vertical = JSpace.m),
                kind = ButtonKind.Prominent,
                large = true,
                icon = if (starting) null else JIcons.handToAI,
                enabled = !starting && (mode == HandOffMode.New || selectedTarget != null)
            )
        }
}

@Composable
private fun PickerRow(
    title: String,
    value: String,
    onClick: () -> Unit,
    menu: @Composable () -> Unit,
) {
    Box {
        Row(
            Modifier
                .fillMaxWidth()
                .clickable(onClick = onClick)
                .padding(horizontal = JSpace.l, vertical = JSpace.m),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Text(title, style = JTheme.type.body, color = JTheme.colors.label, modifier = Modifier.weight(1f))
            Text(value, style = JTheme.type.secondary, color = JTheme.colors.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = JTheme.colors.tertiaryText)
        }
        menu()
    }
}

@Composable
private fun ExistingAgentRow(
    session: Session,
    machineName: String,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m)
    ) {
        Box(
            Modifier
                .size(JSize.iconS)
                .clip(CircleShape)
                .then(
                    if (selected) Modifier.background(JTheme.colors.accent)
                    else Modifier.border(JSize.lineMedium, JTheme.colors.tertiaryText, CircleShape)
                ),
            contentAlignment = Alignment.Center
        ) {
            if (selected) Icon(JIcons.done, null, Modifier.size(JSize.markS), tint = Color.White)
        }
        Column(Modifier.weight(1f)) {
            Text(session.displayTitle, style = JTheme.type.body, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                "$machineName · ${session.folderName}",
                style = JTheme.type.caption,
                color = JTheme.colors.secondaryText,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
        }
    }
}
