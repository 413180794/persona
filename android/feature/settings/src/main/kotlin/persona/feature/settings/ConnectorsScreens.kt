package persona.feature.settings

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import java.time.Instant

/**
 * 设置 › 连接器 › 邮箱 (iOS `MailAccountsView`): list all accounts on the hub,
 * test connection, delete account, and link to add account.
 */
@Composable
internal fun MailAccountsPage(
    connectors: ConnectorsManagement?,
    onBack: () -> Unit,
    onAdd: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    val res = LocalResources.current
    var accounts by remember { mutableStateOf<List<MailAccount>?>(null) }
    var loading by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<MailAccount?>(null) }
    var testingId by remember { mutableStateOf<String?>(null) }
    var actionAccount by remember { mutableStateOf<MailAccount?>(null) }

    fun load() {
        if (connectors == null) {
            loading = false
            return
        }
        scope.launch {
            loading = true
            try {
                accounts = connectors.mailAccounts()
                error = null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.message ?: res.getString(R.string.settings_src_mail_read_failed)
            } finally {
                loading = false
            }
        }
    }

    LaunchedEffect(connectors) { load() }
    BackHandler(onBack = onBack)

    JListPage(title = stringResource(R.string.settings_src_mail_title), onBack = onBack) {
        val list = accounts
        when {
            loading && list == null -> item("loading") { JLoading() }
            list.isNullOrEmpty() -> {
                item("empty") {
                    JEmptyState(
                        title = stringResource(R.string.settings_src_mail_empty_title),
                        description = error ?: stringResource(R.string.settings_src_mail_empty_body),
                        icon = PhIcons.EnvelopeSimple,
                    )
                }
            }
            else -> {
                item("accounts-header") { JSectionHeader(stringResource(R.string.settings_src_mail_accounts_header)) }
                itemsIndexed(list, key = { _, a -> a.id }) { index, a ->
                    JGroupItem(GroupPosition.of(index, list.size)) {
                        JRow(
                            title = a.email,
                            subtitle = a.detail,
                            subtitleLines = 2,
                            leadingIcon = PhIcons.EnvelopeSimple,
                            onClick = { actionAccount = a },
                            trailing = {
                                if (testingId == a.id) {
                                    JSpinner(stroke = 2.dp)
                                } else {
                                    JIconButton(PhIcons.DotsThreeVertical, stringResource(R.string.settings_src_more), onClick = { actionAccount = a })
                                }
                            },
                        )
                    }
                }
            }
        }
        item("add-header") { JSectionHeader(stringResource(R.string.settings_src_add_header)) }
        item("add-button") {
            JGroupItem(GroupPosition.Single) {
                JRow(stringResource(R.string.settings_src_mail_add), leadingIcon = PhIcons.Plus, onClick = onAdd)
            }
        }
        item("add-caption") {
            SettingsCaption(
                stringResource(R.string.settings_src_mail_secret_caption),
                Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
            )
        }
        error?.let { err ->
            item("error") {
                JNotice(err, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
            }
        }
    }

    actionAccount?.let { a ->
        JActionSheet(
            actions = listOf(
                JAction(stringResource(R.string.settings_src_mail_test), onClick = {
                    val targetId = a.id
                    testingId = targetId
                    scope.launch {
                        try {
                            val r = connectors?.testMailAccount(targetId)
                            if (r?.ok == true) {
                                feedback?.notice(res.getString(R.string.settings_src_mail_test_ok))
                            } else {
                                feedback?.notice(res.getString(R.string.settings_src_mail_test_unreachable, r?.error ?: res.getString(R.string.settings_src_unknown_error)))
                            }
                            load()
                        } catch (e: Exception) {
                            feedback?.notice(e.message ?: res.getString(R.string.settings_src_mail_test_failed))
                        } finally {
                            testingId = null
                        }
                    }
                }, icon = PhIcons.PlugsConnected),
                JAction(stringResource(R.string.settings_src_delete), onClick = { deleting = a }, icon = PhIcons.Trash, destructive = true),
            ),
            onDismiss = { actionAccount = null },
            title = a.email,
        )
    }

    deleting?.let { a ->
        JDialog(
            title = stringResource(R.string.settings_src_mail_delete_title, a.email),
            content = {
                Text(
                    stringResource(R.string.settings_src_mail_delete_body),
                    style = PersonaTheme.type.callout,
                    color = PersonaTheme.palette.ink2,
                )
            },
            confirm = stringResource(R.string.settings_src_delete) to {
                val targetId = a.id
                deleting = null
                scope.launch {
                    try {
                        connectors?.deleteMailAccount(targetId)
                        load()
                        feedback?.notice(res.getString(R.string.settings_src_deleted))
                    } catch (e: Exception) {
                        feedback?.notice(res.getString(R.string.settings_src_mail_delete_failed, e.message.orEmpty()))
                    }
                }
            },
            dismiss = stringResource(R.string.settings_src_cancel) to { deleting = null },
            onDismiss = { deleting = null },
        )
    }
}

/**
 * 设置 › 连接器 › 添加邮箱 (iOS `AddMailAccountView`, docs/mail.md).
 * Supports Gmail (Google OAuth or app password), Feishu (fixed servers), and other IMAP.
 */
@Composable
internal fun AddMailAccountPage(
    connectors: ConnectorsManagement?,
    onBack: () -> Unit,
    onAdded: () -> Unit,
    start: MailAccountForm = MailAccountForm(),
) {
    val context = LocalContext.current
    val res = LocalResources.current
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    var form by rememberSaveable(stateSaver = MailAccountForm.Saver) { mutableStateOf(start) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var gmailFallback by rememberSaveable { mutableStateOf<String?>(null) }

    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        if (GoogleSignIn.isWaiting) {
            scope.launch {
                delay(1500)
                if (GoogleSignIn.isWaiting) GoogleSignIn.cancel()
            }
        }
    }

    LaunchedEffect(Unit) {
        GoogleSignIn.finished.collect {
            when (val f = GoogleSignIn.takeFinished()) {
                is GoogleSignIn.Finished.Added -> {
                    feedback?.notice(res.getString(R.string.settings_src_mail_added, f.account.email))
                    onAdded()
                }
                is GoogleSignIn.Finished.Error -> {
                    error = f.error.message ?: res.getString(R.string.settings_src_mail_google_failed)
                }
                null -> Unit
            }
        }
    }

    fun save() {
        if (connectors == null) return
        saving = true
        error = null
        scope.launch {
            try {
                val req = form.request()
                val added = connectors.addMailAccount(req)
                feedback?.notice(res.getString(R.string.settings_src_mail_added, added.email))
                onAdded()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.message ?: res.getString(R.string.settings_src_mail_connect_failed)
            } finally {
                saving = false
            }
        }
    }

    fun signInWithGoogle() {
        if (connectors == null) return
        saving = true
        error = null
        scope.launch {
            try {
                GoogleSignIn.signIn(context, connectors, form.email.takeIf { it.contains('@') })
            } catch (e: CancellationException) {
                throw e
            } catch (_: UnsupportedOperationException) {
                gmailFallback = res.getString(R.string.settings_src_mail_google_unsupported)
            } catch (e: Exception) {
                val msg = e.message.orEmpty()
                if (msg.contains("not configured", ignoreCase = true)) {
                    gmailFallback = res.getString(R.string.settings_src_mail_google_not_configured)
                } else {
                    error = msg.ifEmpty { res.getString(R.string.settings_src_mail_google_start_failed) }
                }
            } finally {
                saving = false
            }
        }
    }

    val gmailSignIn = form.kind == MailAccountForm.Kind.GMAIL && gmailFallback == null
    val canConnect = form.canConnect(gmailFallback != null) && !saving
    BackHandler(onBack = onBack)

    JListPage(
        title = stringResource(R.string.settings_src_mail_add),
        onBack = onBack,
        bottomBar = if (!gmailSignIn) {
            {
                Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s)) {
                    JButton(stringResource(R.string.settings_src_mail_connect), ::save, Modifier.fillMaxWidth(), enabled = canConnect, loading = saving)
                }
            }
        } else null,
    ) {
        item("kind") {
            JSegmented(
                options = MailAccountForm.Kind.entries.map { JSegment(stringResource(it.title)) },
                selected = form.kind.ordinal,
                onSelect = {
                    form = form.copy(kind = MailAccountForm.Kind.entries[it])
                    error = null
                },
                modifier = Modifier.fillMaxWidth().padding(top = Tokens.Space.s),
            )
        }

        if (gmailSignIn) {
            item("google-btn") {
                Column(Modifier.fillMaxWidth().padding(top = Tokens.Space.l)) {
                    JButton(
                        stringResource(if (saving) R.string.settings_src_mail_google_connecting else R.string.settings_src_mail_google_sign_in),
                        onClick = ::signInWithGoogle,
                        enabled = !saving,
                        loading = saving,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    SettingsCaption(
                        stringResource(R.string.settings_src_mail_google_caption),
                        Modifier.padding(top = Tokens.Space.s),
                    )
                }
            }
        } else {
            gmailFallback?.takeIf { form.kind == MailAccountForm.Kind.GMAIL }?.let { why ->
                item("fallback-notice") {
                    JNotice(why, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
                }
            }
            item("form-fields") {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s), modifier = Modifier.padding(top = Tokens.Space.m)) {
                    JTextField(
                        value = form.email,
                        onValueChange = { form = form.copy(email = it.trim()) },
                        label = stringResource(R.string.settings_src_mail_address),
                        placeholder = "name@example.com",
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, autoCorrectEnabled = false),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    JTextField(
                        value = form.password,
                        onValueChange = { form = form.copy(password = it) },
                        label = stringResource(if (form.kind == MailAccountForm.Kind.GMAIL) R.string.settings_src_mail_app_password else R.string.settings_src_mail_password),
                        placeholder = stringResource(if (form.kind == MailAccountForm.Kind.GMAIL) R.string.settings_src_mail_app_password_hint else R.string.settings_src_required),
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            item("form-footer") {
                SettingsCaption(
                    stringResource(when (form.kind) {
                        MailAccountForm.Kind.FEISHU -> R.string.settings_src_mail_feishu_footer
                        MailAccountForm.Kind.IMAP -> R.string.settings_src_mail_imap_footer
                        MailAccountForm.Kind.GMAIL -> R.string.settings_src_mail_gmail_footer
                    }),
                    Modifier.padding(top = Tokens.Space.xs),
                )
            }
            if (form.kind == MailAccountForm.Kind.IMAP) {
                item("servers-title") { JSectionHeader(stringResource(R.string.settings_src_mail_servers_header)) }
                item("servers-fields") {
                    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        JTextField(
                            value = form.imapHost,
                            onValueChange = { form = form.copy(imapHost = it.trim()) },
                            label = stringResource(R.string.settings_src_mail_imap_server),
                            placeholder = "imap.example.com",
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        JTextField(
                            value = form.imapPort,
                            onValueChange = { form = form.copy(imapPort = it.filter(Char::isDigit)) },
                            label = stringResource(R.string.settings_src_mail_imap_port),
                            placeholder = "993",
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        JTextField(
                            value = form.smtpHost,
                            onValueChange = { form = form.copy(smtpHost = it.trim()) },
                            label = stringResource(R.string.settings_src_mail_smtp_server),
                            placeholder = "smtp.example.com",
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        JTextField(
                            value = form.smtpPort,
                            onValueChange = { form = form.copy(smtpPort = it.filter(Char::isDigit)) },
                            label = stringResource(R.string.settings_src_mail_smtp_port),
                            placeholder = "465",
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
            }
        }
        error?.let { err ->
            item("error") {
                JNotice(err, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
            }
        }
        item("ai-tip") {
            Row(
                Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.l),
                horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
            ) {
                JIcon(PhIcons.Sparkle, null, tint = PersonaTheme.palette.accent)
                Text(
                    stringResource(R.string.settings_src_mail_ai_tip),
                    style = PersonaTheme.type.caption,
                    color = PersonaTheme.palette.ink3,
                )
            }
        }
    }
}

/**
 * 设置 › 连接器 › 飞书 (iOS `FeishuConnectorView`): connection state of lark-connector on the hub,
 * signed-in accounts, and features powered by Feishu.
 */
@Composable
internal fun FeishuConnectorPage(
    connectors: ConnectorsManagement?,
    onBack: () -> Unit,
    onAgentModel: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    val res = LocalResources.current
    var status by remember { mutableStateOf<LarkStatus?>(null) }
    var accounts by remember { mutableStateOf<LarkAccounts?>(null) }
    var loading by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }

    fun load() {
        if (connectors == null) {
            loading = false
            return
        }
        scope.launch {
            loading = true
            try {
                status = connectors.larkStatus()
                accounts = connectors.larkAccounts()
                error = null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.message ?: res.getString(R.string.settings_src_lark_read_failed)
            } finally {
                loading = false
            }
        }
    }

    LaunchedEffect(connectors) { load() }
    BackHandler(onBack = onBack)

    val st = status
    val stateLabel = stringResource(when {
        st == null -> if (loading) R.string.settings_src_checking else R.string.settings_src_status_unreadable
        !st.configured -> R.string.settings_src_state_not_configured
        st.connected -> R.string.settings_src_state_connected
        else -> R.string.settings_src_lark_unreachable
    })
    val statusDot = when {
        st?.connected == true -> Status.Idle
        st?.configured == true -> Status.NeedsYou
        else -> Status.Offline
    }

    JListPage(title = stringResource(R.string.settings_src_lark_title), onBack = onBack) {
        item("status-header") { JSectionHeader(stringResource(R.string.settings_src_lark_status_header)) }
        item("status-row") {
            JGroupItem(GroupPosition.Single) {
                JRow(
                    title = st?.name?.takeIf { it.isNotBlank() } ?: stringResource(R.string.settings_src_lark_title),
                    subtitle = stateLabel,
                    leadingIcon = PhIcons.ChatCircleDots,
                    trailing = {
                        Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
                            JStatusDot(statusDot)
                            if (loading) JSpinner(stroke = 2.dp)
                        }
                    },
                )
            }
        }
        st?.lastEventAt?.takeIf { it > 0 }?.let { at ->
            item("last-event") {
                JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.s)) {
                    JRow(stringResource(R.string.settings_src_lark_last_event), value = agoText(res, Instant.ofEpochSecond(at), days = true))
                }
            }
        }
        item("status-caption") {
            SettingsCaption(
                stringResource(if (st?.configured == false) R.string.settings_src_lark_not_configured_caption else R.string.settings_src_lark_caption),
                Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
            )
        }

        val accs = accounts?.accounts.orEmpty()
        if (accs.size > 1) {
            item("accounts-header") { JSectionHeader(stringResource(R.string.settings_src_lark_accounts_header)) }
            itemsIndexed(accs, key = { _, a -> a.id }) { index, a ->
                val isDefault = a.id == accounts?.default
                val sub = when {
                    a.quarantined == true -> stringResource(R.string.settings_src_lark_quarantined)
                    !a.signedIn -> stringResource(R.string.settings_src_state_signed_out)
                    !a.tenant.isNullOrBlank() -> a.tenant
                    else -> stringResource(R.string.settings_src_signed_in)
                }
                JGroupItem(GroupPosition.of(index, accs.size)) {
                    JRow(
                        title = a.title,
                        subtitle = sub,
                        leading = { JAvatar(a.title) },
                        trailing = {
                            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
                                if (isDefault) JTag(stringResource(R.string.settings_src_default))
                                if (a.signedIn && a.unread > 0) JBadge(a.unread)
                            }
                        },
                    )
                }
            }
            item("accounts-caption") {
                SettingsCaption(
                    stringResource(R.string.settings_src_lark_accounts_caption),
                    Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
                )
            }
        }

        item("uses-header") { JSectionHeader(stringResource(R.string.settings_src_lark_uses_header)) }
        item("use-inbox") {
            JGroupItem(GroupPosition.of(0, 5)) {
                JRow(stringResource(R.string.settings_src_lark_use_inbox), subtitle = stringResource(R.string.settings_src_lark_use_inbox_detail), leadingIcon = PhIcons.EnvelopeSimple)
            }
        }
        item("use-kb") {
            JGroupItem(GroupPosition.of(1, 5)) {
                JRow(stringResource(R.string.settings_src_lark_use_kb), subtitle = stringResource(R.string.settings_src_lark_use_kb_detail), leadingIcon = PhIcons.BookOpen)
            }
        }
        item("use-assistant") {
            JGroupItem(GroupPosition.of(2, 5)) {
                JRow(stringResource(R.string.settings_src_lark_use_assistant), subtitle = stringResource(R.string.settings_src_lark_use_assistant_detail), leadingIcon = PhIcons.Sparkle)
            }
        }
        item("use-agent") {
            JGroupItem(GroupPosition.of(3, 5)) {
                JRow("/agent", subtitle = stringResource(R.string.settings_src_lark_use_agent_detail), leadingIcon = PhIcons.Robot)
            }
        }
        item("use-model") {
            JGroupItem(GroupPosition.of(4, 5)) {
                JRow(stringResource(R.string.settings_src_lark_agent_model), subtitle = stringResource(R.string.settings_src_lark_agent_model_detail), leadingIcon = PhIcons.Brain, onClick = onAgentModel)
            }
        }
        error?.let { err ->
            item("error") {
                JNotice(err, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
            }
        }
    }
}

/**
 * 设置 › 连接器 › 通用连接器 (iOS `HubConnectorView`, e.g. GitHub, Telegram).
 */
@Composable
internal fun HubConnectorPage(
    connectors: ConnectorsManagement?,
    connectorId: String,
    onBack: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    val res = LocalResources.current
    var connector by remember { mutableStateOf<HubConnector?>(null) }
    var loading by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }
    var confirmingDelete by remember { mutableStateOf(false) }

    fun load() {
        if (connectors == null) {
            loading = false
            return
        }
        scope.launch {
            loading = true
            try {
                val list = connectors.connectors(all = true)
                connector = list.firstOrNull { it.id == connectorId }
                error = null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.message ?: res.getString(R.string.settings_src_connector_read_failed)
            } finally {
                loading = false
            }
        }
    }

    LaunchedEffect(connectors, connectorId) { load() }
    BackHandler(onBack = onBack)

    val c = connector
    val title = c?.name ?: connectorId
    val stateLabel = c?.let { ConnectorText.state(res, it) } ?: stringResource(if (loading) R.string.settings_src_checking else R.string.settings_src_state_not_connected)
    val statusDot = c?.let { ConnectorText.status(it) } ?: Status.Offline

    JListPage(title = title, onBack = onBack) {
        if (loading && c == null) {
            item("loading") { JLoading() }
        } else if (c != null) {
            item("status-header") { JSectionHeader(stringResource(R.string.settings_src_status_header)) }
            item("status-row") {
                JGroupItem(GroupPosition.Single) {
                    JRow(
                        title = c.name,
                        subtitle = listOfNotNull(c.account, c.via).joinToString(" · ").ifEmpty { stateLabel },
                        leadingIcon = PhIcons.PlugsConnected,
                        trailing = {
                            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
                                JStatusDot(statusDot)
                                if (loading) JSpinner(stroke = 2.dp)
                            }
                        },
                    )
                }
            }
            c.error?.takeIf { it.isNotBlank() }?.let { err ->
                item("connector-error") {
                    JNotice(err, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
                }
            }
            c.guide?.takeIf { it.isNotEmpty() }?.let { guide ->
                item("guide") {
                    SettingsCaption(guide.joinToString("\n"), Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
                }
            }

            item("settings-header") { JSectionHeader(stringResource(R.string.settings_src_settings_header)) }
            item("enabled-switch") {
                JGroupItem(GroupPosition.Single) {
                    JSwitchRow(stringResource(R.string.settings_src_enable), checked = c.enabled ?: true, onCheckedChange = { want ->
                        scope.launch {
                            try {
                                connector = connectors?.setConnectorEnabled(c.id, want)
                            } catch (e: Exception) {
                                feedback?.notice(res.getString(R.string.settings_src_save_failed, e.message.orEmpty()))
                            }
                        }
                    })
                }
            }
            if (c.archive != null || c.inbox != null) {
                item("feeds-header") { JSectionHeader(stringResource(R.string.settings_src_feeds_header)) }
                val feedCount = listOfNotNull(c.archive, c.inbox).size
                var idx = 0
                if (c.archive != null) {
                    val p = GroupPosition.of(idx++, feedCount)
                    item("feed-archive") {
                        JGroupItem(p) {
                            JSwitchRow(stringResource(R.string.settings_src_feed_archive), checked = c.archive ?: false, onCheckedChange = { want ->
                                scope.launch {
                                    try {
                                        connector = connectors?.setConnectorFeed(c.id, "archive", want)
                                    } catch (e: Exception) {
                                        feedback?.notice(res.getString(R.string.settings_src_save_failed, e.message.orEmpty()))
                                    }
                                }
                            })
                        }
                    }
                }
                if (c.inbox != null) {
                    val p = GroupPosition.of(idx++, feedCount)
                    item("feed-inbox") {
                        JGroupItem(p) {
                            JSwitchRow(stringResource(R.string.settings_src_feed_inbox), checked = c.inbox ?: false, onCheckedChange = { want ->
                                scope.launch {
                                    try {
                                        connector = connectors?.setConnectorFeed(c.id, "inbox", want)
                                    } catch (e: Exception) {
                                        feedback?.notice(res.getString(R.string.settings_src_save_failed, e.message.orEmpty()))
                                    }
                                }
                            })
                        }
                    }
                }
            }

            if (c.capabilities?.contains("archive") == true || c.capabilities?.contains("inbox") == true) {
                item("danger-header") { JSectionHeader(stringResource(R.string.settings_src_data_header)) }
                item("delete-data") {
                    JGroupItem(GroupPosition.Single) {
                        JRow(stringResource(R.string.settings_src_delete_data), leadingIcon = PhIcons.Trash, destructive = true, onClick = { confirmingDelete = true })
                    }
                }
            }
        }
        error?.let { err ->
            item("error") {
                JNotice(err, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
            }
        }
    }

    if (confirmingDelete && c != null) {
        JDialog(
            title = stringResource(R.string.settings_src_delete_data_title, c.name),
            content = {
                Text(
                    stringResource(R.string.settings_src_delete_data_body),
                    style = PersonaTheme.type.callout,
                    color = PersonaTheme.palette.ink2,
                )
            },
            confirm = stringResource(R.string.settings_src_delete) to {
                confirmingDelete = false
                scope.launch {
                    try {
                        connectors?.deleteConnectorData(c.id)
                        feedback?.notice(res.getString(R.string.settings_src_data_deleted))
                        load()
                    } catch (e: Exception) {
                        feedback?.notice(res.getString(R.string.settings_src_delete_failed, e.message.orEmpty()))
                    }
                }
            },
            dismiss = stringResource(R.string.settings_src_cancel) to { confirmingDelete = false },
            onDismiss = { confirmingDelete = false },
        )
    }
}
