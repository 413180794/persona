package jarvis.android.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.data.AgentState
import jarvis.android.data.Hub
import jarvis.android.data.Todo
import jarvis.android.data.TodoState
import jarvis.android.data.ordered
import jarvis.android.data.state
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.RowDivider

/**
 * A bottom sheet allowing the user to pick an uncompleted todo to link an agent to.
 */
@Composable
fun TodoPickerSheet(
    hub: Hub,
    onDismiss: () -> Unit,
    onPick: (Todo) -> Unit,
) {
    val todos by hub.todos.collectAsState()
    val uncompleted = todos.filter { !it.isDone }.ordered { k -> hub.session(k) }

    JSheet(onDismiss, title = "关联到待办", onClose = onDismiss) {
        Column(
            Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(bottom = JSpace.m),
            verticalArrangement = Arrangement.spacedBy(JSpace.s)
        ) {
            if (uncompleted.isEmpty()) {
                Text(
                    "暂无未完成的待办",
                    style = JTheme.type.secondary,
                    color = JTheme.colors.secondaryText,
                    modifier = Modifier.padding(horizontal = JSpace.page, vertical = JSpace.l)
                )
            } else {
                GroupCard {
                    uncompleted.forEachIndexed { i, t ->
                        if (i > 0) RowDivider()
                        val state = t.state { k -> hub.session(k) }
                        val tone = JTheme.tone(when (state) {
                            TodoState.NeedsYou -> AgentState.NeedsYou
                            TodoState.Unread -> AgentState.Unread
                            TodoState.Working -> AgentState.Working
                            else -> AgentState.Idle
                        })
                        Row(
                            Modifier
                                .fillMaxWidth()
                                .clickable { onPick(t) }
                                .padding(horizontal = JSpace.l, vertical = JSpace.m),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(JSpace.m)
                        ) {
                            Icon(JIcons.todo, null, Modifier.size(JSize.iconS), tint = JTheme.colors.secondaryText)
                            Text(
                                t.title,
                                style = JTheme.type.body,
                                color = JTheme.colors.label,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.weight(1f)
                            )
                            Chip(state.title, look = ChipLook.Tint(tone.tint, tone.text))
                            Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = JTheme.colors.tertiaryText)
                        }
                    }
                }
            }
        }
    }
}
