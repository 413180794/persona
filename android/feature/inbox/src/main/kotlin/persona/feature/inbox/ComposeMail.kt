package persona.feature.inbox

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.contracts.api.MailAccount
import persona.core.contracts.api.MailTransport
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/**
 * Full-screen mail compose dialog (写邮件 / 回复 / 回复全部 / 转发 / mailto:).
 */
@Composable
fun ComposeMailDialog(
    transport: MailTransport,
    draft: MailDraft,
    onDone: () -> Unit,
) {
    var leaving by remember { mutableStateOf(false) }
    JFullScreen(onDismiss = { leaving = true }) {
        ComposeMailPage(
            transport = transport,
            draft = draft,
            onDone = onDone,
            leaveRequest = leaving,
            onLeaveHandled = { leaving = false },
        )
    }
}

@Composable
fun ComposeMailPage(
    transport: MailTransport,
    draft: MailDraft,
    onDone: () -> Unit,
    leaveRequest: Boolean = false,
    onLeaveHandled: () -> Unit = {},
) {
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current

    var accounts by remember { mutableStateOf<List<MailAccount>>(emptyList()) }
    var accountId by rememberSaveable { mutableStateOf(draft.accountId) }
    var to by rememberSaveable { mutableStateOf(draft.to) }
    var cc by rememberSaveable { mutableStateOf(draft.cc) }
    var bcc by rememberSaveable { mutableStateOf(draft.bcc) }
    var showBcc by rememberSaveable { mutableStateOf(draft.bcc.isNotBlank()) }
    var subject by rememberSaveable { mutableStateOf(draft.subject) }
    var body by rememberSaveable { mutableStateOf(draft.body) }
    var sending by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var confirmDiscard by remember { mutableStateOf(false) }

    LaunchedEffect(transport) {
        runCatching {
            val list = transport.accounts()
            accounts = list
            if (list.isNotEmpty() && list.none { it.id == accountId }) {
                accountId = list.first().id
            }
        }
    }

    val edited = to != draft.to || cc != draft.cc || bcc != draft.bcc || subject != draft.subject || body != draft.body
    val valid = accountId.isNotBlank() &&
        MailCompose.fieldValid(to, required = true) &&
        MailCompose.fieldValid(cc, required = false) &&
        MailCompose.fieldValid(bcc, required = false)

    fun leave() {
        if (edited && !sending) confirmDiscard = true else onDone()
    }

    LaunchedEffect(leaveRequest) {
        if (leaveRequest) {
            onLeaveHandled()
            leave()
        }
    }
    BackHandler { leave() }

    fun send() {
        if (!valid || sending) return
        sending = true
        error = null
        scope.launch {
            try {
                val req = MailCompose.request(
                    draft = draft.copy(
                        accountId = accountId,
                        to = to,
                        cc = cc,
                        bcc = bcc,
                        subject = subject,
                        body = body,
                    ),
                    bcc = bcc,
                )
                transport.sendMail(req)
                feedback?.notice("已发送")
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

    JListPage(
        title = draft.title,
        onBack = ::leave,
        actions = {
            JButton(
                "发送",
                onClick = ::send,
                kind = JButtonKind.Primary,
                size = JButtonSize.Small,
                enabled = valid && !sending,
                loading = sending,
            )
        },
    ) {
        item("fields") {
            JCard {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                    FromRow(accounts, accountId) { accountId = it }
                    JDivider()
                    FieldRow("收件人", to, { to = it }, valid = MailCompose.fieldValid(to, required = true), email = true)
                    JDivider()
                    FieldRow(
                        label = "抄送",
                        value = cc,
                        onValue = { cc = it },
                        valid = MailCompose.fieldValid(cc, required = false),
                        email = true,
                        trailing = if (!showBcc) ({
                            JTextButton("密送", onClick = { showBcc = true })
                        }) else null,
                    )
                    if (showBcc) {
                        JDivider()
                        FieldRow("密送", bcc, { bcc = it }, valid = MailCompose.fieldValid(bcc, required = false), email = true)
                    }
                    JDivider()
                    FieldRow("主题", subject, { subject = it }, valid = true, email = false)
                }
            }
        }
        error?.let { err ->
            item("error") {
                JNotice(err, status = Status.NeedsYou)
            }
        }
        item("body") {
            JCard {
                Box(
                    Modifier
                        .fillMaxWidth()
                        .heightIn(min = 280.dp)
                        .padding(Tokens.Space.m),
                ) {
                    if (body.isEmpty()) {
                        Text("正文", style = PersonaTheme.type.body, color = PersonaTheme.palette.ink3)
                    }
                    BasicTextField(
                        value = body,
                        onValueChange = { body = it },
                        textStyle = PersonaTheme.type.body.copy(color = PersonaTheme.palette.ink),
                        cursorBrush = SolidColor(PersonaTheme.palette.accent),
                        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Sentences),
                        modifier = Modifier.fillMaxWidth().heightIn(min = 280.dp),
                    )
                }
            }
        }
    }

    if (confirmDiscard) {
        JDialog(
            onDismiss = { confirmDiscard = false },
            title = "放弃这封邮件？",
            text = "写的内容不会保存。",
            confirm = "放弃" to {
                confirmDiscard = false
                onDone()
            },
            dismiss = "继续编辑" to { confirmDiscard = false },
            destructive = true,
        )
    }
}

@Composable
private fun FieldLabel(text: String) {
    Text(
        text,
        modifier = Modifier.width(56.dp),
        style = PersonaTheme.type.callout,
        color = PersonaTheme.palette.ink3,
        maxLines = 1,
    )
}

@Composable
private fun FromRow(accounts: List<MailAccount>, selected: String, onSelect: (String) -> Unit) {
    var menuOpen by remember { mutableStateOf(false) }
    val currentAccount = accounts.firstOrNull { it.id == selected }
    val displayText = currentAccount?.email ?: selected.ifBlank { "未选择邮箱" }

    Box {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 44.dp)
                .clickable(enabled = accounts.size > 1) { menuOpen = true }
                .padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.s),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m),
        ) {
            FieldLabel("发件人")
            Text(
                displayText,
                modifier = Modifier.weight(1f),
                style = PersonaTheme.type.body,
                color = if (accounts.isEmpty()) PersonaTheme.palette.ink3 else PersonaTheme.palette.ink,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (accounts.size > 1) {
                JIcon(PhIcons.CaretDown, "切换账户", size = 16.dp, tint = PersonaTheme.palette.ink3)
            }
        }
        if (accounts.size > 1) {
            JMenu(
                expanded = menuOpen,
                onDismiss = { menuOpen = false },
                actions = accounts.map { acc ->
                    JAction(
                        label = acc.email.ifBlank { acc.id },
                        checked = acc.id == selected,
                    ) {
                        onSelect(acc.id)
                    }
                },
            )
        }
    }
}

@Composable
private fun FieldRow(
    label: String,
    value: String,
    onValue: (String) -> Unit,
    valid: Boolean,
    email: Boolean,
    trailing: (@Composable () -> Unit)? = null,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 44.dp)
            .padding(start = Tokens.Space.m, end = Tokens.Space.s, top = Tokens.Space.s, bottom = Tokens.Space.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m),
    ) {
        FieldLabel(label)
        BasicTextField(
            value = value,
            onValueChange = onValue,
            textStyle = PersonaTheme.type.body.copy(
                color = if (valid) PersonaTheme.palette.ink else PersonaTheme.palette.danger.text,
            ),
            cursorBrush = SolidColor(PersonaTheme.palette.accent),
            keyboardOptions = if (email) {
                KeyboardOptions(
                    keyboardType = KeyboardType.Email,
                    capitalization = KeyboardCapitalization.None,
                    autoCorrectEnabled = false,
                )
            } else {
                KeyboardOptions.Default
            },
            modifier = Modifier.weight(1f),
        )
        trailing?.invoke()
    }
}
