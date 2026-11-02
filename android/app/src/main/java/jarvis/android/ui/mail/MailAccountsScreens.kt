package jarvis.android.ui.mail

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.repeatOnLifecycle
import jarvis.android.data.ApiClient
import jarvis.android.mail.GoogleSignIn
import jarvis.android.mail.MailAccount
import jarvis.android.mail.MailAccountForm
import jarvis.android.mail.MailStore
import jarvis.android.mail.addMailAccount
import jarvis.android.mail.deleteMailAccount
import jarvis.android.mail.testMailAccount
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.GroupFooter
import jarvis.android.ui.design.IconAction
import jarvis.android.ui.design.IconTile
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.SegmentedControl
import jarvis.android.ui.settings.KitActionRow
import jarvis.android.ui.settings.KitEmpty
import jarvis.android.ui.settings.KitGroup
import jarvis.android.ui.settings.KitLoadingRow
import jarvis.android.ui.settings.KitPage
import jarvis.android.ui.settings.KitRow
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** An account's mark: Gmail's red, Feishu's blue, the accent for any other IMAP server (iOS `MailProviderMark`). */
@Composable
fun MailProviderMark(provider: String?) {
    val brand = JTheme.brand
    when (provider) {
        "gmail" -> IconTile(JIcons.mail, brand.gmail)
        "feishu" -> IconTile(JIcons.mail, brand.feishu)
        else -> IconTile(JIcons.mail, JTheme.colors.accent)
    }
}

/**
 * 设置 › 连接器 › 邮箱 (iOS `MailAccountsView`): every account on the hub with its server or its error; per row
 * 测试连接 and 删除 (asks first); 添加邮箱 opens [AddMailAccountScreen].
 */
@Composable
fun MailAccountsScreen(api: ApiClient, onBack: () -> Unit, onAdd: () -> Unit, preloaded: Boolean = false) {
    val c = JTheme.colors
    val scope = rememberCoroutineScope()
    val accounts by MailStore.accounts.collectAsState()
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<MailAccount?>(null) }
    var testing by remember { mutableStateOf<String?>(null) }

    suspend fun load() {
        try {
            MailStore.reloadAccounts(api)
            error = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = e.message ?: "读不到邮箱账号"
        }
    }
    LaunchedEffect(Unit) { if (!preloaded) load() }

    KitPage("邮箱", onBack) {
        val list = accounts
        when {
            list == null && error == null -> item { KitLoadingRow() }
            list.isNullOrEmpty() -> item {
                KitEmpty(JIcons.mail, "还没有邮箱", error ?: "添加后，收件箱会把新邮件和飞书消息放在一起，Jarvis 也能帮你读信、起草回复。")
            }
            else -> item(key = "accounts") {
                KitGroup(header = "账户", separatorStart = JSpace.l + JSize.wellS + JSpace.m, footer = error) {
                    list.forEach { a ->
                        row {
                            AccountRow(a, testing = testing == a.id, onTest = {
                                testing = a.id
                                scope.launch {
                                    runCatching { api.testMailAccount(a.id) }
                                        .onSuccess { r ->
                                            if (r.ok) Toasts.show("连接正常", ToastStyle.SUCCESS)
                                            else Toasts.show("连不上：" + (r.error ?: "未知错误"), ToastStyle.ERROR)
                                            load()
                                        }
                                        .onFailure { Toasts.show(it.message ?: "测试失败", ToastStyle.ERROR) }
                                    testing = null
                                }
                            }, onDelete = { deleting = a })
                        }
                    }
                }
            }
        }
        item(key = "add") {
            KitGroup(footer = "密码和授权只存在 hub 上（加密），App 里拿不到。") {
                row { KitActionRow("添加邮箱", onClick = onAdd, icon = JIcons.addFilled) }
            }
        }
    }

    deleting?.let { a ->
        AlertDialog(
            onDismissRequest = { deleting = null },
            title = { Text("删除「${a.email}」？") },
            text = { Text("Jarvis 不再收这个邮箱的信，保存的密码 / 授权一并删掉。邮箱里的邮件不受影响。") },
            confirmButton = {
                TextButton(onClick = {
                    deleting = null
                    scope.launch {
                        runCatching { api.deleteMailAccount(a.id) }
                            .onSuccess { load() }
                            .onFailure { Toasts.show("没删掉：${it.message}", ToastStyle.ERROR) }
                    }
                }) { Text("删除", color = c.danger, maxLines = 1) }
            },
            dismissButton = { TextButton(onClick = { deleting = null }) { Text("取消", maxLines = 1) } },
        )
    }
}

@Composable
private fun AccountRow(a: MailAccount, testing: Boolean, onTest: () -> Unit, onDelete: () -> Unit) {
    val c = JTheme.colors
    var menu by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.box56).padding(start = JSpace.l, end = JSpace.xs, top = JSpace.s, bottom = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        MailProviderMark(a.provider)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(a.email, style = JTheme.type.body, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(a.detail, style = JTheme.type.caption, color = if (a.isError) c.danger else c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        Box {
            if (testing) Box(Modifier.size(JSize.tapTarget), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(Modifier.size(JSize.markL), strokeWidth = JSize.rule, color = c.secondaryText)
            } else IconAction(JIcons.more, "更多", { menu = true }, tint = c.secondaryText)
            DropdownMenu(menu, onDismissRequest = { menu = false }) {
                DropdownMenuItem(text = { Text("测试连接", maxLines = 1) }, leadingIcon = { Icon(JIcons.testConnection, null) },
                    onClick = { menu = false; onTest() })
                DropdownMenuItem(text = { Text("删除", color = c.danger, maxLines = 1) }, leadingIcon = { Icon(JIcons.delete, null, tint = c.danger) },
                    onClick = { menu = false; onDelete() })
            }
        }
    }
}

/**
 * 添加邮箱 (iOS `AddMailAccountView`, docs/mail.md): Gmail (Google sign-in) / 飞书 (address + password, fixed servers) /
 * 其他 IMAP (address, password or 授权码, servers optional). The hub logs in before it saves. 「AI 一键添加」 is gone
 * (2026-11-01): the page points to asking Jarvis instead.
 *
 * Gmail on Android: Google sign-in works when this build catches the hub's redirect scheme ([GoogleSignIn]); when it
 * does not (or the hub has no Google client) the page says so and offers Gmail with an app password, which the hub
 * still takes (`auth: password`).
 */
@Composable
fun AddMailAccountScreen(api: ApiClient, onBack: () -> Unit, onAdded: () -> Unit, start: MailAccountForm = MailAccountForm()) {
    val c = JTheme.colors
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var form by rememberSaveable(stateSaver = MailAccountForm.Saver) { mutableStateOf(start) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    /** Google sign-in is not possible with this build / hub: Gmail takes an app password instead. */
    var gmailFallback by rememberSaveable { mutableStateOf<String?>(null) }

    // Back from the browser without a redirect (closed it): stop waiting.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            if (GoogleSignIn.isWaiting) {
                delay(1500)
                if (GoogleSignIn.isWaiting) GoogleSignIn.cancel()
            }
        }
    }
    // A Google sign-in that finished, even one started before this page was recreated.
    LaunchedEffect(Unit) {
        GoogleSignIn.finished.collect {
            when (val f = GoogleSignIn.takeFinished()) {
                is GoogleSignIn.Finished.Added -> {
                    runCatching { MailStore.reloadAccounts(api) }
                    Toasts.show("已添加 ${f.account.email}", ToastStyle.SUCCESS)
                    onAdded()
                }
                is GoogleSignIn.Finished.Error -> error = f.error.message
                null -> Unit
            }
        }
    }

    fun save() {
        saving = true
        error = null
        scope.launch {
            try {
                val req = form.request()
                api.addMailAccount(req)
                runCatching { MailStore.reloadAccounts(api) }
                Toasts.show("已添加 ${req.email}", ToastStyle.SUCCESS)
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
        saving = true
        error = null
        scope.launch {
            try {
                // The outcome arrives through GoogleSignIn.finished (collected above), also after a recreation.
                GoogleSignIn.signIn(ctx, api, form.email.takeIf { it.contains('@') })
            } catch (e: CancellationException) {
                throw e
            } catch (_: UnsupportedOperationException) {
                gmailFallback = "这个安卓版本没有登记 hub 的 Google 回调地址，没法在手机上完成 Google 登录。" +
                    "可以先用 Gmail 的「应用专用密码」添加（Google 账号 › 安全性 › 两步验证 › 应用专用密码），或者在 iPhone 上用 Google 账号登录。"
            } catch (e: Exception) {
                val msg = e.message.orEmpty()
                if (msg.contains("not configured", ignoreCase = true)) {
                    gmailFallback = "hub 还没有配置 Google 登录（hub.json 的 google.client_id）。可以先用 Gmail 的「应用专用密码」添加。"
                } else error = msg.ifEmpty { "没能开始 Google 登录" }
            } finally {
                saving = false
            }
        }
    }

    val gmailSignIn = form.kind == MailAccountForm.Kind.GMAIL && gmailFallback == null
    val canConnect = form.canConnect(gmailFallback != null) && !saving
    KitPage("添加邮箱", onBack, actions = {
        if (!gmailSignIn) {
            if (saving) CircularProgressIndicator(Modifier.size(JSize.iconXS), strokeWidth = JSize.rule, color = c.accent)
            else PillButton("连接", ::save, kind = ButtonKind.Prominent, enabled = canConnect)
        }
    }) {
        item(key = "kind") {
            SegmentedControl(
                MailAccountForm.Kind.entries.map { it.title }, form.kind.ordinal,
                { form = form.copy(kind = MailAccountForm.Kind.entries[it]); error = null },
                Modifier.padding(horizontal = JSpace.page, vertical = JSpace.s),
            )
        }
        if (gmailSignIn) {
            item(key = "google") {
                Column(Modifier.padding(top = JSpace.s)) {
                    GoogleButton(if (saving) "正在连接 Gmail…" else "使用 Google 账号登录", busy = saving, onClick = ::signInWithGoogle,
                        modifier = Modifier.padding(horizontal = JSpace.page))
                    GroupFooter("跳到 Google 选择账号并授权 Jarvis 收发邮件，不需要密码。授权随时可以在 Google 账号的「第三方应用和服务」里撤销；授权失效时再登录一次即可恢复。")
                }
            }
        } else {
            gmailFallback?.takeIf { form.kind == MailAccountForm.Kind.GMAIL }?.let { why ->
                item(key = "fallback") {
                    KitGroup { row { jarvis.android.ui.settings.KitNoticeRow("用应用专用密码添加", why, icon = JIcons.info) } }
                }
            }
            item(key = "login") {
                KitGroup(
                    footer = when (form.kind) {
                        MailAccountForm.Kind.FEISHU -> "飞书邮箱：服务器固定为 imap.larkoffice.com:993、smtp.larkoffice.com:465（SSL），只填邮箱和密码。密码是飞书邮箱设置里的「第三方客户端登录密码」。"
                        MailAccountForm.Kind.IMAP -> "QQ / 163 / Outlook / iCloud 等会自动填服务器；国内邮箱一般要用「授权码」而不是登录密码。"
                        MailAccountForm.Kind.GMAIL -> "应用专用密码是 16 位，要先开两步验证。服务器会自动填 Gmail 的。"
                    },
                ) {
                    row { FormField("邮箱地址", form.email, { form = form.copy(email = it.trim()) }, KeyboardType.Email, hint = "name@example.com") }
                    row {
                        FormField(if (form.kind == MailAccountForm.Kind.GMAIL) "应用专用密码" else "密码 / 授权码", form.password,
                            { form = form.copy(password = it) }, KeyboardType.Password, secret = true, hint = "必填")
                    }
                }
            }
            if (form.kind == MailAccountForm.Kind.IMAP) {
                item(key = "servers") {
                    KitGroup(header = "服务器（可留空自动识别）") {
                        row { FormField("IMAP 服务器", form.imapHost, { form = form.copy(imapHost = it.trim()) }, KeyboardType.Uri, hint = "imap.example.com") }
                        row { FormField("IMAP 端口", form.imapPort, { form = form.copy(imapPort = it.filter(Char::isDigit)) }, KeyboardType.Number) }
                        row { FormField("SMTP 服务器", form.smtpHost, { form = form.copy(smtpHost = it.trim()) }, KeyboardType.Uri, hint = "smtp.example.com") }
                        row { FormField("SMTP 端口", form.smtpPort, { form = form.copy(smtpPort = it.filter(Char::isDigit)) }, KeyboardType.Number) }
                    }
                }
            }
        }
        error?.let { e -> item(key = "error") { ErrorLine(e, Modifier.padding(horizontal = JSpace.page + JSpace.l, vertical = JSpace.m)) } }
        item(key = "ask") {
            Row(
                Modifier.fillMaxWidth().padding(horizontal = JSpace.page + JSpace.l, vertical = JSpace.l),
                horizontalArrangement = Arrangement.spacedBy(JSpace.s),
            ) {
                Icon(JIcons.ai, null, Modifier.padding(top = JSpace.xxs).size(JSize.mark), tint = c.ai)
                Text("也可以直接在 Jarvis 里说「帮我添加邮箱」，把邮箱和密码告诉它，在手机上确认后就加好了。",
                    style = JTheme.type.caption, color = c.secondaryText)
            }
        }
    }
}

/** A label and its field in a group row (the address, the password, a server). */
@Composable
private fun FormField(label: String, value: String, onValue: (String) -> Unit, type: KeyboardType, secret: Boolean = false, hint: String = "") {
    Column(Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        Text(label, style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1)
        if (secret) JTextField(value, onValue, hint, singleLine = true, password = true)
        else JTextField(value, onValue, hint, KeyboardOptions(keyboardType = type, autoCorrectEnabled = false), singleLine = true)
    }
}

/** Google's sign-in button as its guidelines draw it: a capsule, white (near-black in dark) with a gray outline. */
@Composable
private fun GoogleButton(text: String, busy: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val b = JTheme.brand
    Row(
        modifier.fillMaxWidth().heightIn(min = JSize.box50).clip(CircleShape).background(b.googleFill)
            .border(BorderStroke(JSize.line, b.googleStroke), CircleShape)
            .clickable(enabled = !busy, role = Role.Button, onClick = onClick).padding(horizontal = JSpace.xl),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m, Alignment.CenterHorizontally),
    ) {
        if (busy) CircularProgressIndicator(Modifier.size(JSize.markL), strokeWidth = JSize.rule, color = b.googleText)
        else Text("G", style = JTheme.type.heading, color = b.googleText, maxLines = 1)
        Text(text, style = JTheme.type.heading, color = b.googleText, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}
