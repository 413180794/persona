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
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
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
                error = e.message ?: "读不到邮箱账户"
            } finally {
                loading = false
            }
        }
    }

    LaunchedEffect(connectors) { load() }
    BackHandler(onBack = onBack)

    JListPage(title = "邮箱", onBack = onBack) {
        val list = accounts
        when {
            loading && list == null -> item("loading") { JLoading() }
            list.isNullOrEmpty() -> {
                item("empty") {
                    JEmptyState(
                        title = "还没有邮箱",
                        description = error ?: "添加后，收件箱会把新邮件和飞书消息放在一起，Persona 也能帮你读信、起草回复。",
                        icon = PhIcons.EnvelopeSimple,
                    )
                }
            }
            else -> {
                item("accounts-header") { JSectionHeader("账户") }
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
                                    JIconButton(PhIcons.DotsThreeVertical, "更多", onClick = { actionAccount = a })
                                }
                            },
                        )
                    }
                }
            }
        }
        item("add-header") { JSectionHeader("添加") }
        item("add-button") {
            JGroupItem(GroupPosition.Single) {
                JRow("添加邮箱", leadingIcon = PhIcons.Plus, onClick = onAdd)
            }
        }
        item("add-caption") {
            SettingsCaption(
                "密码和授权只存在 hub 上（加密），App 里拿不到。",
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
                JAction("测试连接", onClick = {
                    val targetId = a.id
                    testingId = targetId
                    scope.launch {
                        try {
                            val r = connectors?.testMailAccount(targetId)
                            if (r?.ok == true) {
                                feedback?.notice("连接正常")
                            } else {
                                feedback?.notice("连不上：" + (r?.error ?: "未知错误"))
                            }
                            load()
                        } catch (e: Exception) {
                            feedback?.notice(e.message ?: "测试失败")
                        } finally {
                            testingId = null
                        }
                    }
                }, icon = PhIcons.PlugsConnected),
                JAction("删除", onClick = { deleting = a }, icon = PhIcons.Trash, destructive = true),
            ),
            onDismiss = { actionAccount = null },
            title = a.email,
        )
    }

    deleting?.let { a ->
        JDialog(
            title = "删除「${a.email}」？",
            content = {
                Text(
                    "Persona 不再收这个邮箱的信，保存的密码 / 授权一并删掉。邮箱里的邮件不受影响。",
                    style = PersonaTheme.type.callout,
                    color = PersonaTheme.palette.ink2,
                )
            },
            confirm = "删除" to {
                val targetId = a.id
                deleting = null
                scope.launch {
                    try {
                        connectors?.deleteMailAccount(targetId)
                        load()
                        feedback?.notice("已删除")
                    } catch (e: Exception) {
                        feedback?.notice("没删掉：${e.message}")
                    }
                }
            },
            dismiss = "取消" to { deleting = null },
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
                    feedback?.notice("已添加 ${f.account.email}")
                    onAdded()
                }
                is GoogleSignIn.Finished.Error -> {
                    error = f.error.message ?: "Google 登录失败"
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
                feedback?.notice("已添加 ${added.email}")
                onAdded()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.message ?: "没连上"
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
                gmailFallback = "这个安卓版本没有登记 hub 的 Google 回调地址，没法在手机上完成 Google 登录。" +
                    "可以先用 Gmail 的「应用专用密码」添加（Google 账号 › 安全性 › 两步验证 › 应用专用密码），或者在 iPhone 上用 Google 账号登录。"
            } catch (e: Exception) {
                val msg = e.message.orEmpty()
                if (msg.contains("not configured", ignoreCase = true)) {
                    gmailFallback = "hub 还没有配置 Google 登录（hub.json 的 google.client_id）。可以先用 Gmail 的「应用专用密码」添加。"
                } else {
                    error = msg.ifEmpty { "没能开始 Google 登录" }
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
        title = "添加邮箱",
        onBack = onBack,
        bottomBar = if (!gmailSignIn) {
            {
                Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s)) {
                    JButton("连接", ::save, Modifier.fillMaxWidth(), enabled = canConnect, loading = saving)
                }
            }
        } else null,
    ) {
        item("kind") {
            JSegmented(
                options = listOf(JSegment("Gmail"), JSegment("飞书"), JSegment("其他 IMAP")),
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
                        if (saving) "正在连接 Gmail…" else "使用 Google 账号登录",
                        onClick = ::signInWithGoogle,
                        enabled = !saving,
                        loading = saving,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    SettingsCaption(
                        "跳到 Google 选择账号并授权 Persona 收发邮件，不需要密码。授权随时可以在 Google 账号的「第三方应用和服务」里撤销；授权失效时再登录一次即可恢复。",
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
                        label = "邮箱地址",
                        placeholder = "name@example.com",
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, autoCorrectEnabled = false),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    JTextField(
                        value = form.password,
                        onValueChange = { form = form.copy(password = it) },
                        label = if (form.kind == MailAccountForm.Kind.GMAIL) "应用专用密码" else "密码 / 授权码",
                        placeholder = if (form.kind == MailAccountForm.Kind.GMAIL) "16 位应用专用密码" else "必填",
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            item("form-footer") {
                SettingsCaption(
                    when (form.kind) {
                        MailAccountForm.Kind.FEISHU -> "飞书邮箱：服务器固定为 imap.larkoffice.com:993、smtp.larkoffice.com:465（SSL），只填邮箱和密码。密码是飞书邮箱设置里的「第三方客户端登录密码」。"
                        MailAccountForm.Kind.IMAP -> "QQ / 163 / Outlook / iCloud 等会自动填服务器；国内邮箱一般要用「授权码」而不是登录密码。"
                        MailAccountForm.Kind.GMAIL -> "应用专用密码是 16 位，要先开两步验证。服务器会自动填 Gmail 的。"
                    },
                    Modifier.padding(top = Tokens.Space.xs),
                )
            }
            if (form.kind == MailAccountForm.Kind.IMAP) {
                item("servers-title") { JSectionHeader("服务器（可留空自动识别）") }
                item("servers-fields") {
                    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        JTextField(
                            value = form.imapHost,
                            onValueChange = { form = form.copy(imapHost = it.trim()) },
                            label = "IMAP 服务器",
                            placeholder = "imap.example.com",
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        JTextField(
                            value = form.imapPort,
                            onValueChange = { form = form.copy(imapPort = it.filter(Char::isDigit)) },
                            label = "IMAP 端口",
                            placeholder = "993",
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        JTextField(
                            value = form.smtpHost,
                            onValueChange = { form = form.copy(smtpHost = it.trim()) },
                            label = "SMTP 服务器",
                            placeholder = "smtp.example.com",
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        JTextField(
                            value = form.smtpPort,
                            onValueChange = { form = form.copy(smtpPort = it.filter(Char::isDigit)) },
                            label = "SMTP 端口",
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
                    "也可以直接在 Persona 里说「帮我添加邮箱」，把邮箱和密码告诉它，在手机上确认后就加好了。",
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
                error = e.message ?: "读不到飞书状态"
            } finally {
                loading = false
            }
        }
    }

    LaunchedEffect(connectors) { load() }
    BackHandler(onBack = onBack)

    val st = status
    val stateLabel = when {
        st == null -> if (loading) "正在检查…" else "读不到状态"
        !st.configured -> "未配置"
        st.connected -> "已连接"
        else -> "连不上 lark-connector"
    }
    val statusDot = when {
        st?.connected == true -> Status.Idle
        st?.configured == true -> Status.NeedsYou
        else -> Status.Offline
    }

    JListPage(title = "飞书", onBack = onBack) {
        item("status-header") { JSectionHeader("连接状态") }
        item("status-row") {
            JGroupItem(GroupPosition.Single) {
                JRow(
                    title = st?.name?.takeIf { it.isNotBlank() } ?: "飞书",
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
                    JRow("最近一条实时消息", value = formatRelativeTime(at))
                }
            }
        }
        item("status-caption") {
            SettingsCaption(
                if (st?.configured == false) {
                    "hub 还没接飞书：在 hub 所在的机器上运行 lark-connector，并在 hub.json 的 lark 里填上它（docs/feishu.md）。"
                } else {
                    "飞书账号在 hub 所在机器的 lark-connector 里登录，会话与收件箱共用这个账号，无需再次登录。"
                },
                Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
            )
        }

        val accs = accounts?.accounts.orEmpty()
        if (accs.size > 1) {
            item("accounts-header") { JSectionHeader("账号") }
            itemsIndexed(accs, key = { _, a -> a.id }) { index, a ->
                val isDefault = a.id == accounts?.default
                val sub = when {
                    a.quarantined == true -> "无法打开，请在 lark-connector 里重试或移除"
                    !a.signedIn -> "未登录"
                    !a.tenant.isNullOrBlank() -> a.tenant
                    else -> "已登录"
                }
                JGroupItem(GroupPosition.of(index, accs.size)) {
                    JRow(
                        title = a.title,
                        subtitle = sub,
                        leading = { JAvatar(a.title) },
                        trailing = {
                            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
                                if (isDefault) JTag("默认")
                                if (a.signedIn && a.unread > 0) JBadge(a.unread)
                            }
                        },
                    )
                }
            }
            item("accounts-caption") {
                SettingsCaption(
                    "已登录的账号都会收进收件箱，「消息」页可以切换账号。/agent、助手和知识库使用默认账号；在 lark-connector 里添加或退出账号。",
                    Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
                )
            }
        }

        item("uses-header") { JSectionHeader("Persona 用飞书做什么") }
        item("use-inbox") {
            JGroupItem(GroupPosition.of(0, 5)) {
                JRow("收件箱", subtitle = "私聊、@我、群消息和云文档里提到你的地方，AI 先分拣、起草回复", leadingIcon = PhIcons.EnvelopeSimple)
            }
        }
        item("use-kb") {
            JGroupItem(GroupPosition.of(1, 5)) {
                JRow("知识库", subtitle = "会话、人、群、项目归档下来，给助手和 AI 分身做上下文", leadingIcon = PhIcons.BookOpen)
            }
        }
        item("use-assistant") {
            JGroupItem(GroupPosition.of(2, 5)) {
                JRow("助手", subtitle = "Persona 能搜消息、读文档；发消息等写操作先在手机上问你", leadingIcon = PhIcons.Sparkle)
            }
        }
        item("use-agent") {
            JGroupItem(GroupPosition.of(3, 5)) {
                JRow("/agent", subtitle = "在飞书任何地方以 /agent 开头说一句，就起一个 Agent 处理并替你回复", leadingIcon = PhIcons.Robot)
            }
        }
        item("use-model") {
            JGroupItem(GroupPosition.of(4, 5)) {
                JRow("/agent 模型", subtitle = "设置 /agent 使用的模型", leadingIcon = PhIcons.Brain, onClick = onAgentModel)
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
                error = e.message ?: "读不到连接器信息"
            } finally {
                loading = false
            }
        }
    }

    LaunchedEffect(connectors, connectorId) { load() }
    BackHandler(onBack = onBack)

    val c = connector
    val title = c?.name ?: connectorId
    val stateLabel = c?.let { ConnectorText.state(it) } ?: if (loading) "正在检查…" else "未连接"
    val statusDot = c?.let { ConnectorText.status(it) } ?: Status.Offline

    JListPage(title = title, onBack = onBack) {
        if (loading && c == null) {
            item("loading") { JLoading() }
        } else if (c != null) {
            item("status-header") { JSectionHeader("状态") }
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

            item("settings-header") { JSectionHeader("设置") }
            item("enabled-switch") {
                JGroupItem(GroupPosition.Single) {
                    JSwitchRow("启用", checked = c.enabled ?: true, onCheckedChange = { want ->
                        scope.launch {
                            try {
                                connector = connectors?.setConnectorEnabled(c.id, want)
                            } catch (e: Exception) {
                                feedback?.notice("保存失败：${e.message}")
                            }
                        }
                    })
                }
            }
            if (c.archive != null || c.inbox != null) {
                item("feeds-header") { JSectionHeader("数据流") }
                val feedCount = listOfNotNull(c.archive, c.inbox).size
                var idx = 0
                if (c.archive != null) {
                    val p = GroupPosition.of(idx++, feedCount)
                    item("feed-archive") {
                        JGroupItem(p) {
                            JSwitchRow("进知识库", checked = c.archive ?: false, onCheckedChange = { want ->
                                scope.launch {
                                    try {
                                        connector = connectors?.setConnectorFeed(c.id, "archive", want)
                                    } catch (e: Exception) {
                                        feedback?.notice("保存失败：${e.message}")
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
                            JSwitchRow("进收件箱", checked = c.inbox ?: false, onCheckedChange = { want ->
                                scope.launch {
                                    try {
                                        connector = connectors?.setConnectorFeed(c.id, "inbox", want)
                                    } catch (e: Exception) {
                                        feedback?.notice("保存失败：${e.message}")
                                    }
                                }
                            })
                        }
                    }
                }
            }

            if (c.capabilities?.contains("archive") == true || c.capabilities?.contains("inbox") == true) {
                item("danger-header") { JSectionHeader("数据") }
                item("delete-data") {
                    JGroupItem(GroupPosition.Single) {
                        JRow("删除已存数据", leadingIcon = PhIcons.Trash, destructive = true, onClick = { confirmingDelete = true })
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
            title = "删除「${c.name}」已存数据？",
            content = {
                Text(
                    "将清除 Persona 从该连接器同步的所有历史数据。连接器本身不受影响。",
                    style = PersonaTheme.type.callout,
                    color = PersonaTheme.palette.ink2,
                )
            },
            confirm = "删除" to {
                confirmingDelete = false
                scope.launch {
                    try {
                        connectors?.deleteConnectorData(c.id)
                        feedback?.notice("已删除数据")
                        load()
                    } catch (e: Exception) {
                        feedback?.notice("删除失败：${e.message}")
                    }
                }
            },
            dismiss = "取消" to { confirmingDelete = false },
            onDismiss = { confirmingDelete = false },
        )
    }
}

private fun formatRelativeTime(epochSeconds: Long): String {
    val now = Instant.now().epochSecond
    val diff = (now - epochSeconds).coerceAtLeast(0)
    return when {
        diff < 60 -> "刚刚"
        diff < 3600 -> "${diff / 60} 分钟前"
        diff < 86400 -> "${diff / 3600} 小时前"
        else -> "${diff / 86400} 天前"
    }
}
