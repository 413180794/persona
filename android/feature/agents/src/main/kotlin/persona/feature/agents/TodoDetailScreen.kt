package persona.feature.agents

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import persona.core.contracts.TodoState
import persona.core.contracts.state
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.tasks.TasksStore
import java.time.Instant

@Composable
fun TodoDetailScreen(
    todoId: String,
    todos: List<Todo>,
    machines: List<Machine>,
    sessions: List<Session>,
    tasks: TasksStore?,
    onBack: () -> Unit,
    onOpenSession: (SessionKey) -> Unit,
    onHandOff: (Todo) -> Unit,
    onError: (String) -> Unit = {},
) {
    val todo = todos.firstOrNull { it.id == todoId }
    val scope = rememberCoroutineScope()

    if (todo == null) {
        JPage(title = "待办详情", onBack = onBack) {
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                JEmptyState("待办已不存在", "该待办可能已被删除。")
            }
        }
        return
    }

    var menuOpen by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    var editingTitle by remember { mutableStateOf(false) }
    var editingNotes by remember { mutableStateOf(false) }
    var titleText by remember(todo.title) { mutableStateOf(todo.title) }
    var notesText by remember(todo.notes) { mutableStateOf(todo.notes) }
    var pastFolded by rememberSaveable { mutableStateOf(true) }

    val sessionMap = remember(sessions) { sessions.associateBy { it.key } }
    val sessionLookup: (SessionKey) -> Session? = remember(sessionMap) { { key: SessionKey -> sessionMap[key] } }
    val state = todo.state(sessionLookup)

    if (confirmDelete) {
        JDialog(
            onDismiss = { confirmDelete = false },
            title = "删除待办",
            text = "确定要删除待办「${todo.title}」吗？关联的会话不会被关闭。",
            confirm = "删除" to {
                confirmDelete = false
                scope.launch {
                    try {
                        tasks?.deleteTodo(todo.id)
                        onBack()
                    } catch (e: Exception) {
                        onError("删除失败：${e.message}")
                    }
                }
            },
            destructive = true
        )
    }

    if (menuOpen) {
        JActionSheet(
            actions = listOf(
                JAction(
                    label = "删除待办",
                    icon = PhIcons.Trash,
                    destructive = true,
                    onClick = { confirmDelete = true }
                )
            ),
            onDismiss = { menuOpen = false },
            title = "待办操作"
        )
    }

    JPage(
        title = "待办详情",
        onBack = onBack,
        actions = {
            JIconButton(PhIcons.DotsThree, "更多", { menuOpen = true })
        },
        bottomBar = {
            JBottomBar {
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = 4.dp, vertical = 2.dp),
                    horizontalArrangement = Arrangement.spacedBy(8.dp)
                ) {
                    JButton(
                        text = if (todo.isDone) "重新打开" else "完成待办",
                        kind = JButtonKind.Secondary,
                        icon = if (todo.isDone) PhIcons.ArrowClockwise else PhIcons.Check,
                        onClick = {
                            scope.launch {
                                try {
                                    tasks?.setTodoDone(todo, !todo.isDone)
                                } catch (e: Exception) {
                                    onError("更新状态失败：${e.message}")
                                }
                            }
                        },
                        modifier = Modifier.weight(1f)
                    )
                    JButton(
                        text = "交给 Agent",
                        icon = PhIcons.Robot,
                        onClick = { onHandOff(todo) },
                        modifier = Modifier.weight(1f)
                    )
                }
            }
        }
    ) { padding ->
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(
                top = padding.calculateTopPadding() + 8.dp,
                bottom = padding.calculateBottomPadding() + 16.dp,
                start = 16.dp,
                end = 16.dp
            ),
            verticalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            // 待办标题与备注卡片
            item(key = "title-notes") {
                JCard(Modifier.fillMaxWidth()) {
                    Column(
                        Modifier.padding(Tokens.Space.m),
                        verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)
                    ) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            val status = when (state) {
                                TodoState.NeedsYou -> Status.NeedsYou
                                TodoState.Unread -> Status.Unread
                                TodoState.Working -> Status.Working
                                else -> Status.Idle
                            }
                            JStatusDot(status)
                            Spacer(Modifier.width(8.dp))
                            if (todo.isDone) {
                                if (todo.doneBy == "auto") {
                                    JTag("自动完成")
                                } else {
                                    JTag("已完成")
                                }
                            } else {
                                JTag(state.title)
                            }
                            Spacer(Modifier.weight(1f))
                            if (!editingTitle && !editingNotes) {
                                JIconButton(PhIcons.NotePencil, "编辑", {
                                    editingTitle = true
                                    editingNotes = true
                                })
                            }
                        }

                        if (editingTitle) {
                            JTextField(
                                titleText,
                                { titleText = it },
                                label = "待办标题",
                                singleLine = true
                            )
                        } else {
                            Text(
                                todo.title,
                                style = PersonaTheme.type.title,
                                color = PersonaTheme.palette.ink
                            )
                        }

                        if (editingNotes) {
                            JTextField(
                                notesText,
                                { notesText = it },
                                label = "说明与要求",
                                placeholder = "添加详细说明，交给 Agent 时会作为上下文一同发出…",
                                minLines = 3,
                                maxLines = 8
                            )
                            Row(
                                Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.End
                            ) {
                                JTextButton("取消", onClick = {
                                    titleText = todo.title
                                    notesText = todo.notes
                                    editingTitle = false
                                    editingNotes = false
                                })
                                Spacer(Modifier.width(8.dp))
                                JButton("保存", onClick = {
                                    editingTitle = false
                                    editingNotes = false
                                    scope.launch {
                                        try {
                                            tasks?.updateTodo(todo.id, title = titleText.trim(), notes = notesText.trim())
                                        } catch (e: Exception) {
                                            onError("保存失败：${e.message}")
                                        }
                                    }
                                })
                            }
                        } else if (todo.notes.isNotBlank()) {
                            Text(
                                todo.notes,
                                style = PersonaTheme.type.body,
                                color = PersonaTheme.palette.ink2
                            )
                        } else {
                            Text(
                                "点击编辑添加任务说明与要求…",
                                style = PersonaTheme.type.caption,
                                color = PersonaTheme.palette.ink3,
                                modifier = Modifier.clickable {
                                    editingTitle = true
                                    editingNotes = true
                                }
                            )
                        }

                        if (todo.isDone && todo.doneBy == "auto" && !todo.doneReason.isNullOrBlank()) {
                            Text(
                                "自动完成理由：${todo.doneReason}",
                                style = PersonaTheme.type.caption,
                                color = PersonaTheme.palette.ink3
                            )
                        }
                    }
                }
            }

            // 正在做的 Agents
            item(key = "active-header") {
                JSectionHeader("正在做的 Agent", count = todo.currentAgents.size)
            }

            if (todo.currentAgents.isEmpty()) {
                item(key = "active-empty") {
                    JEmptyState("暂无分配的 Agent", "点击底部「交给 Agent」指派任务。")
                }
            } else {
                itemsIndexed(todo.currentAgents, key = { _, a -> a.key.path }) { index, agent ->
                    val session = sessionLookup(agent.key)
                    val machineName = machines.firstOrNull { it.id == agent.machineId }?.displayName ?: agent.machineId
                    val title = session?.displayTitle ?: agent.title?.takeIf { it.isNotBlank() } ?: "会话 ${agent.sessionId.take(8)}"
                    val subtitle = "$machineName · ${session?.folderName ?: agent.cwd?.substringAfterLast('/').orEmpty()}"
                    val preview = session?.lastText ?: if (session == null) "（不在当前活跃列表）" else null
                    val status = session?.let {
                        Status.entries.firstOrNull { s -> s.name == it.agentState.name }
                    } ?: Status.Idle

                    JGroupItem(GroupPosition.of(index, todo.currentAgents.size)) {
                        Column {
                            JRow(
                                title = title,
                                subtitle = preview ?: subtitle,
                                leading = { JStatusDot(status) },
                                trailing = {
                                    JIconButton(PhIcons.X, "移出", {
                                        scope.launch {
                                            try {
                                                tasks?.unlinkTodoAgent(todo.id, agent.machineId, agent.sessionId)
                                            } catch (e: Exception) {
                                                onError("移出失败：${e.message}")
                                            }
                                        }
                                    })
                                },
                                chevron = session != null,
                                onClick = { onOpenSession(agent.key) }
                            )
                            if (!agent.startError.isNullOrBlank()) {
                                Row(
                                    Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp),
                                    verticalAlignment = Alignment.CenterVertically
                                ) {
                                    Text(
                                        "启动失败：${agent.startError}",
                                        style = PersonaTheme.type.caption,
                                        color = PersonaTheme.palette.needsYou.text,
                                        modifier = Modifier.weight(1f)
                                    )
                                    JTextButton("移除", onClick = {
                                        scope.launch {
                                            try {
                                                tasks?.unlinkTodoAgent(todo.id, agent.machineId, agent.sessionId)
                                            } catch (e: Exception) {
                                                onError("移除失败：${e.message}")
                                            }
                                        }
                                    })
                                }
                            }
                        }
                    }
                }
            }

            // 参与过的 Agents 折叠区
            if (todo.releasedAgents.isNotEmpty()) {
                item(key = "past-header") {
                    Row(
                        Modifier.fillMaxWidth().clickable { pastFolded = !pastFolded }.padding(vertical = 4.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        JSectionHeader(
                            "参与过的 Agent",
                            count = todo.releasedAgents.size,
                            modifier = Modifier.weight(1f)
                        )
                        JIcon(if (pastFolded) PhIcons.CaretDown else PhIcons.CaretRight, null, size = 16.dp, tint = PersonaTheme.palette.ink3)
                    }
                }

                if (!pastFolded) {
                    itemsIndexed(todo.releasedAgents, key = { _, a -> "rel:${a.key.path}" }) { index, agent ->
                        val session = sessionLookup(agent.key)
                        val title = session?.displayTitle ?: agent.title?.takeIf { it.isNotBlank() } ?: "会话 ${agent.sessionId.take(8)}"
                        val subtitle = "已解除关联 · ${agent.machineId}"
                        JGroupItem(GroupPosition.of(index, todo.releasedAgents.size)) {
                            JRow(
                                title = title,
                                subtitle = subtitle,
                                leading = { JStatusDot(Status.Idle) },
                                chevron = session != null,
                                onClick = { onOpenSession(agent.key) }
                            )
                        }
                    }
                }
            }
        }
    }
}
