package persona.feature.inbox

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import persona.core.designsystem.*

/**
 * Sending in the user's name asks first, the way the old app did: where it goes and every word of it. Every AI draft
 * (the AI 分身's, AI 草拟) goes through this; a reply the user typed is sent as typed (2026-11-02, settled for the port).
 */
data class SendConfirmation(val destination: String, val text: String, val warning: String? = null) {
    val title: String get() = "以你的身份发送？"
    val message: String get() = "$destination：\n\n$text"
}

/** 发到飞书「标题」 for Feishu (chats, documents, posts), 回复邮件「标题」 for mail. */
fun sendConfirmation(record: InboxRecord, text: String): SendConfirmation =
    SendConfirmation(if (record.isLark) "发到飞书「${record.item.title}」" else "回复邮件「${record.item.title}」", text.trim())

/** A mail reply names the message and who gets it; [targetChanged] warns that a newer message came in. */
fun mailSendConfirmation(subject: String, recipient: String, text: String, targetChanged: Boolean): SendConfirmation =
    SendConfirmation(
        "回复邮件「${subject.ifBlank { "无主题" }}」" + (recipient.takeIf { it.isNotBlank() }?.let { "，发给 $it" } ?: ""),
        text.trim(),
        warning = if (targetChanged) "收到新邮件或收件人变了：这次会回复最新一封。" else null,
    )

@Composable
internal fun SendAsYouDialog(confirmation: SendConfirmation, onSend: () -> Unit, onDismiss: () -> Unit, enabled: Boolean = true) {
    val palette = PersonaTheme.palette
    JDialog(onDismiss = onDismiss, title = confirmation.title, confirm = "发送" to onSend, confirmEnabled = enabled) {
        // The whole draft is shown, so a long one scrolls inside the dialog.
        Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            confirmation.warning?.let { Text(it, style = PersonaTheme.type.callout, color = palette.cautionText) }
            Text("${confirmation.destination}：", style = PersonaTheme.type.callout, color = palette.ink2)
            Text(confirmation.text, style = PersonaTheme.type.body, color = palette.ink)
        }
    }
}
