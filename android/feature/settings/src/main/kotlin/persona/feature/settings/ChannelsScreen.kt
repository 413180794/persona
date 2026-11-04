package persona.feature.settings

import android.content.ClipData
import android.content.ClipboardManager
import android.content.ContentValues
import android.content.Context
import android.graphics.Bitmap
import android.provider.MediaStore
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.navigation3.runtime.NavKey
import persona.core.contracts.api.BindPrompt
import persona.core.contracts.api.ChannelDetail
import persona.core.contracts.api.ChannelManagement
import persona.core.contracts.api.HubChannel
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.ChannelRoute
import persona.core.navigation.LocalDetailKey
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * 设置 › 扩展 › 渠道 (docs/channels.md「界面」): every way of talking to Persona in three sections — Persona's own
 * apps, the ones installed (connected or not), and the ones an extension market offers (opening the market's page).
 */
@Composable
internal fun ChannelsPage(
    channels: ChannelManagement?, onBack: () -> Unit, onChannel: (String) -> Unit,
    onMarketExtension: ((market: String, extension: String) -> Unit)? = null,
) {
    if (channels == null) {
        JListPage(title = "渠道", onBack = onBack) { item { JNotice("体验模式没有 hub 的渠道。") } }
        return
    }
    val model = remember(channels) { ChannelListModel(channels) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(model) { model.load() }
    val list = state.channels
    val detail = LocalDetailKey.current
    JListPage(title = "渠道", onBack = onBack, refreshing = state.loading && list != null, onRefresh = { scope.launch { model.load() } }) {
        state.error?.let { error ->
            item("error") {
                JNotice(error, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou,
                    actionLabel = "重试", onAction = { scope.launch { model.load() } })
            }
        }
        if (list == null && state.loading) item("loading") { JLoading() }
        if (list != null && list.isEmpty()) item("empty") {
            JEmptyState("没有渠道", "这台 hub 还没有可以和 Persona 对话的入口。", icon = PhIcons.ChatsCircle)
        }
        if (list != null && list.isNotEmpty()) item("intro") {
            ChannelNote("和 Persona 对话的入口：Persona 自己的各端、微信、飞书机器人……在这里绑定、开关、选消息发到哪段对话。")
        }
        state.sections.forEach { section ->
            item("section-${section.title}") { JSectionHeader(section.title, count = section.channels.size.takeIf { section.channels.size > 1 }) }
            itemsIndexed(section.channels, key = { _, it -> "channel-${it.id}" }) { index, channel ->
                val market = channel.source == "market"
                val open: (() -> Unit)? = if (!market) ({ onChannel(channel.id) })
                    else onMarketExtension?.let { go -> channel.market?.takeIf { it.isNotBlank() }?.let { m ->
                        channel.extension?.takeIf { it.isNotBlank() }?.let { p -> { go(m, p) } } } }
                JGroupItem(GroupPosition.of(index, section.channels.size)) {
                    JRow(channel.label, subtitle = ChannelText.subtitle(channel), leading = { JChannelMark(channel) },
                        trailing = { ChannelStateMark(channel) }, chevron = open != null, enabled = !market || open != null,
                        selected = detail == ChannelRoute(channel.id), onClick = open)
                }
            }
            item("footer-${section.title}") { ChannelNote(section.footer) }
        }
    }
}

/** 设置's 渠道 row, under 扩展: how many are connected, in the danger colour with the broken ones when any went wrong. */
@Composable
internal fun ChannelsSettingsRow(list: List<HubChannel>?, position: GroupPosition, selected: Boolean, onClick: () -> Unit) {
    val summary = ChannelText.listSummary(list)
    val problem = ChannelText.hasProblem(list)
    JGroupItem(position) {
        JRow(selected = selected, title = "渠道", leadingIcon = PhIcons.ChatsCircle, subtitle = "微信、飞书机器人等对话入口",
            onClick = onClick, chevron = true, value = summary.takeUnless { problem },
            trailing = summary.takeIf { problem }?.let { text -> { JStateLabel(text, Status.Idle, problem = true) } })
    }
}

/** The state on a row's right: the style's dot and the words, in the danger colour when it went wrong. */
@Composable
private fun ChannelStateMark(channel: HubChannel) =
    JStateLabel(ChannelText.state(channel), ChannelText.status(channel), problem = ChannelText.isProblem(channel))

@Composable
private fun ChannelNote(text: String) = SettingsCaption(text, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))

/**
 * 设置 › 扩展 › 渠道 › one: how it is doing; its switch; binding it (a step at a time: fields, a QR code to scan, a
 * code to send to the bot) or undoing that; where its messages go; its features; a test message; and its own fuller
 * page when Android has one ([onOpen]). Persona's own apps, /agent in Feishu and voice are set up elsewhere: only shown.
 */
@Composable
internal fun ChannelPage(channels: ChannelManagement?, channelId: String, onBack: () -> Unit, onOpen: ((NavKey) -> Unit)? = null) {
    if (channels == null) {
        JListPage(title = "渠道", onBack = onBack) { item { JNotice("体验模式没有 hub 的渠道。") } }
        return
    }
    val model = remember(channels, channelId) { ChannelModel(channels, channelId) }
    val state by model.state.collectAsState()
    val assistant by remember(channels) { channels.conversations }.collectAsState(emptyList())
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    var picking by remember { mutableStateOf(false) }
    var unbinding by remember { mutableStateOf(false) }
    LaunchedEffect(model) { model.load() }
    // A code on screen: ask the hub again on its own until it is scanned or sent (and the page is open).
    val polled = state.bind.prompt?.takeIf { it.polled }
    LaunchedEffect(model, polled?.step, polled?.kind, polled?.userCode, polled?.url) { if (polled != null) model.poll() }
    LaunchedEffect(state.boundAs) {
        val who = state.boundAs ?: return@LaunchedEffect
        feedback?.notice(if (who.isBlank()) "已绑定" else "已绑定 $who")
        model.boundShown()
    }
    val detail = state.detail
    val channel = detail?.channel
    JListPage(title = channel?.label ?: "渠道", onBack = onBack, largeTitle = false,
        refreshing = state.loading && detail != null, onRefresh = { scope.launch { model.load() } }) {
        header(channelId, state, onRetry = { scope.launch { model.load() } })
        if (detail != null && channel != null) {
            val editable = ChannelText.editable(channel)
            if (editable) item("enabled") {
                Column {
                    JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
                        JSwitchRow("启用", channel.enabled, { on -> scope.launch { model.setEnabled(on) } }, enabled = state.busy == null)
                    }
                    ChannelNote(if (channel.source == "plugin") "关掉后它不再收消息；绑定和设置留着。" else "关掉后它不再收消息，也不再发提问和推送。")
                }
            }
            if (ChannelText.binds(channel)) binding(channel, state, model, onUnbind = { unbinding = true }, scope = scope)
            if (editable && channel.target != null) target(channel, onPick = { picking = true }, busy = state.busy != null)
            if (editable && channel.features.isNotEmpty()) features(channel, state, model, scope)
            recent(detail)
            abilities(channel)
            if (editable && channel.state == "connected") item("test") {
                Column(Modifier.padding(top = Tokens.Space.l), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                    JButton("发一条测试消息", { scope.launch { if (model.test()) feedback?.notice("测试消息发到${channel.label}了") } },
                        Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, icon = PhIcons.PaperPlaneTilt,
                        loading = state.busy == ChannelBusy.Test, enabled = state.busy == null)
                    ChannelNote("只发给绑定的你自己。")
                }
            }
            val more = ChannelText.settingsDestination(detail.settingsUrl)
            if (more != null && onOpen != null) item("more") {
                Column {
                    JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.l)) {
                        JRow("更多设置", leadingIcon = PhIcons.SlidersHorizontal, onClick = { onOpen(more) })
                    }
                    if (!editable) ChannelNote("这个渠道的设置在别处。")
                }
            }
        }
    }
    if (picking && detail != null) {
        val options = remember(detail, assistant) { targetOptions(detail, assistant) }
        JActionSheet(options.map { option ->
            JAction(option.label, checked = option.selected, onClick = { scope.launch { model.setTarget(option.target) } })
        }, { picking = false }, title = "消息发到", note = "在聊天里发 /new 开新对话、/main 回到主对话，也会改这里。")
    }
    if (unbinding && channel != null) JDialog(
        onDismiss = { unbinding = false }, title = "解除和${channel.label}的绑定？",
        text = "之后${channel.label}里发来的消息不再交给 Persona，在等的回答和提问也一起清掉。要用再重新绑定。",
        destructive = true, confirmEnabled = state.busy == null,
        confirm = "解除绑定" to { scope.launch { if (model.unbind()) { unbinding = false; feedback?.notice("已解除绑定") } }; Unit },
    )
}

private fun LazyListScope.header(id: String, state: ChannelState, onRetry: () -> Unit) {
    val channel = state.channel
    item("header") {
        JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.s)) {
            JRow(channel?.label ?: id, subtitle = if (channel != null) ChannelText.headerLine(channel).ifEmpty { null } else if (state.loading) "正在读取…" else "读不到",
                leading = { if (channel != null) JChannelMark(channel, size = MarkSize.Header) else JSourceMark(id, size = MarkSize.Header) },
                trailing = { if (channel == null && state.loading) JSpinner() else if (channel != null && channel.kind != "app") ChannelStateMark(channel) })
        }
    }
    channel?.description?.takeIf { it.isNotBlank() }?.let { item("description") { ChannelNote(it) } }
    if (channel != null && ChannelText.isProblem(channel)) item("channel-error") {
        Column(Modifier.padding(top = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            JInlineStatus(channel.error?.takeIf { it.isNotBlank() } ?: "出错了", tone = InlineTone.Failed)
            if (ChannelText.binds(channel)) ChannelNote("停用再启用试试；还不行就解除绑定后重新绑定。")
        }
    }
    state.error?.let { error ->
        item("error") {
            Column(Modifier.padding(top = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JInlineStatus(error, tone = InlineTone.Failed)
                if (channel == null) JButton("重试", onRetry, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, enabled = !state.loading)
            }
        }
    }
}

private fun LazyListScope.binding(
    channel: HubChannel, state: ChannelState, model: ChannelModel, onUnbind: () -> Unit, scope: kotlinx.coroutines.CoroutineScope,
) {
    val bind = state.bind
    val prompt = bind.prompt
    item("bind-title") { JSectionHeader(if (prompt != null) ChannelText.bindTitle(channel) else "绑定") }
    when {
        ChannelText.isBound(channel) -> item("bound") {
            Column {
                JGroupItem(GroupPosition.of(0, 2)) { JRow("绑定的账号", value = channel.account?.name?.takeIf { it.isNotBlank() } ?: "已绑定") }
                JGroupItem(GroupPosition.of(1, 2)) {
                    JRow("解除绑定", leadingIcon = PhIcons.X, destructive = true, enabled = state.busy == null, onClick = onUnbind, chevron = false)
                }
                ChannelNote("只有绑定的这个人发的消息会交给 Persona，别人发来只回一句「只为绑定它的人服务」。")
            }
        }
        !channel.enabled -> item("bind-off") { ChannelNote("先打开上面的「启用」，再绑定。") }
        prompt == null -> item("bind-start") {
            Column(Modifier.padding(top = Tokens.Space.xs), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                JButton(ChannelText.bindTitle(channel), { scope.launch { model.startBind() } }, Modifier.fillMaxWidth(),
                    icon = if (channel.bind == "qr") PhIcons.QrCode else PhIcons.LinkSimple, loading = bind.sending)
                bind.error?.let { JInlineStatus(it, tone = InlineTone.Failed) }
                ChannelNote(if (channel.bind == "qr") "用手机上的${channel.label}扫码，确认后这里自动继续。" else "绑定以后，只有你发给机器人的消息会交给 Persona。")
            }
        }
        else -> item("bind-step-${prompt.step}-${prompt.kind}") { BindStep(channel, prompt, bind, model) }
    }
}

/** One binding step: its hint and the hub's word on it, then a QR code, a code, or fields to fill in. */
@Composable
private fun BindStep(channel: HubChannel, prompt: BindPrompt, bind: BindState, model: ChannelModel) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val feedback = LocalAppFeedback.current
    val uri = LocalUriHandler.current
    Column(Modifier.fillMaxWidth().padding(top = Tokens.Space.xs), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        // Words line up with the notes and rows' text (the style's row padding); the buttons span the page like the others.
        prompt.hint?.takeIf { it.isNotBlank() }?.let {
            Text(it, Modifier.padding(horizontal = rowPadding), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
        }
        prompt.error?.takeIf { it.isNotBlank() }?.let { JInlineStatus(it, tone = InlineTone.Failed) }
        bind.error?.let { JInlineStatus(it, tone = InlineTone.Failed) }
        val rows = prompt.qr?.rows.orEmpty()
        when {
            prompt.kind == "qr" && rows.isNotEmpty() -> {
                JQrCode(rows, Modifier.align(Alignment.CenterHorizontally), label = "绑定${channel.label}的二维码")
                Waiting("等你在${channel.label}里扫码确认…")
                Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    JButton("保存二维码", {
                        scope.launch {
                            val saved = saveQrImage(context, rows, channel.label)
                            feedback?.notice(if (saved) "二维码存到相册了，在${channel.label}里从相册扫码" else "没存上二维码")
                        }
                    }, Modifier.weight(1f), kind = JButtonKind.Secondary, icon = PhIcons.Image)
                    JButton("取消", model::cancelBind, Modifier.weight(1f), kind = JButtonKind.Secondary)
                }
            }
            prompt.kind == "pair_code" || prompt.kind == "device_code" -> {
                val code = prompt.userCode.orEmpty()
                Row(Modifier.padding(start = rowPadding), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                    JCodeDisplay(code, Modifier.weight(1f))
                    JIconButton(PhIcons.Copy, "复制配对码", {
                        context.getSystemService(ClipboardManager::class.java)?.setPrimaryClip(ClipData.newPlainText("配对码", code))
                        feedback?.notice("已复制")
                    }, enabled = code.isNotEmpty())
                }
                Waiting(if (prompt.kind == "pair_code") "等你在${channel.label}里发给机器人…" else "等你授权…")
                Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    prompt.url?.takeIf { prompt.kind == "device_code" && it.startsWith("https://") }?.let { url ->
                        JButton("打开网页", { runCatching { uri.openUri(url) } }, Modifier.weight(1f), kind = JButtonKind.Secondary, icon = PhIcons.ArrowSquareOut)
                    }
                    JButton("取消", model::cancelBind, Modifier.weight(1f), kind = JButtonKind.Secondary)
                }
            }
            prompt.polled -> {
                // A step to wait on with nothing the app can draw (a QR step whose code did not come): it still goes on.
                Waiting("等你在${channel.label}里确认…")
                JButton("取消", model::cancelBind, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary)
            }
            else -> {
                prompt.fields.forEach { field ->
                    JTextField(bind.values[field.name].orEmpty(), { model.setValue(field.name, it) }, Modifier.fillMaxWidth(),
                        label = field.label.ifBlank { field.name }, placeholder = field.placeholder?.takeIf { it.isNotBlank() } ?: field.label,
                        singleLine = true, enabled = !bind.sending,
                        visualTransformation = if (field.secret) PasswordVisualTransformation() else VisualTransformation.None,
                        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false,
                            keyboardType = if (field.secret) KeyboardType.Password else KeyboardType.Ascii))
                }
                Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    JButton("下一步", { scope.launch { model.submit() } }, Modifier.weight(1f), loading = bind.sending,
                        loadingText = "正在验证", enabled = bind.canSubmit)
                    JButton("取消", model::cancelBind, Modifier.weight(1f), kind = JButtonKind.Secondary, enabled = !bind.sending)
                }
            }
        }
    }
}

/** A spinner and what the page is waiting for, on one line. */
@Composable
private fun Waiting(text: String) {
    Row(Modifier.padding(horizontal = rowPadding), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        JSpinner()
        Text(text, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

private fun LazyListScope.target(channel: HubChannel, onPick: () -> Unit, busy: Boolean) {
    item("target") {
        Column {
            JSectionHeader("消息发到")
            JGroupItem(GroupPosition.Single) {
                JRow("发到", leadingIcon = PhIcons.ChatCircle, value = ChannelText.targetText(channel.target), enabled = !busy, onClick = onPick)
            }
            ChannelNote("在聊天里发 /new 开新对话、/main 回到主对话，也会改这里。")
        }
    }
}

private fun LazyListScope.features(channel: HubChannel, state: ChannelState, model: ChannelModel, scope: kotlinx.coroutines.CoroutineScope) {
    item("features-title") { JSectionHeader("功能") }
    itemsIndexed(channel.features, key = { _, it -> "feature-${it.id}" }) { index, feature ->
        JGroupItem(GroupPosition.of(index, channel.features.size)) {
            JSwitchRow(feature.title.ifBlank { feature.id }, feature.on, { on -> scope.launch { model.setFeature(feature.id, on) } },
                subtitle = feature.description?.takeIf { it.isNotBlank() }, enabled = state.busy == null)
        }
    }
}

private fun LazyListScope.recent(detail: ChannelDetail) {
    val waiting = ChannelText.waiting(detail)
    val conversations = detail.conversations
    if (waiting == null && conversations.isEmpty()) return
    val count = conversations.size + if (waiting != null) 1 else 0
    item("recent-title") { JSectionHeader("最近") }
    waiting?.let { item("waiting") { JGroupItem(GroupPosition.of(0, count)) { JRow("在等", leadingIcon = PhIcons.HourglassMedium, value = it) } } }
    val offset = if (waiting != null) 1 else 0
    itemsIndexed(conversations, key = { _, it -> "conversation-${it.machineId}/${it.sessionId}" }) { index, conversation ->
        JGroupItem(GroupPosition.of(index + offset, count)) {
            JRow(conversation.title?.takeIf { it.isNotBlank() } ?: "一段对话", leadingIcon = PhIcons.ChatCircle,
                subtitle = "消息进过这段对话" + (persona.core.contracts.parseTime(conversation.at)?.let { " · " + ChannelText.ago(it) } ?: ""))
        }
    }
}

private fun LazyListScope.abilities(channel: HubChannel) {
    val lines = ChannelText.capabilities(channel.capabilities)
    val surfaces = channel.surfaces.takeIf { it.isNotEmpty() }
    val count = lines.size + if (surfaces != null) 1 else 0
    item("abilities-title") { JSectionHeader("它能做什么") }
    itemsIndexed(lines, key = { _, it -> "ability-$it" }) { index, line ->
        JGroupItem(GroupPosition.of(index, count)) { JRow(line, leadingIcon = PhIcons.Check) }
    }
    surfaces?.let { list ->
        item("surfaces") {
            JGroupItem(GroupPosition.of(lines.size, count)) {
                JRow("可以在这些地方用", leadingIcon = PhIcons.DeviceMobile, subtitle = list.joinToString("、"), subtitleLines = 3)
            }
        }
    }
}

/** The code as a picture in the photo library (Pictures/Persona), for the chat app on this phone to scan from there. */
private suspend fun saveQrImage(context: Context, rows: List<String>, name: String): Boolean = withContext(Dispatchers.IO) {
    runCatching {
        val bitmap = qrBitmap(rows)
        val values = ContentValues().apply {
            put(MediaStore.Images.Media.DISPLAY_NAME, "persona-$name-${System.currentTimeMillis()}.png")
            put(MediaStore.Images.Media.MIME_TYPE, "image/png")
            put(MediaStore.Images.Media.RELATIVE_PATH, "Pictures/Persona")
            put(MediaStore.Images.Media.IS_PENDING, 1)
        }
        val resolver = context.contentResolver
        val uri = resolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values) ?: return@runCatching false
        val written = runCatching {
            resolver.openOutputStream(uri)?.use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) } == true
        }.getOrDefault(false)
        if (!written) { resolver.delete(uri, null, null); return@runCatching false }
        resolver.update(uri, ContentValues().apply { put(MediaStore.Images.Media.IS_PENDING, 0) }, null, null)
        true
    }.getOrDefault(false)
}
