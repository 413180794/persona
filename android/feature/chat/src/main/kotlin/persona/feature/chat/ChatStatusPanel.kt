package persona.feature.chat

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import persona.core.contracts.Session
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.resultUnknown
import persona.core.designsystem.*

@Composable
internal fun OutboxStatus(
    message: OutboxMessage, busy: Boolean, onAction: ((DeliveryAction) -> Unit)?,
    onDetails: (() -> Unit)? = null,
) {
    var retryConfirmation by remember(message.clientMessageId) { mutableStateOf(false) }
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        val pending = message.state.pendingStatus
        if (pending != null) PendingBadge(pending, reason = message.refusalReason())
        else Text("已送达，等待对话记录", style = PersonaTheme.type.caption,
            color = PersonaTheme.palette.ink2)
        if (onAction != null) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            if (message.state.allows(DeliveryAction.Cancel)) {
                JButton(kind = JButtonKind.Secondary, size = JButtonSize.Small, text = "取消发送", onClick = { onAction(DeliveryAction.Cancel) }, enabled = !busy)
                JButton(kind = JButtonKind.Secondary, size = JButtonSize.Small, text = "撤回编辑", onClick = { onAction(DeliveryAction.Edit) }, enabled = !busy)
            }
            if (message.state.allows(DeliveryAction.Retry)) JButton(kind = JButtonKind.Secondary, size = JButtonSize.Small, text = "重试", onClick = {
                if (message.state.resultUnknown) retryConfirmation = true else onAction(DeliveryAction.Retry)
            }, enabled = !busy)
            if (message.state.resultUnknown && onDetails != null)
                JButton(kind = JButtonKind.Secondary, size = JButtonSize.Small, text = "核对消息", onClick = onDetails, enabled = !busy)
        }
    }
    if (retryConfirmation) JDialog(onDismiss = { retryConfirmation = false }, title = "重发这条消息？",
        text = "还没能确认是否送达。重发可能让电脑收到两遍，请先核对对话记录。",
        confirm = "仍要重发" to { retryConfirmation = false; onAction?.invoke(DeliveryAction.Retry) },
        confirmEnabled = !busy, dismiss = "先不重发" to { retryConfirmation = false })
}

/** Keep session state and secondary facts on one line; details open in the status sheet. */
@Composable
internal fun ChatSessionStatusLine(
    state: StatusLineState, session: Session?, recordedDuration: Long?, tools: Int,
    onDetails: () -> Unit, terminal: (() -> Unit)?, draftSaveError: String? = null,
) {
    val chips = listOfNotNull(
        session?.queued?.takeIf { it.isNotEmpty() }?.let { "排队 ${it.size}" },
        session?.loop?.let { "循环" },
        session?.background?.takeIf { it.isNotEmpty() }?.let { "后台 ${it.size}" },
        draftSaveError?.let { "草稿保存失败" },
    )
    StatusLine(state, chips = chips, onClick = { onDetails() })
}
