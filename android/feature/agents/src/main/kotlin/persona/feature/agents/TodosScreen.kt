package persona.feature.agents

import persona.core.designsystem.text
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
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
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
    val res = agentResources()

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
            title = stringResource(R.string.agents_todo_new)
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
                    label = stringResource(R.string.agents_todo_title_label),
                    placeholder = stringResource(R.string.agents_todo_title_placeholder),
                    singleLine = true
                )
                JTextField(
                    newNotes,
                    { newNotes = it },
                    label = stringResource(R.string.agents_todo_notes_optional),
                    placeholder = stringResource(R.string.agents_todo_notes_placeholder),
                    minLines = 3,
                    maxLines = 6
                )
                Spacer(Modifier.height(8.dp))
                JButton(
                    text = stringResource(R.string.agents_todo_create),
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
                                    onError(res.getString(R.string.agents_todo_create_failed, e.message.orEmpty()))
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
        title = stringResource(R.string.agents_todos_title),
        onBack = onBack,
        actions = {
            JIconButton(PhIcons.Plus, stringResource(R.string.agents_todo_new), { creating = true })
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
                JSearchField(query, { query = it }, placeholder = stringResource(R.string.agents_todos_search))
            }

            if (uncompleted.isEmpty() && completed.isEmpty()) {
                item(key = "empty") {
                    JEmptyState(
                        title = stringResource(if (query.isBlank()) R.string.agents_todos_empty else R.string.agents_todos_no_match),
                        description = stringResource(if (query.isBlank()) R.string.agents_todos_empty_hint else R.string.agents_todos_no_match_hint),
                        icon = PhIcons.ListChecks
                    )
                }
            } else {
                if (uncompleted.isNotEmpty()) {
                    item(key = "uncompleted-header") {
                        JSectionHeader(stringResource(R.string.agents_todos_open), count = uncompleted.size)
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
                            start = SwipeAction(stringResource(R.string.agents_todo_done_action), PhIcons.Check, Status.Working, {
                                scope.launch {
                                    try {
                                        tasks?.setTodoDone(todo, true)
                                    } catch (e: Exception) {
                                        onError(res.getString(R.string.agents_todo_done_failed, e.message.orEmpty()))
                                    }
                                }
                            }),
                            end = SwipeAction(stringResource(R.string.agents_todo_delete_action), PhIcons.Trash, Status.NeedsYou, {
                                scope.launch {
                                    try {
                                        tasks?.deleteTodo(todo.id)
                                    } catch (e: Exception) {
                                        onError(res.getString(R.string.agents_todo_delete_failed, e.message.orEmpty()))
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
                                                            onError(res.getString(R.string.agents_todo_done_failed, e.message.orEmpty()))
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
                                        JTag(state.label)
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
                                            ?: if (urgent.isFailed) stringResource(R.string.agents_todo_start_failed, urgent.agent.startError.orEmpty())
                                            else stringResource(R.string.agents_todo_in_progress)
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
                                        val timeStr = relativeTime(res, todo.updated ?: todo.created, now)
                                        if (timeStr.isNotBlank()) {
                                            Text(timeStr, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                                        }
                                        Spacer(Modifier.weight(1f))
                                        if (todo.currentAgents.isNotEmpty()) {
                                            Text(pluralStringResource(R.plurals.agents_todo_agents, todo.currentAgents.size, todo.currentAgents.size), style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
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
                                stringResource(R.string.agents_todos_done),
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
                                                        onError(res.getString(R.string.agents_todo_reopen_failed, e.message.orEmpty()))
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
                                        JTag(stringResource(R.string.agents_todo_auto_done))
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

/** A to-do's state in the language on screen (core:contracts' `TodoState.title` is the Chinese). */
internal val TodoState.label: String @Composable get() = stringResource(when (this) {
    TodoState.Done -> R.string.agents_todos_done
    TodoState.NeedsYou -> R.string.agents_todo_state_needs_you
    TodoState.Unread -> R.string.agents_todo_state_unread
    TodoState.Working -> R.string.agents_todo_state_working
    TodoState.FollowUp -> R.string.agents_todo_state_follow_up
    TodoState.NotStarted -> R.string.agents_todo_state_not_started
})
