package persona.feature.agents

import persona.core.designsystem.text
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.Todo
import persona.core.contracts.TodoState
import persona.core.contracts.ordered
import persona.core.contracts.state
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

@Composable
fun TodoPickerSheet(
    todos: List<Todo>,
    sessionLookup: (SessionKey) -> Session?,
    onDismiss: () -> Unit,
    onPick: (Todo) -> Unit,
    title: String = stringResource(R.string.agents_todo_link_title),
    // The todo the agent is on: marked and not picked again (linking it again would only rewrite its source, #2348).
    currentTodoId: String? = null,
    /** "New to-do…" / "Create and link" (iOS TodoLinkAgentSheet): a new to-do with this title, linked at once. */
    onCreate: ((String) -> Unit)? = null,
    /** "Unlink from to-do" / "Unlink" when it is on one: the label and what it does. */
    unlink: Pair<String, () -> Unit>? = null,
) {
    val uncompleted = todos.filter { !it.isDone }.ordered(sessionLookup)
    var creating by rememberSaveable { mutableStateOf(false) }
    var newTitle by rememberSaveable { mutableStateOf("") }
    JSheet(onDismiss = onDismiss, title = title) {
        Column(
            Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = Tokens.Space.l, vertical = Tokens.Space.s),
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)
        ) {
            if (uncompleted.isEmpty()) {
                JEmptyState(stringResource(R.string.agents_picker_empty), stringResource(R.string.agents_picker_empty_hint))
            }
            run {
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
            if (onCreate != null) {
                Spacer(Modifier.height(Tokens.Space.s))
                if (creating) {
                    JTextField(newTitle, { newTitle = it }, Modifier.fillMaxWidth(), placeholder = stringResource(R.string.agents_picker_new_title),
                        singleLine = true)
                    JButton(stringResource(R.string.agents_picker_create_and_link), { onCreate(newTitle.trim()) }, Modifier.fillMaxWidth(),
                        enabled = newTitle.isNotBlank(), icon = PhIcons.Plus)
                } else {
                    JGroupItem(GroupPosition.Single) {
                        JRow(stringResource(R.string.agents_picker_new_todo), leadingIcon = PhIcons.Plus, chevron = false, onClick = { creating = true })
                    }
                }
            }
            unlink?.let { (label, action) ->
                JGroupItem(GroupPosition.Single) {
                    JRow(label, leadingIcon = PhIcons.LinkSimple, destructive = true, chevron = false, onClick = action)
                }
            }
        }
    }
}
