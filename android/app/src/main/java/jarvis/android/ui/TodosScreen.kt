package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberSwipeToDismissBoxState
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.JarvisApp
import jarvis.android.data.AgentState
import jarvis.android.data.Hub
import jarvis.android.data.Session
import jarvis.android.data.Todo
import jarvis.android.data.TodoAgent
import jarvis.android.data.TodoState
import jarvis.android.data.ordered
import jarvis.android.data.state
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.DOT_SEP
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SearchField
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.design.StatusDot
import kotlinx.coroutines.launch

/**
 * 待办列表页面 (umbrella.final.md, mockup 2-light.png).
 * 包含未完成列表（按状态和时间排序）、完成圆圈、第三行展示最要紧 Agent 动态、底部已完成折叠区与新建功能。
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TodosScreen(
    hub: Hub,
    onBack: (() -> Unit)?,
    onOpenTodo: (String) -> Unit,
) {
    val todos by hub.todos.collectAsState()
    var query by rememberSaveable { mutableStateOf("") }
    var creating by remember { mutableStateOf(false) }
    var completedFolded by rememberSaveable { mutableStateOf(true) }
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    val sessionLookup: (jarvis.android.data.SessionKey) -> Session? = { hub.session(it) }

    // 搜索过滤
    val filtered = remember(todos, query) {
        if (query.isBlank()) todos
        else {
            val q = query.trim()
            todos.filter { it.title.contains(q, ignoreCase = true) || it.notes.contains(q, ignoreCase = true) }
        }
    }

    val uncompleted = remember(filtered, todos) {
        filtered.filter { !it.isDone }.ordered(sessionLookup)
    }
    val completed = remember(filtered, todos) {
        filtered.filter { it.isDone }.sortedByDescending { it.done ?: it.created ?: java.time.Instant.EPOCH }
    }

    Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
        PageHeader(
            title = "待办",
            onBack = onBack,
            actions = {
                CircleButton(JIcons.add, "新建待办", { creating = true })
            }
        )

        Column(Modifier.fillMaxWidth().padding(horizontal = JSpace.page, vertical = JSpace.s)) {
            SearchField(query, { query = it }, "搜索待办")
        }

        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                scope.launch {
                    refreshing = true
                    runCatching { hub.loadTodos() }
                    refreshing = false
                }
            },
            modifier = Modifier.weight(1f)
        ) {
            LazyColumn(Modifier.fillMaxSize()) {
                if (uncompleted.isEmpty() && completed.isEmpty()) {
                    item(key = "empty") {
                        if (query.isNotBlank()) {
                            EmptyState(JIcons.search, "没有找到待办", "没有与「$query」匹配的待办事项。")
                        } else {
                            EmptyState(JIcons.todo, "还没有待办", "点右上角新建一个待办，把它交给 Agent 去做。")
                        }
                    }
                } else {
                    if (uncompleted.isNotEmpty()) {
                        item(key = "h-uncompleted") {
                            SectionHeader("没完成", count = uncompleted.size)
                        }
                        item(key = "card-uncompleted") {
                            GroupCard {
                                uncompleted.forEachIndexed { i, todo ->
                                    if (i > 0) RowDivider()
                                    SwipeableTodoRow(
                                        todo = todo,
                                        hub = hub,
                                        onClick = { onOpenTodo(todo.id) },
                                        onToggleDone = {
                                            scope.launch {
                                                runCatching { hub.setTodoDone(todo, true) }
                                                    .onFailure { Toasts.show("操作失败：${it.message}", ToastStyle.ERROR) }
                                            }
                                        }
                                    )
                                }
                            }
                        }
                    }

                    if (completed.isNotEmpty()) {
                        item(key = "h-completed-spacer") {
                            Spacer(Modifier.height(JSpace.l))
                        }
                        item(key = "card-completed") {
                            GroupCard {
                                Row(
                                    Modifier
                                        .fillMaxWidth()
                                        .clickable { completedFolded = !completedFolded }
                                        .padding(horizontal = JSpace.l, vertical = JSpace.m),
                                    verticalAlignment = Alignment.CenterVertically
                                ) {
                                    Text(
                                        "已完成 · ${completed.size}",
                                        style = JTheme.type.body,
                                        color = JTheme.colors.secondaryText,
                                        modifier = Modifier.weight(1f)
                                    )
                                    Icon(
                                        if (completedFolded) JIcons.disclosure else JIcons.expand,
                                        null,
                                        Modifier.size(JSize.markM),
                                        tint = JTheme.colors.tertiaryText
                                    )
                                }
                                if (!completedFolded) {
                                    completed.forEachIndexed { i, todo ->
                                        RowDivider()
                                        CompletedTodoRow(
                                            todo = todo,
                                            onClick = { onOpenTodo(todo.id) },
                                            onToggleDone = {
                                                scope.launch {
                                                    runCatching { hub.setTodoDone(todo, false) }
                                                        .onFailure { Toasts.show("操作失败：${it.message}", ToastStyle.ERROR) }
                                                }
                                            }
                                        )
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    if (creating) {
        NewTodoSheet(
            hub = hub,
            onDismiss = { creating = false },
            onCreated = { newTodo ->
                creating = false
                onOpenTodo(newTodo.id)
            }
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SwipeableTodoRow(
    todo: Todo,
    hub: Hub,
    onClick: () -> Unit,
    onToggleDone: () -> Unit,
) {
    val state = rememberSwipeToDismissBoxState(confirmValueChange = { v ->
        if (v == SwipeToDismissBoxValue.EndToStart) {
            onToggleDone()
        }
        false
    })
    SwipeToDismissBox(
        state = state,
        enableDismissFromStartToEnd = false,
        backgroundContent = {
            Row(
                Modifier
                    .fillMaxSize()
                    .background(JTheme.colors.positive)
                    .padding(end = JSpace.xl),
                horizontalArrangement = Arrangement.spacedBy(JSpace.sm, Alignment.End),
                verticalAlignment = Alignment.CenterVertically
            ) {
                Icon(JIcons.done, null, Modifier.size(JSize.iconXS), tint = Color.White)
                Text("完成", style = JTheme.type.bodyStrong, color = Color.White, maxLines = 1)
            }
        }
    ) {
        Box(Modifier.background(JTheme.colors.card)) {
            TodoRow(todo, hub, onClick = onClick, onToggleDone = onToggleDone)
        }
    }
}

@Composable
private fun TodoRow(
    todo: Todo,
    hub: Hub,
    onClick: () -> Unit,
    onToggleDone: () -> Unit,
) {
    val c = JTheme.colors
    val todoState = todo.state { k -> hub.session(k) }
    val currentAgents = todo.currentAgents
    val sessions = currentAgents.mapNotNull { hub.session(it.key) }
    val interactions by hub.interactions.collectAsState()

    // 找到最要紧的那个 Agent
    val mostUrgentSession = remember(sessions) {
        sessions.minByOrNull { it.agentState.priority }
    }
    val mostUrgentAgent = remember(currentAgents, mostUrgentSession) {
        if (mostUrgentSession != null) {
            currentAgents.firstOrNull { it.key == mostUrgentSession.key }
        } else {
            currentAgents.firstOrNull()
        }
    }
    val ix = remember(interactions, mostUrgentSession) {
        interactions.firstOrNull { it.machineId == mostUrgentSession?.machineId && it.sessionId == mostUrgentSession?.sessionId }
    }
    val isPlan = ix?.kind == "plan" || ix?.plan != null || mostUrgentSession?.lastText?.contains("计划") == true

    val stateTone = JTheme.tone(when (todoState) {
        TodoState.NeedsYou -> AgentState.NeedsYou
        TodoState.Unread -> AgentState.Unread
        TodoState.Working -> AgentState.Working
        else -> AgentState.Idle
    })

    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = JSpace.l, vertical = JSpace.m),
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
        verticalAlignment = Alignment.Top
    ) {
        // 完成圆圈按钮
        Box(
            Modifier
                .padding(top = JSpace.xxs)
                .size(JSize.iconS)
                .clip(CircleShape)
                .border(JSize.lineMedium, c.tertiaryText, CircleShape)
                .clickable(onClick = onToggleDone)
        )

        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            // 第一行：标题与时间
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(JSpace.s),
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    todo.title,
                    style = JTheme.type.heading,
                    color = c.label,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f)
                )
                Text(listTime(todo.created), style = JTheme.type.caption, color = c.tertiaryText, maxLines = 1)
            }

            // 第二行：状态点 · 状态文案 · 挂着的 Agents 信息
            Row(
                Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(JSpace.xs)
            ) {
                Box(Modifier.size(JSize.dot), contentAlignment = Alignment.Center) {
                    Box(Modifier.size(JSize.dotS).clip(CircleShape).background(stateTone.dot))
                }
                Text(
                    todoState.title,
                    style = JTheme.type.caption,
                    color = if (todoState == TodoState.NeedsYou) JTheme.needsYou.text else c.secondaryText,
                    maxLines = 1
                )
                if (todoState == TodoState.NeedsYou && isPlan) {
                    Text(DOT_SEP, style = JTheme.type.caption, color = c.tertiaryText)
                    Text("计划等你审批", style = JTheme.type.caption, color = c.secondaryText, maxLines = 1)
                } else if (currentAgents.isNotEmpty()) {
                    Text(DOT_SEP, style = JTheme.type.caption, color = c.tertiaryText)
                    Text(
                        "${currentAgents.size} 个 Agent",
                        style = JTheme.type.caption,
                        color = c.secondaryText,
                        maxLines = 1
                    )
                    // 紧凑展示各个 Agent 的状态点
                    Row(
                        horizontalArrangement = Arrangement.spacedBy(JSpace.xxs),
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier.padding(start = JSpace.xxs)
                    ) {
                        currentAgents.take(4).forEach { a ->
                            val s = hub.session(a.key)
                            StatusDot(s?.agentState ?: AgentState.Idle, size = JSize.dotXS)
                        }
                    }
                }
            }

            // 第三行：最要紧的那个 Agent 最后在说什么
            val thirdLine = remember(todo, mostUrgentSession, mostUrgentAgent, ix, isPlan) {
                when {
                    currentAgents.isEmpty() -> "还没交给 Agent"
                    mostUrgentSession?.agentState == AgentState.NeedsYou && isPlan -> {
                        ix?.plan?.lines()?.firstOrNull { it.isNotBlank() }?.trim('#', '*', ' ')
                            ?: mostUrgentSession.lastText?.replace('\n', ' ')
                            ?: "计划写好了，等你批准后开始改。"
                    }
                    mostUrgentSession?.agentState == AgentState.NeedsYou -> {
                        val name = mostUrgentSession.slotName?.takeIf { it.isNotBlank() && it != mostUrgentSession.slotIndex?.toString() }
                            ?: mostUrgentSession.displayTitle.ifEmpty { mostUrgentAgent?.title.orEmpty() }
                        val rawText = mostUrgentSession.lastText?.replace('\n', ' ').orEmpty()
                        val q = rawText.removePrefix(mostUrgentSession.displayTitle).trim().removePrefix("在问你：").removePrefix("Claude 在问你：").trim()
                        "$name 在问你：${q.ifEmpty { rawText }}"
                    }
                    mostUrgentSession?.lastText?.isNotBlank() == true -> {
                        mostUrgentSession.lastText.replace('\n', ' ')
                    }
                    mostUrgentAgent?.startError?.isNotBlank() == true -> {
                        "启动失败：${mostUrgentAgent.startError}"
                    }
                    else -> "正在进行中…"
                }
            }
            Text(
                thirdLine,
                style = JTheme.type.secondary,
                color = if (todoState == TodoState.NeedsYou && currentAgents.isNotEmpty()) JTheme.needsYou.text else c.secondaryText,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
        }
    }
}

@Composable
private fun CompletedTodoRow(
    todo: Todo,
    onClick: () -> Unit,
    onToggleDone: () -> Unit,
) {
    val c = JTheme.colors
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = JSpace.l, vertical = JSpace.m),
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
        verticalAlignment = Alignment.CenterVertically
    ) {
        // 实心已打勾圆圈
        Box(
            Modifier
                .size(JSize.iconS)
                .clip(CircleShape)
                .background(c.secondaryText)
                .clickable(onClick = onToggleDone),
            contentAlignment = Alignment.Center
        ) {
            Icon(JIcons.done, null, Modifier.size(JSize.markS), tint = Color.White)
        }

        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(
                todo.title,
                style = JTheme.type.body.copy(textDecoration = TextDecoration.LineThrough),
                color = c.secondaryText,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
            if (todo.notes.isNotBlank()) {
                Text(
                    todo.notes,
                    style = JTheme.type.caption,
                    color = c.tertiaryText,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis
                )
            }
        }

        Text(listTime(todo.done ?: todo.created), style = JTheme.type.caption, color = c.tertiaryText, maxLines = 1)
    }
}

@Composable
fun NewTodoSheet(
    hub: Hub,
    onDismiss: () -> Unit,
    onCreated: (Todo) -> Unit,
) {
    var title by remember { mutableStateOf("") }
    var notes by remember { mutableStateOf("") }
    var creating by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    JSheet(onDismiss, title = "新建待办", onClose = onDismiss) {
        Column(
            Modifier
                .fillMaxWidth()
                .padding(bottom = JSpace.m),
            verticalArrangement = Arrangement.spacedBy(JSpace.s)
        ) {
            SectionHeader("标题")
            Column(Modifier.padding(horizontal = JSpace.page)) {
                JTextField(
                    title,
                    { title = it },
                    placeholder = "待办事项标题",
                    fill = JTheme.colors.card
                )
            }

            SectionHeader("说明")
            Column(Modifier.padding(horizontal = JSpace.page)) {
                JTextField(
                    notes,
                    { notes = it },
                    placeholder = "详细说明（交给 Agent 时会一起发过去）",
                    minLines = 3,
                    maxLines = 6,
                    fill = JTheme.colors.card
                )
            }

            PillButton(
                if (creating) "正在创建…" else "创建",
                onClick = {
                    if (title.isBlank()) return@PillButton
                    creating = true
                    scope.launch {
                        runCatching { hub.createTodo(title.trim(), notes.trim()) }
                            .onSuccess { newTodo ->
                                creating = false
                                onCreated(newTodo)
                            }
                            .onFailure { e ->
                                creating = false
                                Toasts.show("创建失败：${e.message}", ToastStyle.ERROR)
                            }
                    }
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = JSpace.page, vertical = JSpace.m),
                kind = ButtonKind.Prominent,
                large = true,
                enabled = !creating && title.isNotBlank()
            )
        }
    }
}
