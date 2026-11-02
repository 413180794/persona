package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.JarvisApp
import jarvis.android.data.Hub
import jarvis.android.data.InboxItem
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.PillButton
import kotlinx.coroutines.launch

// What an inbox item opened as a page shows over its content (iOS InboxActions.swift): the AI's gist with AI 草拟回复 /
// 交给 AI, and the reply sheet.

/** Over an opened item's content (iOS `InboxActionBar`): ✦ the AI's gist, then AI 草拟回复 (查看草稿) and 交给 AI. */
@Composable
fun InboxActionBar(item: InboxItem, onDraft: () -> Unit, onHandOff: () -> Unit, modifier: Modifier = Modifier) {
    val c = JTheme.colors
    Column(modifier.fillMaxWidth().background(c.card)) {
        Column(Modifier.padding(horizontal = JSpace.l, vertical = JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
            item.summary?.takeIf { it.isNotBlank() }?.let {
                Row(horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                    Icon(JIcons.ai, null, Modifier.padding(top = JSpace.xxs).size(JSize.markS), tint = c.ai)
                    Text(it, style = JTheme.type.secondary, color = c.label, maxLines = 3, overflow = TextOverflow.Ellipsis)
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                val drafted = item.agent?.state == "drafted" && !item.agent.draft.isNullOrBlank()
                PillButton(if (drafted) "查看草稿" else "AI 草拟回复", onDraft, Modifier.weight(1f), kind = ButtonKind.Prominent, icon = JIcons.draftReply)
                PillButton("交给 AI", onHandOff, Modifier.weight(1f), kind = ButtonKind.Tinted, icon = JIcons.handToAI)
            }
        }
        HorizontalDivider(thickness = JSize.hairline, color = c.separator)
    }
}

/**
 * AI 草拟回复 (iOS `InboxDraftSheet`): the AI 分身's draft, or a new one drafted on opening; editable; 重写 asks again
 * with what to change. 发送 asks first (it goes out as the user), then the sheet closes and it sends in the
 * background: Feishu in the chat, mail as a reply, removed as a comment; the item is then done.
 */
@Composable
fun InboxDraftSheet(hub: Hub, item: InboxItem, onDismiss: () -> Unit, onSent: () -> Unit = {}) {
    val agentDraft = item.agent?.takeIf { it.state == "drafted" }?.draft?.takeIf { it.isNotBlank() }
    var text by rememberSaveable(item.id) { mutableStateOf(agentDraft.orEmpty()) }
    var instruction by rememberSaveable(item.id) { mutableStateOf("") }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var confirm by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val c = JTheme.colors

    fun draft() {
        loading = true; error = null
        scope.launch {
            runCatching { hub.api.inboxDraft(item.id, instruction.trim()) }
                .onSuccess { text = it; instruction = "" }
                .onFailure { error = "没草拟出来：${it.message}" }
            loading = false
        }
    }
    LaunchedEffect(item.id) { if (text.isEmpty()) draft() }

    val title = when {
        item.moment != null -> "评论removed"
        item.source == "lark" -> "回复 ${item.title}"
        else -> "回复邮件"
    }
    JSheet(onDismiss, title = title, onClose = onDismiss) {
        Column(Modifier.padding(horizontal = JSpace.page), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
            item.agent?.note?.takeIf { it.isNotBlank() && agentDraft != null && text == agentDraft }?.let {
                Row(horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                    Icon(JIcons.ai, null, Modifier.padding(top = JSpace.xxs).size(JSize.markS), tint = c.ai)
                    Text(it, style = JTheme.type.caption, color = c.secondaryText, maxLines = 4, overflow = TextOverflow.Ellipsis)
                }
            }
            JTextField(text, { text = it }, if (loading) "AI 正在起草…" else "回复的内容", minLines = 5, maxLines = 12, enabled = !loading, fill = c.card)
            error?.let { ErrorLine(it) }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                JTextField(instruction, { instruction = it }, "让 AI 改：语气、要点…", Modifier.weight(1f), singleLine = true, enabled = !loading, fill = c.card)
                if (loading) CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule, color = c.secondaryText)
                else PillButton("重写", { draft() }, kind = ButtonKind.Tinted, icon = JIcons.retry)
            }
            PillButton("发送", { confirm = true }, Modifier.fillMaxWidth(), kind = ButtonKind.Prominent, large = true, icon = JIcons.sendMail,
                enabled = !loading && text.isNotBlank())
        }
    }
    if (confirm) ConfirmSend(item, text, onDismiss = { confirm = false }) {
        confirm = false
        val body = text.trim()
        onDismiss()
        JarvisApp.instance.scope.launch {
            runCatching { hub.api.inboxReply(item.id, body) }
                .onSuccess { hub.loadInbox(); Toasts.show("已发送", ToastStyle.SUCCESS); onSent() }
                .onFailure { Toasts.show("没发出去：${it.message}", ToastStyle.ERROR) }
        }
    }
}
