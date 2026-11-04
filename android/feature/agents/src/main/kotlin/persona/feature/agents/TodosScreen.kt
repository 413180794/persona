package persona.feature.agents

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import persona.core.contracts.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.tasks.TasksStore
import java.time.Instant

data class TodoMostUrgent(
    val agent: TodoAgent,
    val session: Session?,
    val status: AgentState,
    val isFailed: Boolean,
)

fun mostUrgentAgent(todo: Todo, sessionLookup: (SessionKey) -> Session?): TodoMostUrgent? {
    val active = todo.currentAgents
    if (active.isEmpty()) return null

    fun score(st: AgentState, isFailed: Boolean): Int = when {
        isFailed -> 1
        st == AgentState.NeedsYou -> 4
        st == AgentState.Unread -> 3
        st == AgentState.Working -> 2
        else -> 0
    }

    var best: TodoMostUrgent? = null
    var bestScore = -1

    for (a in active) {
        val isFailed = !a.startError.isNullOrBlank()
        val s = sessionLookup(a.key)
        val st = if (isFailed) AgentState.Offline else (s?.agentState ?: AgentState.Offline)
        val sc = score(st, isFailed)
        if (sc > bestScore || best == null) {
            bestScore = sc
            best = TodoMostUrgent(a, s, st, isFailed)
        }
    }
    return best
}

@Composable
fun TodosScreen(
    todos: List<Todo>,
    sessions: List<Session>,
    tasks: TasksStore?,
    onBack: (() -> Unit)?,
    onOpenTodo: (String) -> Unit,
    now: Instant = Instant.now(),
    onError: (String) -> Unit = {},
) {
    var query by rememberSaveable { mutableStateOf("") }
    var creating by remember { mutableStateOf(false) }
    var completedFolded by rememberSaveable { mutableStateOf(true) }
    var newTitle by remember { mutableStateOf("") }
    var newNotes by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()

    val sessionMap = remember(sessions) { sessions.associateBy { it.key } }
    val sessionLookup: (SessionKey) -> Session? = remember(sessionMap) { { key: SessionKey -> sessionMap[key] } }

    val filtered = remember(todos, query) {
        if (query.isBlank()) todos
        else {
            val q = query.trim()
            todos.filter { it.title.contains(q, ignoreCase = true) || it.notes.contains(q, ignoreCase = true) }
        }
    }

    val uncompleted = remember(filtered, sessions) {
        filtered.filter { !it.isDone }.ordered(sessionLookup)
    }
    val completed = remember(filtered) {
        filtered.filter { it.isDone }.sortedWith(
            compareByDescending<Todo> { it.done ?: it.created ?: Instant.EPOCH }.thenBy { it.id }
        )
    }

    if (creating) {
        JSheet(
            onDismiss = {
                creating = false
                newTitle = ""
                newNotes = ""
            },
            title = "新建待办"
        ) {
            Column(
                Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp)
            ) {
                JTextField(
                    newTitle,
                    { newTitle = it },
                    label = "待办标题",
                    placeholder = "一句话描述要做的事…",
                    singleLine = true
                )
                JTextField(
                    newNotes,
                    { newNotes = it },
                    label = "说明与要求（可选）",
                    placeholder = "详细说明要求与背景，交给 Agent 时一同发出…",
                    minLines = 3,
                    maxLines = 6
                )
                Spacer(Modifier.height(8.dp))
                JButton(
                    text = "创建待办",
                    onClick = {
                        val title = newTitle.trim()
                        val notes = newNotes.trim()
                        if (title.isNotBlank()) {
                            creating = false
                            newTitle = ""
                            newNotes = ""
                            scope.launch {
                                try {
                                    tasks?.createTodo(title, notes)
                                } catch (e: Exception) {
                                    onError("创建待办失败：${e.message}")
                                }
                            }
                        }
                    },
                    enabled = newTitle.isNotBlank(),
                    modifier = Modifier.fillMaxWidth()
                )
            }
        }
    }

    JPage(
        title = "待办",
        onBack = onBack,
        actions = {
            JIconButton(PhIcons.Plus, "新建待办", { creating = true })
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
            verticalArrangement = Arrangement.spacedBy(10.dp)
        ) {
            item(key = "search") {
                JSearchField(query, { query = it }, placeholder = "搜索待办")
            }

            if (uncompleted.isEmpty() && completed.isEmpty()) {
                item(key = "empty") {
                    JEmptyState(
                        title = if (query.isBlank()) "暂无待办" else "未找到相关待办",
                        description = if (query.isBlank()) "记下一件要做的事，把它交给 Agent 去做。" else "换个关键词搜索试试。",
                        icon = PhIcons.ListChecks
                    )
                }
            } else {
                if (uncompleted.isNotEmpty()) {
                    item(key = "uncompleted-header") {
                        JSectionHeader("未完成", count = uncompleted.size)
                    }

                    itemsIndexed(uncompleted, key = { _, todo -> todo.id }) { index, todo ->
                        val state = todo.state(sessionLookup)
                        val urgent = mostUrgentAgent(todo, sessionLookup)
                        val status = when (state) {
                            TodoState.NeedsYou -> Status.NeedsYou
                            TodoState.Unread -> Status.Unread
                            TodoState.Working -> Status.Working
                            else -> Status.Idle
                        }

                        JSwipeActions(
                            start = SwipeAction("完成", PhIcons.Check, Status.Working, {
                                scope.launch {
                                    try {
                                        tasks?.setTodoDone(todo, true)
                                    } catch (e: Exception) {
                                        onError("完成待办失败：${e.message}")
                                    }
                                }
                            }),
                            end = SwipeAction("删除", PhIcons.Trash, Status.NeedsYou, {
                                scope.launch {
                                    try {
                                        tasks?.deleteTodo(todo.id)
                                    } catch (e: Exception) {
                                        onError("删除待办失败：${e.message}")
                                    }
                                }
                            }, destructive = true)
                        ) {
                            JCard(
                                modifier = Modifier.fillMaxWidth(),
                                onClick = { onOpenTodo(todo.id) }
                            ) {
                                Column(
                                    Modifier.padding(Tokens.Space.m),
                                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)
                                ) {
                                    Row(
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)
                                    ) {
                                        Box(
                                            Modifier
                                                .size(24.dp)
                                                .clip(CircleShape)
                                                .clickable {
                                                    scope.launch {
                                                        try {
                                                            tasks?.setTodoDone(todo, true)
                                                        } catch (e: Exception) {
                                                            onError("完成待办失败：${e.message}")
                                                        }
                                                    }
                                                },
                                            contentAlignment = Alignment.Center
                                        ) {
                                            Box(
                                                Modifier
                                                    .size(18.dp)
                                                    .border(1.5.dp, PersonaTheme.palette.ink3, CircleShape)
                                            )
                                        }
                                        Text(
                                            todo.title,
                                            style = PersonaTheme.type.headline,
                                            color = PersonaTheme.palette.ink,
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                            modifier = Modifier.weight(1f)
                                        )
                                        JTag(state.title)
                                    }

                                    // 最要紧 Agent 动态
                                    if (urgent != null) {
                                        val agentStatus = when (urgent.status) {
                                            AgentState.NeedsYou -> Status.NeedsYou
                                            AgentState.Unread -> Status.Unread
                                            AgentState.Working -> Status.Working
                                            else -> Status.Idle
                                        }
                                        val text = urgent.session?.lastText?.takeIf { it.isNotBlank() }
                                            ?: urgent.agent.title?.takeIf { it.isNotBlank() }
                                            ?: if (urgent.isFailed) "启动失败：${urgent.agent.startError}" else "正在进行"
                                        Row(
                                            verticalAlignment = Alignment.CenterVertically,
                                            horizontalArrangement = Arrangement.spacedBy(6.dp),
                                            modifier = Modifier.padding(start = 32.dp)
                                        ) {
                                            JStatusDot(agentStatus)
                                            Text(
                                                text,
                                                style = PersonaTheme.type.caption,
                                                color = PersonaTheme.palette.ink2,
                                                maxLines = 1,
                                                overflow = TextOverflow.Ellipsis,
                                                modifier = Modifier.weight(1f)
                                            )
                                        }
                                    }

                                    // 底部相对时间与挂靠 Agent 数量
                                    Row(
                                        Modifier.fillMaxWidth().padding(start = 32.dp, top = 2.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(8.dp)
                                    ) {
                                        val timeStr = relativeTime(todo.updated ?: todo.created, now)
                                        if (timeStr.isNotBlank()) {
                                            Text(timeStr, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                                        }
                                        Spacer(Modifier.weight(1f))
                                        if (todo.currentAgents.isNotEmpty()) {
                                            Text("${todo.currentAgents.size} 个 Agent", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                // 底部可折叠已完成分组
                if (completed.isNotEmpty()) {
                    item(key = "completed-header") {
                        Row(
                            Modifier.fillMaxWidth().clickable { completedFolded = !completedFolded }.padding(vertical = 4.dp),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            JSectionHeader(
                                "已完成",
                                count = completed.size,
                                modifier = Modifier.weight(1f)
                            )
                            JIcon(if (completedFolded) PhIcons.CaretDown else PhIcons.CaretUp, null, size = 16.dp, tint = PersonaTheme.palette.ink3)
                        }
                    }

                    if (!completedFolded) {
                        itemsIndexed(completed, key = { _, todo -> "done:${todo.id}" }) { _, todo ->
                            JCard(
                                modifier = Modifier.fillMaxWidth(),
                                onClick = { onOpenTodo(todo.id) }
                            ) {
                                Row(
                                    Modifier.padding(Tokens.Space.m),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)
                                ) {
                                    Box(
                                        Modifier
                                            .size(24.dp)
                                            .clip(CircleShape)
                                            .clickable {
                                                scope.launch {
                                                    try {
                                                        tasks?.setTodoDone(todo, false)
                                                    } catch (e: Exception) {
                                                        onError("重新打开待办失败：${e.message}")
                                                    }
                                                }
                                            },
                                        contentAlignment = Alignment.Center
                                    ) {
                                        JIcon(PhIcons.CheckCircle, null, size = 18.dp, selected = true, tint = PersonaTheme.palette.done.dot)
                                    }
                                    Text(
                                        todo.title,
                                        style = PersonaTheme.type.body.copy(textDecoration = TextDecoration.LineThrough),
                                        color = PersonaTheme.palette.ink3,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                        modifier = Modifier.weight(1f)
                                    )
                                    if (todo.doneBy == "auto") {
                                        JTag("自动完成")
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
