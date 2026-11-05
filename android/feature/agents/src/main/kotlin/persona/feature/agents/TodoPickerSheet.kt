package persona.feature.agents

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.Todo
import persona.core.contracts.TodoState
import persona.core.contracts.ordered
import persona.core.contracts.state
import persona.core.designsystem.*

@Composable
fun TodoPickerSheet(
    todos: List<Todo>,
    sessionLookup: (SessionKey) -> Session?,
    onDismiss: () -> Unit,
    onPick: (Todo) -> Unit,
    title: String = stringResource(R.string.agents_todo_link_title),
    // The todo the agent is on: marked and not picked again (linking it again would only rewrite its source, #2348).
    currentTodoId: String? = null,
) {
    val uncompleted = todos.filter { !it.isDone }.ordered(sessionLookup)
    JSheet(onDismiss = onDismiss, title = title) {
        Column(
            Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            if (uncompleted.isEmpty()) {
                JEmptyState(stringResource(R.string.agents_picker_empty), stringResource(R.string.agents_picker_empty_hint))
            } else {
                uncompleted.forEachIndexed { index, todo ->
                    val state = todo.state(sessionLookup)
                    val status = when (state) {
                        TodoState.NeedsYou -> Status.NeedsYou
                        TodoState.Unread -> Status.Unread
                        TodoState.Working -> Status.Working
                        else -> Status.Idle
                    }
                    JGroupItem(GroupPosition.of(index, uncompleted.size)) {
                        JRow(
                            title = todo.title,
                            subtitle = todo.notes.takeIf { it.isNotBlank() },
                            leading = { JStatusDot(status) },
                            trailing = { JTag(if (todo.id == currentTodoId) stringResource(R.string.agents_picker_current) else state.label) },
                            chevron = todo.id != currentTodoId,
                            onClick = { if (todo.id != currentTodoId) onPick(todo) }
                        )
                    }
                }
            }
        }
    }
}
