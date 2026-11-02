package jarvis.feature.chat

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import jarvis.core.contracts.api.DeliveryState
import jarvis.core.contracts.api.MessageSender
import jarvis.core.contracts.api.OutboxMessage
import jarvis.core.contracts.resultUnknown
import jarvis.core.designsystem.PendingStatus

internal val DeliveryState.pendingStatus: PendingStatus?
    get() = when (this) {
        DeliveryState.Queued, DeliveryState.WaitingForNetwork -> PendingStatus.Waiting
        DeliveryState.Sending -> PendingStatus.Sending
        DeliveryState.Unknown -> PendingStatus.Unconfirmed
        DeliveryState.Verifying -> PendingStatus.Verifying
        DeliveryState.NeedsConfirmation -> PendingStatus.NeedsReview
        DeliveryState.Rejected, DeliveryState.Refused -> PendingStatus.Refused
        DeliveryState.Sent -> null
    }

internal enum class DeliveryAction { Cancel, Edit, Retry, ConfirmDelivered, ConfirmNotDelivered }

internal fun DeliveryState.allows(action: DeliveryAction): Boolean = when (action) {
    DeliveryAction.Cancel, DeliveryAction.Edit -> this == DeliveryState.Queued || this == DeliveryState.WaitingForNetwork
    DeliveryAction.Retry -> this == DeliveryState.Refused || this == DeliveryState.Rejected || this == DeliveryState.NeedsConfirmation
    DeliveryAction.ConfirmDelivered, DeliveryAction.ConfirmNotDelivered -> resultUnknown
}

/** Both the timeline and the sheet share the same in-flight guard and authoritative transitions. */
internal class ChatDeliveryActions(private val sender: MessageSender) {
    var busyIds by mutableStateOf(emptySet<String>())
        private set

    suspend fun perform(id: String, action: DeliveryAction, restore: (String) -> Unit): Boolean {
        if (id in busyIds) return false
        val message = sender.outbox.value.firstOrNull { it.clientMessageId == id } ?: return false
        if (!message.state.allows(action)) return false
        busyIds = busyIds + id
        try {
            val changed = when (action) {
                DeliveryAction.Cancel, DeliveryAction.Edit -> sender.cancel(id)
                DeliveryAction.Retry -> sender.retry(id)
                DeliveryAction.ConfirmDelivered -> sender.confirm(id, delivered = true)
                DeliveryAction.ConfirmNotDelivered -> sender.confirm(id, delivered = false)
            }
            if (changed && action in setOf(DeliveryAction.Edit, DeliveryAction.ConfirmNotDelivered)) restore(message.text)
            return changed
        } finally {
            busyIds = busyIds - id
        }
    }
}

internal fun OutboxMessage.refusalReason(): String = when (error) {
    "input_draft" -> "电脑上有草稿"
    "dialog_open" -> "请先处理电脑上的对话框"
    "op_id_reused" -> "这条消息的发送标识已被使用"
    "invalid" -> "消息内容不符合要求"
    else -> error?.takeIf(String::isNotBlank) ?: "电脑未接受这条消息"
}
