package jarvis.android.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.automirrored.rounded.Chat
import androidx.compose.material.icons.rounded.ArrowCircleUp
import androidx.compose.material.icons.rounded.Edit
import androidx.compose.material.icons.rounded.KeyboardArrowDown
import androidx.compose.material.icons.rounded.OpenInFull
import androidx.compose.material.icons.rounded.Terminal
import androidx.compose.material3.HorizontalDivider
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import jarvis.android.data.*
import jarvis.android.ui.chat.*
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.IconAction
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.PageScaffold
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.surface
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * A card for something the agent waits on (docs/protocol.md「Interaction」, iOS `InteractionPanel`): pick answers
 * (several questions page through 上一题 / 下一题), approve or send back a plan (full screen from the corner), allow a
 * tool, or press a terminal dialog's option. In a chat the header folds it to one line. 交回电脑处理 is in its menu.
 */
@Composable
fun InteractionCard(
    hub: Hub,
    ix: Interaction,
    modifier: Modifier = Modifier,
    /** Pinned in a chat: the header folds the card to one line (iOS `InteractionPanel`). Off in sheets. */
    collapsible: Boolean = true,
    onResolved: () -> Unit = {},
) {
    val sessions by hub.sessions.collectAsState()
    val allowed = sessions[ix.machineId]?.firstOrNull { it.sessionId == ix.sessionId }?.canApprove == true
    val c = chatColors
    var busy by remember(ix.id) { mutableStateOf(false) }
    var menu by remember { mutableStateOf(false) }
    var collapsed by rememberSaveable(ix.id) { mutableStateOf(false) }
    var fullPlan by remember(ix.id) { mutableStateOf(false) }
    var error by remember(ix.id) { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val view = jarvis.android.ui.design.hapticView()
    val submit: (Resolution) -> Unit = { r ->
        if (!busy && allowed && hub.session(ix.sessionKey)?.canApprove == true) {
            busy = true
            error = null
            scope.launch {
                try {
                    hub.resolve(ix, r)
                    jarvis.android.ui.design.Haptics.success(view)
                    onResolved()
                } catch (e: HubException) {
                    if (e.status != 410 && e.status != 404) jarvis.android.ui.design.Haptics.error(view)
                    error = when {
                        e.status == 410 || e.status == 404 -> null // answered elsewhere or expired
                        e.code == "prompt_changed" -> "终端上的对话框已经变了，这次没有按任何键：看看新的再答"
                        else -> e.message
                    }
                } catch (e: Exception) {
                    if (!e.isCancellation()) error = "连不上 hub，刚才的操作没有生效"
                }
                busy = false
            }
        }
    }
    val folded = collapsible && collapsed

    val tone = JTheme.needsYou
    Column(
        modifier.fillMaxWidth().surface(RoundedCornerShape(JRadius.panel), floating = true)
            .padding(start = JSpace.l, end = JSpace.s, top = JSpace.xs, bottom = if (folded) JSpace.xs else JSpace.l),
    ) {
        Row(
            Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget).then(
                if (collapsible) Modifier.clickable(role = Role.Button, onClickLabel = if (folded) "展开" else "收起") { collapsed = !collapsed }
                else Modifier,
            ),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(interactionIcon(ix), null, Modifier.size(JSize.markL), tint = tone.dot)
            Spacer(Modifier.width(JSpace.sm))
            Text(ix.kindLabel(hub.session(ix.sessionKey)?.agentLabel ?: "Claude"), style = JTheme.type.captionStrong, color = tone.text, maxLines = 1)
            if (folded) {
                Spacer(Modifier.width(JSpace.s))
                Text(previewText(ix), style = JTheme.type.secondary, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            } else {
                ix.toolName?.takeIf { ix.kind == "permission" }?.let {
                    Text(" · " + ToolText.baseName(it), style = JTheme.type.captionStrong, color = c.secondaryLabel, maxLines = 1,
                        overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                }
                Spacer(Modifier.weight(1f))
            }
            if (busy) CircularProgressIndicator(Modifier.padding(end = JSpace.xs).size(JSize.markM), strokeWidth = JSize.rule, color = c.secondaryLabel)
            if (ix.kind == "plan" && !folded) IconAction(JIcons.enlarge, "全屏查看计划", { fullPlan = true }, tint = c.secondaryLabel, size = JSize.markL)
            Box {
                IconAction(JIcons.more, "更多", { menu = true }, tint = c.accent, size = JSize.iconXS)
                DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                    DropdownMenuItem(
                        text = { Text("交回电脑处理", maxLines = 1) },
                        enabled = allowed,
                        onClick = { menu = false; submit(Resolutions.dismiss()) },
                        leadingIcon = { Icon(JIcons.machine, null) },
                    )
                }
            }
            if (collapsible) {
                Icon(JIcons.expand, contentDescription = null, tint = c.secondaryLabel,
                    modifier = Modifier.padding(end = JSpace.s).size(JSize.iconXS).rotate(if (folded) 180f else 0f))
            }
        }
        if (!folded) {
            Column(Modifier.padding(end = JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
                when (ix.kind) {
                    "question" -> QuestionBody(ix, busy || !allowed, submit)
                    "plan" -> PlanBody(ix, busy || !allowed, submit)
                    "permission" -> PermissionBody(ix, busy || !allowed, submit)
                    "screen" -> ScreenBody(ix, busy || !allowed, submit)
                    else -> Text("这个请求手机还不支持，在电脑上处理", style = JTheme.type.secondary, color = c.secondaryLabel)
                }
                error?.let {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        Icon(JIcons.error, null, Modifier.size(JSize.markS), tint = c.danger)
                        Text(it, color = c.danger, style = JTheme.type.caption)
                    }
                }
            }
        }
    }
    if (fullPlan) FullPlan(ix.plan.orEmpty()) { fullPlan = false }
}

/** The resolutions the cards send (docs/protocol.md「Resolution」). */
private object Resolutions {
    fun answer(question: String, label: String) = Resolution(answers = mapOf(question to label))
    fun answers(map: Map<String, String>) = Resolution(answers = map)
    /** 批准并自动执行. */
    fun approvePlanAuto() = Resolution(approve = true, mode = "bypassPermissions")
    /** 自动接受编辑 (also the notification's 批准). */
    fun approvePlanAcceptEdits() = Resolution(approve = true, mode = "acceptEdits")
    fun rejectPlan(feedback: String) = Resolution(approve = false, feedback = feedback)
    fun allow(always: Boolean = false) = Resolution(allow = true, always = if (always) true else null)
    fun deny(message: String? = null) = Resolution(allow = false, message = message?.takeIf { it.isNotBlank() })
    fun screenOption(key: String, text: String? = null) = Resolution(option = key, text = text)
    fun screenKeys(keys: List<String>, text: String? = null) = Resolution(keys = keys, text = text)
    fun screenText(text: String) = Resolution(keys = listOf("enter"), text = text)
    fun dismiss() = Resolution(dismiss = true)
}

/** What an interaction asks for (iOS `InteractionKind.title`); [agentLabel] = Claude / Codex / TRAEX. */
fun Interaction.kindLabel(agentLabel: String = "Claude"): String = when (kind) {
    "question" -> if (toolName == "ask_user") "Jarvis 在问你" else "$agentLabel 在问你"
    "plan" -> "计划等你审批"
    "permission" -> "需要你授权"
    "screen" -> when {
        secret == true -> "要密码"
        dialog != null -> "$agentLabel 在问你"
        else -> "终端等你处理"
    }
    else -> "需要你"
}

// Claude's 本会话总是允许; another agent's card (Codex, an agent runtime) only when it says what always allows.
private val Interaction.canAlwaysAllow: Boolean get() = kind == "permission" && sessionId.isNotEmpty() &&
    (agent.isNullOrEmpty() || agent == "claude" || !alwaysLabel.isNullOrEmpty())

/** The one-line gist a folded card shows (iOS `InteractionPanel.previewText`). */
private fun previewText(ix: Interaction): String = when (ix.kind) {
    "question" -> ix.questions?.firstOrNull()?.question.orEmpty()
    "plan" -> planTitle(ix.plan.orEmpty())
    "permission" -> ix.summary.orEmpty()
    "screen" -> ix.dialog?.question.orEmpty()
    else -> ix.summary.orEmpty()
}

/** iOS `PlanText.title`: the first non-empty line without Markdown marks. */
fun planTitle(plan: String): String =
    plan.lineSequence().map { it.trim().trim('#', '*', ' ') }.firstOrNull { it.isNotEmpty() } ?: "执行计划"

/** The plan full screen (iOS: a sheet with the title and 完成). */
@Composable
private fun FullPlan(plan: String, onDone: () -> Unit) {
    Dialog(onDismissRequest = onDone, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        PageScaffold(planTitle(plan), onBack = onDone, actions = {
            Text("完成", style = JTheme.type.bodyStrong, color = JTheme.colors.accent, maxLines = 1,
                modifier = Modifier.clip(CircleShape).clickable(onClick = onDone).padding(horizontal = JSpace.m, vertical = JSpace.s))
        }) {
            Box(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(JSpace.page)) {
                Markdown(plan.ifBlank { "（计划是空的）" })
            }
        }
    }
}

// --- Question --------------------------------------------------------------------------------------------------------

/**
 * AskUserQuestion, iOS `QuestionForm`: the question, its options in one grouped box (numbered like the terminal, a
 * check when picked, 推荐 for the suggested one), 其他回答 as the box's last row. One question, one choice: a tap
 * answers. Several questions: "header · 1/2" on top, 上一题 / 下一题 / 提交 below.
 */
@Composable
private fun QuestionBody(ix: Interaction, busy: Boolean, submit: (Resolution) -> Unit) {
    val c = chatColors
    val questions = ix.questions.orEmpty()
    if (questions.isEmpty()) return
    // Picked labels and free text, per question text.
    val picked = remember(ix.id) { mutableStateMapOf<String, Set<String>>() }
    val typed = remember(ix.id) { mutableStateMapOf<String, String>() }
    var page by rememberSaveable(ix.id) { mutableIntStateOf(0) }
    val scope = rememberCoroutineScope()
    val needsFooter = questions.size > 1 || questions[0].multiSelect
    page = page.coerceIn(0, questions.size - 1)
    val q = questions[page]

    fun answer(q: Question): String? {
        val t = typed[q.question]?.trim().orEmpty()
        if (t.isNotEmpty()) return t
        return answerFor(q, picked[q.question].orEmpty()).takeIf { it.isNotEmpty() }
    }
    val allAnswered = questions.all { answer(it) != null }
    fun send() {
        if (!allAnswered) {
            questions.indexOfFirst { answer(it) == null }.takeIf { it >= 0 }?.let { page = it }
            return
        }
        submit(Resolutions.answers(questions.associate { it.question to answer(it)!! }))
    }

    val meta = listOfNotNull(
        q.header?.takeIf { it.isNotBlank() },
        if (questions.size > 1) "${page + 1}/${questions.size}" else null,
        if (q.multiSelect) "可多选" else null,
    ).joinToString(" · ")
    if (meta.isNotEmpty()) Text(meta, style = JTheme.type.captionStrong, color = c.secondaryLabel, maxLines = 1)
    Text(q.question, style = JTheme.type.heading, color = c.label)
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.card)).background(c.fillWell)) {
        q.options.forEachIndexed { i, opt ->
            if (i > 0) RowDivider(OptionTextColumn)
            val selected = opt.label in picked[q.question].orEmpty()
            OptionRow(opt.displayLabel, opt.recommended, opt.description, number = "${i + 1}", selected = selected, enabled = !busy) {
                val cur = picked[q.question].orEmpty()
                if (q.multiSelect) {
                    picked[q.question] = if (opt.label in cur) cur - opt.label else cur + opt.label
                } else {
                    picked[q.question] = setOf(opt.label)
                    typed.remove(q.question)
                    if (questions.size == 1) {
                        submit(Resolutions.answer(q.question, opt.label)) // one question, one choice: the tap answers
                    } else if (page < questions.size - 1) {
                        scope.launch { delay(250); page += 1 }
                    }
                }
            }
        }
        RowDivider(OptionTextColumn)
        OtherRow(
            value = typed[q.question].orEmpty(),
            enabled = !busy,
            onChange = {
                typed[q.question] = it
                if (it.isNotBlank() && !q.multiSelect) picked.remove(q.question)
            },
            onSend = if (!needsFooter) { { typed[q.question]?.trim()?.takeIf { it.isNotEmpty() }?.let { submit(Resolutions.answer(q.question, it)) } } } else null,
        )
    }
    if (needsFooter) {
        val isLast = page >= questions.size - 1 || allAnswered
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            if (page > 0) PillButton("上一题", { page -= 1 }, kind = ButtonKind.Neutral, enabled = !busy)
            Spacer(Modifier.weight(1f))
            PillButton(if (isLast) "提交" else "下一题", { if (isLast) send() else page += 1 }, kind = ButtonKind.Prominent, enabled = !busy && answer(q) != null)
        }
    }
}

private fun answerFor(q: Question, picked: Set<String>): String =
    q.options.map { it.label }.filter { it in picked }.joinToString(", ")

/** Where an option's words start (after its number): the separators between options start there. */
private val OptionTextColumn = JSpace.l + JSize.icon + JSpace.m

/**
 * One option in the grouped box: a numbered circle (accent with a white check when picked), the label in the label
 * color and its description in the secondary color on a faint accent wash, readable in every style.
 */
@Composable
private fun OptionRow(
    label: String,
    recommended: Boolean,
    description: String?,
    number: String,
    selected: Boolean,
    enabled: Boolean,
    busy: Boolean = false,
    onClick: () -> Unit,
) {
    val c = chatColors
    Row(
        Modifier.fillMaxWidth()
            .background(if (selected) c.accent.copy(alpha = 0.10f) else Color.Transparent)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = JSpace.l, vertical = JSpace.m)
            .semantics { if (selected) stateDescription = "已选" },
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Box(
            Modifier.size(JSize.icon).clip(CircleShape).background(if (selected || busy) c.accent else c.fillChip),
            contentAlignment = Alignment.Center,
        ) {
            when {
                busy -> CircularProgressIndicator(Modifier.size(JSize.markXS), strokeWidth = JSize.lineMedium, color = Color.White)
                selected -> Icon(JIcons.done, contentDescription = null, tint = Color.White, modifier = Modifier.size(JSize.markS))
                else -> Text(number, style = JTheme.type.labelStrong, color = c.secondaryLabel, maxLines = 1)
            }
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                Text(label, style = JTheme.type.body.copy(fontWeight = androidx.compose.ui.text.font.FontWeight.Medium), color = c.label, modifier = Modifier.weight(1f, fill = false))
                if (recommended) Chip("推荐", look = ChipLook.Tint(c.ai))
            }
            description?.takeIf { it.isNotBlank() }?.let { Text(it, style = JTheme.type.caption, color = c.secondaryLabel) }
        }
    }
}

/** 其他回答…: a quiet row with a pencil; typing turns it into the answer (with its own send arrow when [onSend]). */
@Composable
private fun OtherRow(value: String, enabled: Boolean, onChange: (String) -> Unit, onSend: (() -> Unit)?, placeholder: String = "其他回答…") {
    val c = chatColors
    Row(
        Modifier.fillMaxWidth().padding(start = JSpace.l, end = JSpace.sm, top = JSpace.xs, bottom = JSpace.xs),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Box(Modifier.size(JSize.icon).clip(CircleShape).background(c.fillChip), contentAlignment = Alignment.Center) {
            Icon(JIcons.edit, null, tint = c.secondaryLabel, modifier = Modifier.size(JSize.markXS))
        }
        BasicTextField(
            value = value,
            onValueChange = onChange,
            enabled = enabled,
            maxLines = 4,
            textStyle = JTheme.type.body.copy(color = c.label),
            cursorBrush = SolidColor(c.accent),
            modifier = Modifier.weight(1f).padding(vertical = JSpace.s),
            decorationBox = { inner ->
                Box {
                    if (value.isEmpty()) Text(placeholder, style = JTheme.type.body, color = c.secondaryLabel, maxLines = 1)
                    inner()
                }
            },
        )
        if (onSend != null && value.isNotBlank()) {
            RoundButton(JIcons.send, "发送回答", onSend, fill = c.accent, words = Color.White, enabled = enabled)
        }
    }
}

// --- Plan ------------------------------------------------------------------------------------------------------------

@Composable
private fun PlanBody(ix: Interaction, busy: Boolean, submit: (Resolution) -> Unit) {
    var rejecting by remember(ix.id) { mutableStateOf(false) }
    var feedback by remember(ix.id) { mutableStateOf("") }
    Box(Modifier.fillMaxWidth().heightIn(max = JSize.box280).verticalScroll(rememberScrollState())) {
        Markdown(ix.plan.orEmpty().ifBlank { "（计划是空的）" })
    }
    if (rejecting) {
        JTextField(feedback, { feedback = it }, "修改意见（可以不写）", enabled = !busy, maxLines = 5)
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
            PillButton("取消", { rejecting = false }, Modifier.weight(1f), kind = ButtonKind.Neutral, enabled = !busy)
            PillButton("打回修改", { submit(Resolutions.rejectPlan(feedback.trim())) }, Modifier.weight(1f), kind = ButtonKind.Prominent, enabled = !busy)
        }
    } else {
        PillButton("批准并自动执行", { submit(Resolutions.approvePlanAuto()) }, Modifier.fillMaxWidth(), kind = ButtonKind.Prominent, icon = JIcons.run, enabled = !busy, large = true)
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
            PillButton("自动接受编辑", { submit(Resolutions.approvePlanAcceptEdits()) }, Modifier.weight(1f), kind = ButtonKind.Tinted, enabled = !busy)
            PillButton("打回", { rejecting = true }, Modifier.weight(1f), kind = ButtonKind.Tinted, enabled = !busy)
        }
    }
}

// --- Permission ------------------------------------------------------------------------------------------------------

@Composable
private fun PermissionBody(ix: Interaction, busy: Boolean, submit: (Resolution) -> Unit) {
    var denying by remember(ix.id) { mutableStateOf(false) }
    var reason by remember(ix.id) { mutableStateOf("") }
    val detail = ix.summary?.takeIf { it.isNotBlank() } ?: ix.toolInput?.let { pretty(it) }
    if (!detail.isNullOrBlank()) {
        Box(
            Modifier.fillMaxWidth().heightIn(max = JSize.box200).background(chatColors.codeBackground, RoundedCornerShape(JRadius.control))
                .verticalScroll(rememberScrollState()).padding(JSpace.m),
        ) { Text(detail, style = JTheme.type.code, color = chatColors.label) }
    }
    if (denying) {
        JTextField(reason, { reason = it }, "告诉它为什么（可以不写）", enabled = !busy, maxLines = 4)
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
            PillButton("取消", { denying = false }, Modifier.weight(1f), kind = ButtonKind.Neutral, enabled = !busy)
            PillButton("拒绝", { submit(Resolutions.deny(reason)) }, Modifier.weight(1f), kind = ButtonKind.DestructiveProminent, enabled = !busy)
        }
    } else {
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
            PillButton("拒绝", { denying = true }, Modifier.weight(1f), kind = ButtonKind.Destructive, enabled = !busy, large = true)
            PillButton("允许", { submit(Resolutions.allow()) }, Modifier.weight(1f), kind = ButtonKind.Prominent, enabled = !busy, large = true)
        }
        if (ix.canAlwaysAllow) PillButton("本会话总是允许", { submit(Resolutions.allow(always = true)) }, Modifier.fillMaxWidth(), kind = ButtonKind.Tinted, enabled = !busy)
    }
}

// --- Screen ----------------------------------------------------------------------------------------------------------

/**
 * The terminal waiting on a choice (iOS `ScreenForm`): the parsed dialog laid out like a question (numbered options,
 * 其他回答… for "Type something"), 改在对话里说 for the chat option, and the raw terminal one tap away (查看终端).
 * A screen without a parsed dialog (or a password prompt) shows the terminal directly.
 */
@Composable
private fun ScreenBody(ix: Interaction, busy: Boolean, submit: (Resolution) -> Unit) {
    val c = chatColors
    var showTerminal by rememberSaveable(ix.id) { mutableStateOf(false) }
    var typing by remember(ix.id) { mutableStateOf("") }
    var sending by remember(ix.id) { mutableStateOf<String?>(null) }
    val dialog = ix.dialog
    if (ix.secret == true || dialog == null || dialog.options.isEmpty()) {
        ScreenTerminal(ix, busy, submit)
        return
    }
    dialog.header?.takeIf { it.isNotBlank() }?.let {
        Text(it, style = JTheme.type.captionStrong, color = c.secondaryLabel, maxLines = 1)
    }
    dialog.question.takeIf { it.isNotBlank() }?.let {
        Text(it, style = JTheme.type.heading, color = c.label)
    }
    dialog.detail?.takeIf { it.isNotBlank() }?.let {
        Box(
            Modifier.fillMaxWidth().heightIn(max = JSize.box200)
                .background(c.codeBackground, RoundedCornerShape(JRadius.control))
                .verticalScroll(rememberScrollState()).padding(JSpace.m),
        ) { Text(it, style = JTheme.type.code, color = c.secondaryLabel) }
    }
    val choices = dialog.options.filter { it.kind != "chat" }
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.card)).background(c.fillWell)) {
        choices.forEachIndexed { i, opt ->
            if (i > 0) RowDivider(OptionTextColumn)
            if (opt.needsText) {
                OtherRow(
                    value = typing,
                    enabled = !busy,
                    onChange = { typing = it },
                    onSend = {
                        val t = typing.trim()
                        if (t.isNotEmpty()) { sending = opt.key; submit(Resolutions.screenOption(opt.key, t)) }
                    },
                )
            } else {
                val (label, recommended) = QuestionOption(label = opt.label).let { it.displayLabel to it.recommended }
                OptionRow(
                    label, recommended, opt.detail, number = opt.key, selected = false, enabled = !busy && sending == null,
                    busy = sending == opt.key,
                ) { sending = opt.key; submit(Resolutions.screenOption(opt.key)) }
            }
        }
    }
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        dialog.options.firstOrNull { it.kind == "chat" }?.let { chat ->
            PillButton("改在对话里说", { sending = chat.key; submit(Resolutions.screenOption(chat.key)) }, kind = ButtonKind.Tinted, icon = JIcons.chatInstead, enabled = !busy)
        }
        Spacer(Modifier.weight(1f))
        PillButton(if (showTerminal) "收起终端" else "查看终端", { showTerminal = !showTerminal }, kind = ButtonKind.Neutral, icon = JIcons.terminal)
    }
    if (showTerminal) ScreenTerminal(ix, busy, submit)
}

/** The raw screen, a few keys and a line of text (or a password): for anything the dialog reading did not catch. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ScreenTerminal(ix: Interaction, busy: Boolean, submit: (Resolution) -> Unit) {
    val c = chatColors
    var text by remember(ix.id) { mutableStateOf("") }
    if (ix.secret == true) {
        ix.screen?.lineSequence()?.lastOrNull { it.isNotBlank() }?.let { Text(it.trim(), style = JTheme.type.code, color = c.label) }
        JTextField(text, { text = it }, "密码", singleLine = true, enabled = !busy, password = true)
        Text("只用这一次，不会保存", style = JTheme.type.label, color = c.secondaryLabel)
        PillButton("发送", { submit(Resolutions.screenText(text)) }, Modifier.fillMaxWidth(), kind = ButtonKind.Prominent, enabled = !busy && text.isNotEmpty(), large = true)
        return
    }
    ix.screen?.takeIf { it.isNotBlank() }?.let {
        Box(
            Modifier.fillMaxWidth().heightIn(max = JSize.box220)
                .background(TerminalInk, RoundedCornerShape(JRadius.control))
                .verticalScroll(rememberScrollState()).padding(JSpace.m),
        ) { Text(it.trimEnd(), style = JTheme.type.code, softWrap = true, color = TerminalPaper) }
    }
    FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
        listOf("1" to listOf("1"), "2" to listOf("2"), "3" to listOf("3"), "回车" to listOf("enter"), "Esc" to listOf("esc"))
            .forEach { (label, keys) -> PillButton(label, { submit(Resolutions.screenKeys(keys)) }, kind = ButtonKind.Neutral, enabled = !busy) }
    }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
        JTextField(text, { text = it }, "输入文字发送到终端", Modifier.weight(1f), enabled = !busy, maxLines = 3)
        if (text.isNotEmpty()) RoundButton(JIcons.send, "发送", { submit(Resolutions.screenText(text)) }, fill = c.accent, words = Color.White, enabled = !busy)
    }
}

/** The terminal's own colors (iOS `Theme.terminalBackground` / `terminalText`), the same in every style. */
private val TerminalInk = Color(0xFF16171B)
private val TerminalPaper = Color(0xFFE6E3DA)

