package jarvis.android.ui.mail

import android.text.format.Formatter
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.data.ApiClient
import jarvis.android.data.InboxItem
import jarvis.android.mail.MailCompose
import jarvis.android.mail.MailDraft
import jarvis.android.mail.MailHtml
import jarvis.android.mail.MailMessage
import jarvis.android.mail.MailStore
import jarvis.android.mail.mailSetFlags
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.IconAction
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.Monogram
import jarvis.android.ui.design.PageLoading
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.surface
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/**
 * The mail of an inbox item (iOS `MailDetailView` inside `InboxDetailView`): each mail of the thread, older ones
 * folded to a line, the newest open — sender, recipients, date, the HTML in a locked-down WebView with remote images
 * on request (or the text), attachment names; under it 回复 / 回复全部 / 转发 (into [onCompose]) and the star. Opening
 * a mail marks it read on the server. The item's title and the AI's summary are drawn by the detail around it.
 */
@Composable
fun MailThread(api: ApiClient, item: InboxItem, onCompose: (MailDraft) -> Unit, modifier: Modifier = Modifier) {
    val m = item.mail ?: return
    val c = JTheme.colors
    val scope = rememberCoroutineScope()
    val accountId = m.accountId.ifEmpty { item.account }
    val mailbox = m.mailbox.ifEmpty { "INBOX" }
    val uids = remember(m.uids) { m.uids.sorted() }
    val newest = uids.lastOrNull()
    // Read before (or handed in by the screenshot tests): there at once.
    val messages = remember(item.id) {
        mutableStateMapOf<Long, MailMessage>().apply { uids.forEach { u -> MailStore.message(accountId, mailbox, u)?.let { put(u, it) } } }
    }
    val errors = remember(item.id) { mutableStateMapOf<Long, String>() }
    var open by rememberSaveable(item.id) { mutableStateOf(listOfNotNull(newest)) }
    var imagesFor by rememberSaveable(item.id) { mutableStateOf(listOf<Long>()) }
    var reload by remember { mutableStateOf(0) }

    suspend fun load(uid: Long) {
        if (messages.containsKey(uid)) return
        errors.remove(uid)
        try {
            val msg = MailStore.loadMessage(api, accountId, mailbox, uid)
            messages[uid] = msg
            if (msg.seen == false) {
                runCatching { api.mailSetFlags(accountId, uid, mailbox, seen = true) }
                MailStore.put(accountId, mailbox, msg.copy(seen = true))
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            errors[uid] = e.message ?: "读不到这封邮件"
        }
    }
    LaunchedEffect(open, reload) { open.forEach { load(it) } }
    LaunchedEffect(Unit) { runCatching { if (MailStore.accounts.value == null) MailStore.reloadAccounts(api) } }

    val me = MailStore.account(accountId)?.email
    val onLink: (String) -> Unit = { url ->
        if (url.startsWith("mailto:", ignoreCase = true)) onCompose(MailDraft(accountId = accountId, to = url.substring(7).substringBefore('?')))
        else jarvis.android.ui.web.Links.open(url, null)
    }

    Column(modifier.fillMaxWidth().surface(RoundedCornerShape(JRadius.card)).padding(vertical = JSpace.s)) {
        uids.forEachIndexed { i, uid ->
            val msg = messages[uid]
            if (i > 0) RowDivider()
            if (uid !in open) {
                FoldedMail(msg, onClick = { open = open + uid })
                return@forEachIndexed
            }
            Column(Modifier.padding(horizontal = JSpace.m, vertical = JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
                MailHeader(msg, item)
                when {
                    msg != null -> MailBody(msg, images = uid in imagesFor, onShowImages = { imagesFor = imagesFor + uid }, onLink = onLink)
                    errors[uid] != null -> Column(verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
                        ErrorLine(errors[uid]!!)
                        PillButton("重试", { reload++ }, kind = ButtonKind.Neutral, icon = JIcons.retry)
                    }
                    else -> PageLoading(Modifier.padding(bottom = JSpace.xl))
                }
                if (uid == newest && msg != null) {
                    var flagged by remember(uid) { mutableStateOf(msg.flagged == true) }
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                        PillButton("回复", { onCompose(MailCompose.reply(msg, accountId, me, all = false)) }, kind = ButtonKind.Tinted, icon = JIcons.reply)
                        IconAction(JIcons.replyAll, "回复全部", { onCompose(MailCompose.reply(msg, accountId, me, all = true)) }, tint = c.accent)
                        IconAction(JIcons.forward, "转发", { onCompose(MailCompose.forward(msg, accountId)) }, tint = c.accent)
                        Spacer(Modifier.weight(1f))
                        IconAction(if (flagged) JIcons.starred else JIcons.star, if (flagged) "取消星标" else "加星标", {
                            val to = !flagged
                            flagged = to
                            scope.launch {
                                runCatching { api.mailSetFlags(accountId, uid, mailbox, flagged = to) }
                                    .onSuccess { MailStore.put(accountId, mailbox, msg.copy(flagged = to)) }
                                    .onFailure { flagged = !to; Toasts.show("没改成：${it.message}", ToastStyle.ERROR) }
                            }
                        }, tint = if (flagged) c.caution else c.secondaryText)
                    }
                }
            }
        }
    }
}

@Composable
private fun MailHeader(m: MailMessage?, item: InboxItem) {
    val c = JTheme.colors
    val from = m?.from
    val name = from?.display ?: item.sender.orEmpty()
    Row(horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
        Monogram(name, c.accent)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(name, style = JTheme.type.secondaryStrong, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!from?.email.isNullOrEmpty() && from?.email != name) {
                Text(from!!.email, style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            m?.to?.takeIf { it.isNotEmpty() }?.let { to ->
                Text("收件人：" + to.joinToString("，") { it.display }, style = JTheme.type.label, color = c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            m?.cc?.takeIf { it.isNotEmpty() }?.let { cc ->
                Text("抄送：" + cc.joinToString("，") { it.display }, style = JTheme.type.label, color = c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        Text(jarvis.android.ui.listTime(m?.date ?: item.updated), style = JTheme.type.label, color = c.secondaryText, maxLines = 1)
    }
}

/** An older mail of the thread, folded: who, when, the first line. */
@Composable
private fun FoldedMail(m: MailMessage?, onClick: () -> Unit) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().clickable(role = Role.Button, onClick = onClick).padding(horizontal = JSpace.m, vertical = JSpace.s),
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val who = m?.from?.display ?: "更早的一封"
        Monogram(who, c.secondaryText, size = JSize.well)
        Column(Modifier.weight(1f)) {
            Text(who, style = JTheme.type.secondaryMedium, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val line = m?.let { MailCompose.plainText(it).lineSequence().firstOrNull { l -> l.isNotBlank() } } ?: "点一下展开"
            Text(line, style = JTheme.type.caption, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (m?.date != null) Text(jarvis.android.ui.listTime(m.date), style = JTheme.type.label, color = c.secondaryText, maxLines = 1)
    }
}

@Composable
private fun MailBody(m: MailMessage, images: Boolean, onShowImages: () -> Unit, onLink: (String) -> Unit) {
    val c = JTheme.colors
    val html = m.html.orEmpty()
    val text = m.text.orEmpty()
    // HTML when the mail has it (layout, tables, links); the text part otherwise.
    when {
        html.isNotBlank() -> Column(verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
            val remote = remember(html) { MailHtml.hasRemoteImages(html) }
            if (remote && !images) {
                Row(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.control)).surface(RoundedCornerShape(JRadius.control), fill = c.fillWell)
                        .padding(start = JSpace.m, end = JSpace.xs, top = JSpace.xs, bottom = JSpace.xs),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(JSpace.s),
                ) {
                    Icon(JIcons.photo, null, Modifier.size(JSize.markL), tint = c.secondaryText)
                    Text("网络图片没有加载（可能被用来确认你读过）", Modifier.weight(1f), style = JTheme.type.caption, color = c.secondaryText)
                    PillButton("显示图片", onShowImages, kind = ButtonKind.Tinted)
                }
            }
            MailWebView(html, allowImages = images, onLink = onLink, modifier = Modifier.fillMaxWidth().heightIn(min = JSize.avatar))
        }
        text.isNotBlank() -> SelectionContainer { Text(text, style = JTheme.type.body, color = c.label) }
        else -> Text("（没有正文）", style = JTheme.type.secondary, color = c.secondaryText)
    }
    val atts = m.attachments.orEmpty()
    if (atts.isNotEmpty()) {
        val ctx = LocalContext.current
        Column(
            Modifier.fillMaxWidth().surface(RoundedCornerShape(JRadius.control), fill = c.fillWell).padding(JSpace.m),
            verticalArrangement = Arrangement.spacedBy(JSpace.sm),
        ) {
            atts.forEach { a ->
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    Icon(JIcons.attach, null, Modifier.size(JSize.mark), tint = c.secondaryText)
                    Text(a.filename?.takeIf { it.isNotBlank() } ?: "附件", Modifier.weight(1f), style = JTheme.type.caption, color = c.label,
                        maxLines = 1, overflow = TextOverflow.Ellipsis)
                    a.size?.let { Text(Formatter.formatShortFileSize(ctx, it), style = JTheme.type.caption, color = c.secondaryText, maxLines = 1) }
                }
            }
            Text("附件暂时只能看到名字和大小，hub 还不能下载附件。", style = JTheme.type.label, color = c.tertiaryText)
        }
    }
}
