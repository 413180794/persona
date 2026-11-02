package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.data.AgentState
import jarvis.android.data.Hub
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.data.Todo
import jarvis.android.data.TodoAgent
import jarvis.android.data.TodoState
import jarvis.android.data.state
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.DOT_SEP
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.design.StatusDot
import kotlinx.coroutines.launch
import java.time.format.DateTimeFormatter

/**
 * 待办详情页面 (umbrella.final.md, mockup 3-light.png).
 * 标题与说明可直接改，展示状态、当前 Agents（状态点、标题、电脑·空间›tab、最后一句、时间）、
 * 超出时间窗标已结束、启动失败可移除、收起的参与过、底部「交给 Agent」和「完成 / 重新打开」、⋯ 菜单删除。
 */
@Composable
fun TodoDetailScreen(
    hub: Hub,
    todoId: String,
    onBack: () -> Unit,
    onOpenSession: (SessionKey) -> Unit,
) {
    val todos by hub.todos.collectAsState()
    val machines by hub.machines.collectAsState()
    val interactions by hub.interactions.collectAsState()
    val todo = todos.firstOrNull { it.id == todoId }
    val scope = rememberCoroutineScope()

    var menuOpen by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    var editingTitle by remember { mutableStateOf(false) }
    var editingNotes by remember { mutableStateOf(false) }
    var handingOff by remember { mutableStateOf(false) }
    var releasedFolded by rememberSaveable { mutableStateOf(true) }

    if (todo == null) {
        Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
            PageHeader(title = "待办详情", onBack = onBack)
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Text("待办已不存在", style = JTheme.type.body, color = JTheme.colors.secondaryText)
            }
        }
        return
    }

    val state = todo.state { k -> hub.session(k) }
    val currentAgents = todo.currentAgents
    val releasedAgents = todo.releasedAgents

    val stateTone = JTheme.tone(when (state) {
        TodoState.NeedsYou -> AgentState.NeedsYou
        TodoState.Unread -> AgentState.Unread
        TodoState.Working -> AgentState.Working
        else -> AgentState.Idle
    })

    Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
        PageHeader(
            title = "",
            onBack = onBack,
            actions = {
                Box {
                    CircleButton(JIcons.more, "更多", { menuOpen = true })
                    DropdownMenu(menuOpen, onDismissRequest = { menuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text("编辑标题", maxLines = 1) },
                            onClick = { menuOpen = false; editingTitle = true },
                            leadingIcon = { Icon(JIcons.edit, null) }
                        )
                        DropdownMenuItem(
                            text = { Text("编辑说明", maxLines = 1) },
                            onClick = { menuOpen = false; editingNotes = true },
                            leadingIcon = { Icon(JIcons.compose, null) }
                        )
                        RowDivider()
                        DropdownMenuItem(
                            text = { Text("删除待办", color = JTheme.colors.danger, maxLines = 1) },
                            onClick = { menuOpen = false; confirmDelete = true },
                            leadingIcon = { Icon(JIcons.delete, null, tint = JTheme.colors.danger) }
                        )
                    }
                }
            }
        )

        Column(
            Modifier
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(bottom = JSpace.l)
        ) {
            // 大标题与状态信息
            Column(
                Modifier
                    .fillMaxWidth()
                    .padding(horizontal = JSpace.page, vertical = JSpace.s),
                verticalArrangement = Arrangement.spacedBy(JSpace.xs)
            ) {
                Text(
                    todo.title,
                    style = JTheme.type.display,
                    color = JTheme.colors.label,
                    modifier = Modifier.clickable { editingTitle = true }
                )
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(JSpace.s)
                ) {
                    Chip(state.title, look = ChipLook.Tint(stateTone.tint, stateTone.text))
                    val createdText = todo.created?.let {
                        DateTimeFormatter.ofPattern("M月d日 HH:mm").withZone(java.time.ZoneId.systemDefault()).format(it) + " 创建"
                    } ?: ""
                    Text(
                        listOfNotNull("${currentAgents.size} 个 Agent", createdText.takeIf { it.isNotEmpty() }).joinToString(DOT_SEP),
                        style = JTheme.type.caption,
                        color = JTheme.colors.secondaryText
                    )
                }
            }

            // 说明卡片
            SectionHeader("说明", trailing = {
                Text(
                    "编辑",
                    style = JTheme.type.labelStrong,
                    color = JTheme.colors.accent,
                    modifier = Modifier.clickable { editingNotes = true }
                )
            })
            GroupCard(Modifier.padding(horizontal = JSpace.page)) {
                Column(Modifier.padding(JSpace.l)) {
                    Text(
                        if (todo.notes.isNotBlank()) todo.notes else "暂无说明，点击右上角添加说明",
                        style = JTheme.type.body,
                        color = if (todo.notes.isNotBlank()) JTheme.colors.label else JTheme.colors.secondaryText
                    )
                }
            }

            // 当前挂着的 Agents
            SectionHeader("Agents", count = currentAgents.size)
            if (currentAgents.isEmpty()) {
                GroupCard(Modifier.padding(horizontal = JSpace.page)) {
                    Text(
                        "还没有 Agent 在为这个待办干活",
                        style = JTheme.type.secondary,
                        color = JTheme.colors.secondaryText,
                        modifier = Modifier.padding(horizontal = JSpace.l, vertical = JSpace.m)
                    )
                }
            } else {
                GroupCard(Modifier.padding(horizontal = JSpace.page)) {
                    currentAgents.forEachIndexed { i, a ->
                        if (i > 0) RowDivider()
                        val s = hub.session(a.key)
                        val m = machines.firstOrNull { it.id == a.machineId }
                        val ix = interactions.firstOrNull { it.machineId == a.machineId && it.sessionId == a.sessionId }
                        TodoAgentRow(
                            agent = a,
                            session = s,
                            machineName = m?.displayName ?: a.machineId,
                            interaction = ix,
                            onClick = { onOpenSession(a.key) },
                            onRemove = {
                                scope.launch {
                                    runCatching { hub.unlinkTodoAgent(todo.id, a.key) }
                                        .onFailure { Toasts.show("移除失败：${it.message}", ToastStyle.ERROR) }
                                }
                            }
                        )
                    }
                }
            }

            // 参与过的 Agents（折叠区）
            if (releasedAgents.isNotEmpty()) {
                Spacer(Modifier.height(JSpace.m))
                GroupCard(Modifier.padding(horizontal = JSpace.page)) {
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable { releasedFolded = !releasedFolded }
                            .padding(horizontal = JSpace.l, vertical = JSpace.m),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Text(
                            "参与过 · ${releasedAgents.size}",
                            style = JTheme.type.body,
                            color = JTheme.colors.secondaryText,
                            modifier = Modifier.weight(1f)
                        )
                        Icon(
                            if (releasedFolded) JIcons.disclosure else JIcons.expand,
                            null,
                            Modifier.size(JSize.markM),
                            tint = JTheme.colors.tertiaryText
                        )
                    }
                    if (!releasedFolded) {
                        releasedAgents.forEachIndexed { i, a ->
                            RowDivider()
                            val s = hub.session(a.key)
                            val m = machines.firstOrNull { it.id == a.machineId }
                            val ix = interactions.firstOrNull { it.machineId == a.machineId && it.sessionId == a.sessionId }
                            TodoAgentRow(
                                agent = a,
                                session = s,
                                machineName = m?.displayName ?: a.machineId,
                                interaction = ix,
                                onClick = { onOpenSession(a.key) },
                                isReleased = true
                            )
                        }
                    }
                }
            }
        }

        // 底部固定的操作栏
        Row(
            Modifier
                .fillMaxWidth()
                .background(JTheme.colors.page)
                .navigationBarsPadding()
                .padding(horizontal = JSpace.page, vertical = JSpace.s),
            horizontalArrangement = Arrangement.spacedBy(JSpace.m)
        ) {
            PillButton(
                "交给 Agent",
                onClick = { handingOff = true },
                modifier = Modifier.weight(1f),
                kind = ButtonKind.Prominent,
                icon = JIcons.handToAI
            )
            PillButton(
                if (todo.isDone) "重新打开" else "完成",
                onClick = {
                    scope.launch {
                        runCatching { hub.setTodoDone(todo, !todo.isDone) }
                            .onFailure { Toasts.show("操作失败：${it.message}", ToastStyle.ERROR) }
                    }
                },
                modifier = Modifier.weight(1f),
                kind = ButtonKind.Neutral,
                icon = JIcons.done
            )
        }
    }

    if (handingOff) {
        TodoHandOffSheet(
            hub = hub,
            todo = todo,
            onDismiss = { handingOff = false },
            onStarted = { key ->
                handingOff = false
                onOpenSession(key)
            }
        )
    }

    if (editingTitle) {
        RenameDialog(
            title = "修改待办标题",
            current = todo.title,
            note = null,
            onDismiss = { editingTitle = false },
            onSave = { newTitle ->
                editingTitle = false
                scope.launch {
                    runCatching { hub.updateTodo(todo.id, title = newTitle.trim()) }
                        .onFailure { Toasts.show("修改失败：${it.message}", ToastStyle.ERROR) }
                }
            }
        )
    }

    if (editingNotes) {
        EditNotesDialog(
            notes = todo.notes,
            onDismiss = { editingNotes = false },
            onSave = { newNotes ->
                editingNotes = false
                scope.launch {
                    runCatching { hub.updateTodo(todo.id, notes = newNotes.trim()) }
                        .onFailure { Toasts.show("修改失败：${it.message}", ToastStyle.ERROR) }
                }
            }
        )
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("删除待办？") },
            text = { Text("删除待办「${todo.title}」，相关的 Agent 会话本身不会受到影响。") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    scope.launch {
                        runCatching {
                            hub.deleteTodo(todo.id)
                            onBack()
                        }.onFailure { Toasts.show("删除失败：${it.message}", ToastStyle.ERROR) }
                    }
                }) {
                    Text("删除", color = JTheme.colors.danger, maxLines = 1)
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmDelete = false }) {
                    Text("取消", maxLines = 1)
                }
            }
        )
    }
}

@Composable
private fun TodoAgentRow(
    agent: TodoAgent,
    session: Session?,
    machineName: String,
    interaction: jarvis.android.data.Interaction? = null,
    onClick: () -> Unit,
    onRemove: (() -> Unit)? = null,
    isReleased: Boolean = false,
) {
    val c = JTheme.colors
    val state = session?.agentState ?: AgentState.Idle
    val title = session?.displayTitle ?: agent.title.ifEmpty { "会话" }
    val tab = session?.slotName?.takeIf { it.isNotBlank() && it != session.slotIndex?.toString() }
    val spaceName = session?.spaceName?.takeIf { it.isNotBlank() } ?: agent.cwd.trimEnd('/').substringAfterLast('/').ifEmpty { null }
    val spaceAndTab = listOfNotNull(spaceName, tab).joinToString(" › ")
    val place = listOfNotNull(
        machineName.takeIf { it.isNotBlank() },
        spaceAndTab.takeIf { it.isNotBlank() }
    ).joinToString(" · ")

    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = JSpace.l, vertical = JSpace.m),
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
        verticalAlignment = Alignment.CenterVertically
    ) {
        StatusDot(state)

        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(JSpace.s),
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    title,
                    style = JTheme.type.heading,
                    color = c.label,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f)
                )
                if (isReleased) {
                    Text("参与过", style = JTheme.type.caption, color = c.tertiaryText)
                } else if (session == null || !session.isLive) {
                    Text("已结束", style = JTheme.type.caption, color = c.tertiaryText)
                } else {
                    Text(listTime(session.updated), style = JTheme.type.caption, color = c.tertiaryText)
                }
            }

            if (place.isNotEmpty()) {
                Text(place, style = JTheme.type.caption, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }

            val lastLine = session?.lastText?.replace('\n', ' ')
            if (!lastLine.isNullOrBlank()) {
                val isNeedsYou = state == AgentState.NeedsYou
                val displayText = when {
                    isNeedsYou && (interaction?.kind == "plan" || interaction?.plan != null) -> {
                        val planTitle = interaction?.plan?.lines()?.firstOrNull { it.isNotBlank() }?.trim('#', '*', ' ')
                        "计划待批准：${planTitle ?: lastLine}"
                    }
                    isNeedsYou -> {
                        val clean = lastLine.removePrefix(title).trim().removePrefix("在问你：").removePrefix("Claude 在问你：").trim()
                        "在问你：$clean"
                    }
                    else -> lastLine
                }
                Text(
                    displayText,
                    style = JTheme.type.secondary,
                    color = if (isNeedsYou) JTheme.needsYou.text else c.secondaryText,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis
                )
            }

            if (!agent.startError.isNullOrBlank()) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(JSpace.s)
                ) {
                    Text(
                        "启动失败：${agent.startError}",
                        style = JTheme.type.caption,
                        color = JTheme.colors.danger,
                        modifier = Modifier.weight(1f),
                        maxLines = 2
                    )
                    if (onRemove != null) {
                        Text(
                            "移除",
                            style = JTheme.type.captionStrong,
                            color = JTheme.colors.accent,
                            modifier = Modifier.clickable(onClick = onRemove)
                        )
                    }
                }
            }
        }

        Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = c.tertiaryText)
    }
}

@Composable
private fun EditNotesDialog(
    notes: String,
    onDismiss: () -> Unit,
    onSave: (String) -> Unit,
) {
    var text by remember { mutableStateOf(notes) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("编辑说明") },
        text = {
            JTextField(
                text,
                { text = it },
                placeholder = "说明内容…",
                minLines = 4,
                maxLines = 8,
                fill = JTheme.colors.card
            )
        },
        confirmButton = {
            TextButton(onClick = { onSave(text) }) {
                Text("保存", maxLines = 1)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text("取消", maxLines = 1)
            }
        }
    )
}
