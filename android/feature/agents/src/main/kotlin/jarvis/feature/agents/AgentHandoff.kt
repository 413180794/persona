package jarvis.feature.agents

import jarvis.core.contracts.Session
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.MainConversationSender
import jarvis.core.contracts.api.PendingMainConversation
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.filterNotNull

/** Keep the caller's operation ID through local write failures and navigation. */
internal suspend fun handoffToMain(
    sender: MainConversationSender,
    operationId: String,
    session: Session,
    onQueued: () -> Unit,
    onBound: (SessionKey) -> Unit,
) {
    val text = session.handoffPrompt()
    if (sender.outbox.value.none { it.clientMessageId == operationId }) sender.sendToMain(text, operationId)
    onQueued()
    val target = sender.outbox.map { messages ->
        messages.firstOrNull { it.clientMessageId == operationId }?.session?.takeIf {
            it != PendingMainConversation && it.machineId.isNotBlank() && it.sessionId.isNotBlank()
        }
    }.filterNotNull().first()
    onBound(target)
}
