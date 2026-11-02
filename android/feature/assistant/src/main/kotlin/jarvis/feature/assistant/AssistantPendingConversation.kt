package jarvis.feature.assistant

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import jarvis.core.contracts.Session
import jarvis.core.contracts.api.DeliveryState
import jarvis.core.contracts.api.OutboxMessage
import jarvis.core.contracts.api.PendingMainConversation
import jarvis.core.designsystem.*

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
            JEmptyState("问问 Jarvis", "发出的内容会先保存在本机，连上后继续发送。")
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

@Preview(name = "离线首句（浅色）", showBackground = true, widthDp = 320, heightDp = 480)
@Preview(name = "离线首句（深色）", showBackground = true, widthDp = 320, heightDp = 480,
    uiMode = android.content.res.Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun PendingConversationPreview() {
    JarvisTheme(dynamicColor = false) {
        JPage(title = "Jarvis") { padding ->
            AssistantPendingConversation(listOf(OutboxMessage("preview", PendingMainConversation,
                "帮我整理今天的日程。", DeliveryState.WaitingForNetwork, mainTarget = true)), null,
                Modifier.fillMaxSize(), contentPadding = padding)
        }
    }
}
