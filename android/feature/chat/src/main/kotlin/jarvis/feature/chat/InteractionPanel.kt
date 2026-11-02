package jarvis.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import jarvis.core.contracts.Interaction
import jarvis.core.contracts.Machine
import jarvis.core.contracts.Resolution
import jarvis.core.designsystem.*
import jarvis.core.domain.answer.*
import java.time.Instant
import kotlinx.coroutines.delay

@Composable
internal fun InteractionPanel(
    requests: List<Interaction>, machine: Machine?, maxHeight: Dp, busyId: String?, uncertainIds: Set<String>,
    onResolve: (Interaction, Resolution) -> Unit, onTerminal: (() -> Unit)?, onOverlay: (Boolean) -> Unit,
    linkVisible: Boolean = false,
    collapsed: Boolean = false, onCollapsedChange: (Boolean) -> Unit = {},
) {
    var selected by rememberSaveable { mutableStateOf(0) }
    var full by rememberSaveable { mutableStateOf(false) }
    var all by remember { mutableStateOf(false) }
    LaunchedEffect(LocalChatLinkOpening.current) { full = false; all = false }
    LaunchedEffect(requests.isEmpty()) { if (requests.isEmpty()) { full = false; all = false } }
    LaunchedEffect(full, all) { onOverlay(full || all) }
    DisposableEffect(Unit) { onDispose { onOverlay(false) } }
    if (requests.isEmpty()) return
    val index = selected.coerceIn(0, requests.lastIndex)
    val request = requests[index]
    val body: @Composable (Boolean) -> Unit = { complete ->
        key(request.id, request.dialog?.rev, complete) {
            RequestEditor(request, machine, busyId != null, request.id in uncertainIds, complete,
                onTerminal = onTerminal, onExpand = { full = true }, linkVisible = linkVisible) { action, value, planRead ->
                AnswerBuilder.build(request, action, value, AnswerContext(fullPlanRead = planRead)).resolution?.let {
                    onResolve(request, it)
                }
            }
        }
    }
    val now by produceState(Instant.now(), request.expiresAt) {
        while (true) { value = Instant.now(); delay(1_000) }
    }
    DecisionDock(
        kind = when (request.kind) {
            "permission" -> if (request.toolName == "Bash") DecisionKind.Command else DecisionKind.Tool
            "plan" -> DecisionKind.Plan
            "question" -> DecisionKind.Question
            "elicitation" -> DecisionKind.Form
            "screen" -> DecisionKind.Screen
            else -> DecisionKind.Other
        },
        headline = request.headline, expires = RequestExpiry.label(request.expiresAt, now),
        modifier = Modifier.heightIn(max = maxHeight), position = index + 1 to requests.size,
        onPrevious = { selected = index - 1 }, onNext = { selected = index + 1 }, onList = { all = true },
        collapsed = collapsed, onCollapsedChange = onCollapsedChange, onExpand = { full = true },
    ) {
        if (!full && !all) body(false)
    }
    if (full) JSheet(onDismiss = { full = false }, title = request.headline) {
        Column(Modifier.fillMaxHeight().imePadding()) {
            JTextButton("收起完整页", { full = false })
            body(true)
        }
    }
    if (all) JSheet(onDismiss = { all = false }, title = "等你处理的请求") {
        LazyColumn(Modifier.fillMaxWidth().heightIn(max = maxHeight), contentPadding = PaddingValues(bottom = 16.dp)) {
            itemsIndexed(requests, key = { _, interaction -> interaction.id }) { i, interaction ->
                JRow(interaction.headline, value = "${i + 1}", onClick = { selected = i; all = false; full = true })
            }
        }
    }
}

@Composable
internal fun InteractionForm(
    interaction: Interaction, machine: Machine?, draft: AnswerDraft, update: (AnswerDraft) -> Unit,
    busy: Boolean, uncertain: Boolean, full: Boolean,
    onAnswer: (AnswerAction, AnswerDraft, Boolean) -> Unit, onTerminal: (() -> Unit)?, onExpand: () -> Unit,
    items: List<jarvis.core.contracts.Item> = emptyList(),
    readOnly: Boolean = false,
    linkVisible: Boolean = false,
) {
    val scroll = rememberScrollState()
    var planRead by rememberSaveable(interaction.id, interaction.plan) { mutableStateOf(false) }
    val atPlanEnd by remember { derivedStateOf { scroll.value >= scroll.maxValue } }
    LaunchedEffect(full, atPlanEnd) {
        if (full) {
            withFrameNanos { }; withFrameNanos { }
            if (scroll.value >= scroll.maxValue) planRead = true
        }
    }
    val secret = interaction.secret == true
    // Secrets live only in this visible form, never in saved state or request drafts.
    var password by remember(interaction.id) { mutableStateOf("") }
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    DisposableEffect(lifecycle, interaction.id) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_STOP) password = ""
        }
        lifecycle.addObserver(observer)
        onDispose { password = ""; lifecycle.removeObserver(observer) }
    }
    var terminalHelp by remember { mutableStateOf(false) }
    val now by produceState(Instant.now(), interaction.expiresAt) {
        while (true) { value = Instant.now(); delay(1_000) }
    }
    val expired = RequestExpiry.expired(interaction.expiresAt, now)
    LaunchedEffect(expired, uncertain) { if (expired || uncertain) password = "" }
    val context = AnswerContext(state = if (expired || uncertain || readOnly || linkVisible) "unknown" else "pending", sending = busy,
        fullPlanRead = planRead)
    val editable = !busy && !expired && !uncertain && !readOnly && !linkVisible
    fun act(action: AnswerAction, value: AnswerDraft = draft) {
        if (RequestExpiry.expired(interaction.expiresAt, Instant.now())) return
        val result = AnswerBuilder.build(interaction, action, value, context)
        if (!result.allowed) return
        if (result.resolution == null) update(result.draft)
        else { password = ""; onAnswer(action, result.draft, planRead) }
    }
    fun enabled(action: AnswerAction) = AnswerBuilder.build(interaction, action, draft, context).allowed
    val questions = interaction.questions.orEmpty()
    val question = questions.getOrNull(draft.page)
    val fallback = remember(interaction.screen, secret) {
        if (secret) null else ScreenAnswers.parse(interaction.screen.orEmpty())
    }
    val options = interaction.dialog?.options?.map {
        ParsedScreenOption(it.key, it.label, it.detail, when (it.kind) {
            "type_something" -> ScreenOptionKind.TypeSomething
            "chat" -> ScreenOptionKind.Chat
            null, "", "plain" -> ScreenOptionKind.Plain
            else -> ScreenOptionKind.Unknown
        })
    } ?: fallback?.options.orEmpty()
    val actions: @Composable () -> Unit = {
        when (interaction.kind) {
            "permission" -> DecisionButtons(
                primary = JAction("允许", enabled = enabled(AnswerAction.Allow)) { act(AnswerAction.Allow) },
                secondary = JAction("拒绝", enabled = enabled(AnswerAction.Deny)) { act(AnswerAction.Deny) },
                more = listOf(JAction("本会话总是允许", enabled = enabled(AnswerAction.Always)) { act(AnswerAction.Always) }),
            )
            "plan" -> {
                DecisionButtons(
                    primary = JAction("批准编辑", enabled = enabled(AnswerAction.ApproveEdits)) { act(AnswerAction.ApproveEdits) },
                    secondary = JAction("打回", enabled = enabled(AnswerAction.Reject)) { act(AnswerAction.Reject) },
                    more = listOf(JAction("批准并自动执行", enabled = enabled(AnswerAction.ApproveAll)) { act(AnswerAction.ApproveAll) }),
                )
                Text(if (planRead) "等于全部放行：之后的命令和编辑都不再问你" else "完整页读完计划后才能批准并自动执行",
                    style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
            }
            "question" -> if (question != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    JTextButton("上一题", { update(draft.copy(page = draft.page - 1)) }, enabled = draft.page > 0 && editable)
                    Text("${draft.page + 1} / ${questions.size}", Modifier.padding(top = Tokens.Space.m))
                    val action = if (draft.page == questions.lastIndex) AnswerAction.Submit else AnswerAction.Advance
                    JTextButton(if (draft.page == questions.lastIndex) "提交" else "下一题", { act(action) }, enabled = enabled(action))
                }
            }
            "elicitation" -> ElicitationActions(interaction, draft, editable, ::act)
            "screen" -> if (secret) {
                JButton("提交密码", onClick = { act(AnswerAction.TerminalText, AnswerDraft(text = password)) },
                    enabled = editable && password.isNotEmpty())
            } else options.forEach { option ->
                val value = draft.copy(option = option.key)
                val display = AnswerBuilder.display(option.label)
                DecisionOption(display.text, { act(AnswerAction.ScreenOption, value) }, detail = option.detail,
                    recommended = display.recommended, selected = draft.option == option.key,
                    enabled = option.kind != ScreenOptionKind.Unknown &&
                        AnswerBuilder.build(interaction, AnswerAction.ScreenOption, value, context).allowed)
            }
        }
    }
    val content: @Composable () -> Unit = {
        // In the conversation's dock the session is the page itself and the countdown is in the dock's header.
        if (full) Text(interactionSource(interaction, machine), style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
        interaction.requestSourceLabel()?.let { Text(it, style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2) }
        if (uncertain) PendingBadge(PendingStatus.Unconfirmed)
        if (full || readOnly || uncertain || busy) Text(when {
            readOnly -> "已处理或已撤回，仅查看历史"
            uncertain -> "请先核对请求或到终端查看"
            busy -> "正在回传你的选择…"
            else -> RequestExpiry.label(interaction.expiresAt, now) ?: "等待你的选择"
        }, style = JarvisTheme.type.callout, color = JarvisTheme.palette.ink2)
        Spacer(Modifier.height(Tokens.Space.s))
        when (interaction.kind) {
            "permission" -> {
                PermissionRequestSummary(interaction)
                interaction.toolInput?.let { input ->
                    var showInput by remember(interaction) { mutableStateOf(false) }
                    JTextButton(if (showInput) "收起完整参数" else "查看完整参数", { showInput = !showInput })
                    if (showInput) JCodeBlock(input.toString())
                }
                JTextField(draft.message, { if (it.length <= 4000) update(draft.copy(message = it)) },
                    label = "拒绝原因（可选）", modifier = Modifier.fillMaxWidth(), enabled = editable)
                if (draft.message.isNotEmpty()) Text("要允许请先清空原因", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
            }
            "plan" -> {
                if (full) ChatMarkdown(interaction.plan.orEmpty())
                else {
                    Column(Modifier.heightIn(max = 140.dp).verticalScroll(rememberScrollState(), enabled = false)) {
                        ChatMarkdown(interaction.plan.orEmpty())
                    }
                    JTextButton("查看完整计划", onExpand)
                }
                JTextField(draft.feedback, { update(draft.copy(feedback = it)) }, label = "意见（写了就只能打回）",
                    modifier = Modifier.fillMaxWidth(), enabled = editable)
                if (draft.feedback.isNotEmpty()) Text("要批准请先清空意见", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
            }
            "question" -> if (readOnly) {
                questions.forEach { past ->
                    ChatLinkText(past.question, style = JarvisTheme.type.body)
                    past.options.forEach { option ->
                        Text(AnswerBuilder.display(option.label).text, style = JarvisTheme.type.callout)
                        option.description?.let { Text(it, style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2) }
                    }
                }
            } else if (question != null) {
                ChatLinkText(question.question, style = JarvisTheme.type.body)
                question.options.forEachIndexed { index, option ->
                    val display = AnswerBuilder.display(option.label)
                    DecisionOption(display.text, { act(AnswerAction.PickOption, draft.copy(optionIndex = index)) },
                        detail = option.description, recommended = display.recommended,
                        selected = option.label in draft.picks[question.question].orEmpty(), enabled = editable)
                }
                var otherOpen by rememberSaveable(interaction.id, question.question) { mutableStateOf(false) }
                if (otherOpen || question.question in draft.other) {
                    JTextField(draft.other[question.question].orEmpty(), {
                        update(draft.copy(other = draft.other + (question.question to it), picks = draft.picks - question.question))
                    }, label = "其他回答", modifier = Modifier.fillMaxWidth(), enabled = editable)
                } else JTextButton("其他回答…", { otherOpen = true }, enabled = editable)
            }
            "elicitation" -> ElicitationFields(interaction, draft, update, editable)
            "screen" -> if (secret) {
                Text("终端需要输入密码")
                JTextField(password, { password = it }, label = "密码",
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
                    singleLine = true, modifier = Modifier.fillMaxWidth(), enabled = editable)
                Text("密码不会保存；收起面板或离开页面后清空", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
            } else {
                ChatLinkText(interaction.dialog?.question ?: fallback?.question ?: interaction.headline)
                val command = interaction.requestCommand(items)
                if (command != null) {
                    Text("完整命令", style = JarvisTheme.type.headline)
                    JCodeBlock(command.command)
                    Text("请求 ${command.requestId}，工具调用 ${command.toolUseId}", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
                } else (interaction.dialog?.detail ?: fallback?.detail)?.let { ChatLinkText(it) }
                options.mapNotNull { it.detail }.forEach { ChatLinkText(it, style = JarvisTheme.type.caption.copy(color = JarvisTheme.palette.ink2)) }
                if (options.any { it.kind == ScreenOptionKind.TypeSomething }) JTextField(draft.text,
                    { update(draft.copy(text = it)) }, label = "输入回答", modifier = Modifier.fillMaxWidth(), enabled = editable)
                if (options.isEmpty()) Text("暂时读不出这个对话框，请在电脑上核对并处理。")
            }
            else -> Text("暂不支持在手机上处理这个请求，请在电脑上核对并处理。")
        }
        Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            JTextButton(if (onTerminal != null) RequestExpiry.terminalLabel else "终端处理说明",
                { password = ""; if (onTerminal != null) onTerminal() else terminalHelp = true })
            if (ApprovalPolicy.actions(AnswerEntry.Conversation, interaction.kind, false).any { it.id == ApprovalAction.Dismiss }) {
                JTextButton("交给电脑处理", { act(AnswerAction.Dismiss) }, enabled = enabled(AnswerAction.Dismiss))
            }
        }
    }
    // Large accessibility text scrolls together with vertically arranged actions.
    val largeText = LocalDensity.current.fontScale >= 1.5f
    Column(if (full) Modifier.fillMaxHeight() else Modifier) {
        Column(Modifier.weight(1f, fill = full).verticalScroll(scroll)) {
            content()
            if (largeText && !readOnly) actions()
        }
        if (!largeText && !readOnly) {
            JDivider()
            // Bound long option lists so the request body remains reachable on compact screens.
            BoxWithConstraints {
                Column(Modifier.heightIn(max = maxHeight * 0.6f).verticalScroll(rememberScrollState())) { actions() }
            }
        }
    }
    if (terminalHelp) TerminalHelpDialog { terminalHelp = false }
}

@Composable
internal fun TerminalHelpDialog(onDismiss: () -> Unit) {
    JDialog(onDismiss = onDismiss, title = "在电脑上处理",
        text = "请打开这个会话所在电脑的终端，核对当前问题后操作。",
        confirm = "知道了" to onDismiss, dismiss = null)
}
