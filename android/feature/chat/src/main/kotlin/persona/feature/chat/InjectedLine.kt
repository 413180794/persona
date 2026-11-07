package persona.feature.chat

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.flow.MutableStateFlow
import persona.core.contracts.Injected
import persona.core.contracts.Todo
import persona.core.designsystem.ChatHint
import persona.core.designsystem.JSheet
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.taskOfInjected

private val noTodos = MutableStateFlow(emptyList<Todo>())

/**
 * A message Persona typed in for the model (a task's notice, a scheduled run…; docs/protocol.md "Messages Persona injects"):
 * one centered line, never its words. A task's notice opens the task (in the assistant's conversations); a scheduled
 * run's line shows what the task asks.
 */
@Composable
internal fun InjectedLine(id: String, n: Injected) {
    val host = LocalAssistantTaskHost.current
    val todos by (host?.tasks?.todos ?: noTodos).collectAsStateWithLifecycle()
    var open by rememberSaveable(id) { mutableStateOf(false) }
    val todo = host?.let { h -> taskOfInjected(n, todos, h.conversation)?.let { tid -> todos.firstOrNull { it.id == tid } } }
    val (icon, tint) = injectedIcon(n)
    val onClick: (() -> Unit)? = when {
        todo != null && host != null -> { { openTask(host, todo) } }
        !n.detail.isNullOrBlank() -> { { open = true } }
        else -> null
    }
    ChatHint(n.line, icon, tint, onClick, Modifier.testTag("injected-${n.kind}"),
        onClickLabel = stringResource(if (todo != null) R.string.chat_injected_open_task else R.string.chat_injected_view_request))
    if (open) JSheet({ open = false }, title = n.title ?: n.line, scrollable = true) {
        ChatMarkdown(n.detail.orEmpty(), "injected/$id")
    }
}

@Composable
private fun injectedIcon(n: Injected): Pair<PhIcon, Color> {
    val palette = PersonaTheme.palette
    return when (n.kind) {
        "task_notice" -> when (n.state) {
            "needs" -> PhIcons.Hand to palette.needsYou.text
            "interrupted", "failed" -> PhIcons.WarningCircle to palette.danger.dot
            else -> PhIcons.CheckCircle to palette.positive
        }
        "followup" -> (if (n.state == "wake") PhIcons.BellRinging else PhIcons.Clock) to palette.accent
        else -> PhIcons.Info to palette.ink3
    }
}
