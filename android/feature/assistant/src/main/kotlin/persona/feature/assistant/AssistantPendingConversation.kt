package persona.feature.assistant

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import persona.core.contracts.Session
import persona.core.contracts.api.DeliveryState
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.PendingMainConversation
import persona.core.designsystem.*

/** The durable outbox is the only source of pending bubbles, including after process recreation. */
@Composable
internal fun AssistantPendingConversation(
    messages: List<OutboxMessage>, session: Session?, modifier: Modifier = Modifier,
    contentPadding: PaddingValues = PaddingValues(), demo: Boolean = false,
) {
    LazyColumn(modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = pagePadding, end = pagePadding,
            top = contentPadding.calculateTopPadding() + 12.dp, bottom = contentPadding.calculateBottomPadding() + 16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (demo) item { AssistantDemoNotice() }
        item { StatusLine(assistantStatus(session, messages)) }
        if (messages.isEmpty()) item {
            JEmptyState(stringResource(R.string.assistant_pending_empty_title), stringResource(R.string.assistant_pending_empty_detail))
        }
        items(messages, key = { it.clientMessageId }) { message ->
            Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.End,
                verticalArrangement = Arrangement.spacedBy(4.dp)) {
                UserBubble(message.text)
                message.state.pendingStatus()?.let { PendingBadge(it) }
            }
        }
    }
}

@Preview(name = "Offline first message (light)", showBackground = true, widthDp = 320, heightDp = 480)
@Preview(name = "Offline first message (dark)", showBackground = true, widthDp = 320, heightDp = 480,
    uiMode = android.content.res.Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun PendingConversationPreview() {
    PersonaTheme(dynamicColor = false) {
        JPage(title = "Persona") { padding ->
            AssistantPendingConversation(listOf(OutboxMessage("preview", PendingMainConversation,
                stringResource(R.string.assistant_suggest_calendar_prompt), DeliveryState.WaitingForNetwork, mainTarget = true)), null,
                Modifier.fillMaxSize(), contentPadding = padding)
        }
    }
}
