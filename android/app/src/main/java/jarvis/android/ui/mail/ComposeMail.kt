package jarvis.android.ui.mail

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import jarvis.android.data.ApiClient
import jarvis.android.mail.MailAccount
import jarvis.android.mail.MailCompose
import jarvis.android.mail.MailDraft
import jarvis.android.mail.MailStore
import jarvis.android.mail.sendMail
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/**
 * 写邮件 / 回复 / 转发 over everything (iOS presents `ComposeView` as a sheet): a full-screen window of its own, so it
 * also comes up over the inbox detail's sheet. Leaving with something typed asks first.
 */
@Composable
fun ComposeMailDialog(api: ApiClient, draft: MailDraft, onDone: () -> Unit) {
    var leaving by remember { mutableStateOf(false) }
    Dialog(
        onDismissRequest = { leaving = true },
        properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false, dismissOnClickOutside = false),
    ) {
        ComposeMailPage(api, draft, onDone, leaveRequest = leaving, onLeaveHandled = { leaving = false })
    }
}

/**
 * The page itself (iOS `ComposeView`): 发件人 (one of the hub's accounts), 收件人 / 抄送 / 密送 (comma separated,
 * `Name <a@b>` works), 主题, 正文 (a reply comes with the quote). Sending goes through the hub's SMTP / Gmail API.
 * No attachments: the hub's send API has none yet (docs/mail.md).
 */
@Composable
fun ComposeMailPage(
    api: ApiClient, draft: MailDraft, onDone: () -> Unit,
    /** The window was asked to close (back): leave, or ask first when something is typed. */
    leaveRequest: Boolean = false, onLeaveHandled: () -> Unit = {},
) {
    val c = JTheme.colors
    val scope = rememberCoroutineScope()
    val accounts = MailStore.accounts.collectAsState().value.orEmpty()
    var accountId by rememberSaveable { mutableStateOf(draft.accountId) }
    var to by rememberSaveable { mutableStateOf(draft.to) }
    var cc by rememberSaveable { mutableStateOf(draft.cc) }
    var bcc by rememberSaveable { mutableStateOf("") }
    var showBcc by rememberSaveable { mutableStateOf(false) }
    var subject by rememberSaveable { mutableStateOf(draft.subject) }
    var body by rememberSaveable { mutableStateOf(draft.body) }
    var sending by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var confirmDiscard by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) { runCatching { MailStore.reloadAccounts(api) } }
    LaunchedEffect(accounts) {
        if (accounts.isNotEmpty() && accounts.none { it.id == accountId }) accountId = accounts.first().id
    }

    val edited = to != draft.to || cc != draft.cc || bcc.isNotEmpty() || subject != draft.subject || body != draft.body
    val valid = accountId.isNotEmpty() && MailCompose.fieldValid(to, required = true) &&
        MailCompose.fieldValid(cc, required = false) && MailCompose.fieldValid(bcc, required = false)

    fun leave() { if (edited && !sending) confirmDiscard = true else onDone() }
    LaunchedEffect(leaveRequest) { if (leaveRequest) { onLeaveHandled(); leave() } }
    BackHandler { leave() }

    fun send() {
        if (!valid || sending) return
        sending = true
        error = null
        scope.launch {
            try {
                api.sendMail(MailCompose.request(draft.copy(accountId = accountId, to = to, cc = cc, subject = subject, body = body), bcc))
                Toasts.show("已发送", ToastStyle.SUCCESS)
                onDone()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.message ?: "发送失败"
            } finally {
                sending = false
            }
        }
    }

    Column(Modifier.fillMaxSize().background(c.page)) {
        PageHeader(draft.title, onBack = ::leave, actions = {
            if (sending) CircularProgressIndicator(Modifier.size(JSize.iconXS), strokeWidth = JSize.rule, color = c.accent)
            else PillButton("发送", ::send, kind = ButtonKind.Prominent, icon = JIcons.sendMail, enabled = valid)
        })
        Column(
            Modifier.fillMaxSize().imePadding().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(bottom = JSpace.xl),
            verticalArrangement = Arrangement.spacedBy(JSpace.m),
        ) {
            GroupCard(Modifier.padding(top = JSpace.xs)) {
                FromRow(accounts, accountId) { accountId = it }
                RowDivider()
                FieldRow("收件人", to, { to = it }, MailCompose.fieldValid(to, false), email = true)
                RowDivider()
                FieldRow("抄送", cc, { cc = it }, MailCompose.fieldValid(cc, false), email = true, trailing = if (!showBcc) ({
                    Text("密送", style = JTheme.type.secondaryMedium, color = c.accent, maxLines = 1,
                        modifier = Modifier.clickable { showBcc = true }.padding(horizontal = JSpace.s, vertical = JSpace.xs))
                }) else null)
                if (showBcc) {
                    RowDivider()
                    FieldRow("密送", bcc, { bcc = it }, MailCompose.fieldValid(bcc, false), email = true)
                }
                RowDivider()
                FieldRow("主题", subject, { subject = it }, valid = true, email = false)
            }
            error?.let { ErrorLine(it, Modifier.padding(horizontal = JSpace.page + JSpace.xs)) }
            GroupCard {
                Box(Modifier.fillMaxWidth().padding(JSpace.l)) {
                    if (body.isEmpty()) Text("正文", style = JTheme.type.body, color = c.tertiaryText)
                    BasicTextField(
                        body, { body = it },
                        textStyle = JTheme.type.body.copy(color = c.label),
                        cursorBrush = SolidColor(c.accent),
                        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Sentences),
                        modifier = Modifier.fillMaxWidth().heightIn(min = JSize.box280),
                    )
                }
            }
        }
    }

    if (confirmDiscard) {
        AlertDialog(
            onDismissRequest = { confirmDiscard = false },
            title = { Text("放弃这封邮件？") },
            text = { Text("写的内容不会保存。") },
            confirmButton = { TextButton(onClick = { confirmDiscard = false; onDone() }) { Text("放弃", color = c.danger, maxLines = 1) } },
            dismissButton = { TextButton(onClick = { confirmDiscard = false }) { Text("继续编辑", maxLines = 1) } },
        )
    }
}

@Composable
private fun FieldLabel(text: String) {
    Text(text, Modifier.widthIn(min = JSize.box56), style = JTheme.type.body, color = JTheme.colors.secondaryText, maxLines = 1)
}

@Composable
private fun FromRow(accounts: List<MailAccount>, selected: String, onSelect: (String) -> Unit) {
    val c = JTheme.colors
    var open by remember { mutableStateOf(false) }
    Box {
        Row(
            Modifier.fillMaxWidth().clickable(enabled = accounts.size > 1) { open = true }.heightIn(min = JSize.box50).padding(horizontal = JSpace.l),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(JSpace.s),
        ) {
            FieldLabel("发件人")
            Text(accounts.firstOrNull { it.id == selected }?.email ?: "没有邮箱账号", Modifier.weight(1f), style = JTheme.type.body,
                color = if (accounts.isEmpty()) c.tertiaryText else c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (accounts.size > 1) Icon(JIcons.expand, null, Modifier.size(JSize.iconXS), tint = c.secondaryText)
        }
        DropdownMenu(open, onDismissRequest = { open = false }) {
            accounts.forEach { a ->
                DropdownMenuItem(
                    text = { Text(a.email, style = if (a.id == selected) JTheme.type.bodyStrong else JTheme.type.body, maxLines = 1) },
                    trailingIcon = { if (a.id == selected) Icon(JIcons.done, "已选", tint = c.accent) },
                    onClick = { open = false; onSelect(a.id) },
                )
            }
        }
    }
}

@Composable
private fun FieldRow(
    label: String, value: String, onValue: (String) -> Unit, valid: Boolean, email: Boolean,
    trailing: (@Composable () -> Unit)? = null,
) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.box50).padding(start = JSpace.l, end = JSpace.s, top = JSpace.m, bottom = JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        FieldLabel(label)
        BasicTextField(
            value, onValue,
            textStyle = JTheme.type.body.copy(color = if (valid) c.label else c.danger),
            cursorBrush = SolidColor(c.accent),
            keyboardOptions = if (email) KeyboardOptions(keyboardType = KeyboardType.Email, capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false)
            else KeyboardOptions.Default,
            modifier = Modifier.weight(1f),
        )
        trailing?.invoke()
    }
}
