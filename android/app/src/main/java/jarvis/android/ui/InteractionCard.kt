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
    val submit: (Resolution) -> Unit = { r ->
        if (!busy && allowed && hub.session(ix.sessionKey)?.canApprove == true) {
            busy = true
            error = null
            scope.launch {
                try {
                    hub.resolve(ix, r)
                    onResolved()
                } catch (e: HubException) {
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

    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        color = c.card,
        border = BorderStroke(1.dp, AgentState.NeedsYou.color().copy(alpha = 0.28f)),
        shadowElevation = 6.dp,
    ) {
        Column(Modifier.padding(start = 16.dp, end = 8.dp, top = 4.dp, bottom = if (folded) 4.dp else 14.dp)) {
            Row(
                Modifier.fillMaxWidth().then(
                    if (collapsible) Modifier.clickable(role = Role.Button, onClickLabel = if (folded) "展开" else "收起") { collapsed = !collapsed }
                    else Modifier,
                ),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(Modifier.size(8.dp).background(AgentState.NeedsYou.color(), CircleShape))
                Spacer(Modifier.width(8.dp))
                Text(
                    ix.kindLabel(hub.session(ix.sessionKey)?.agentLabel ?: "Claude"),
                    style = MaterialTheme.typography.labelLarge,
                    color = AgentState.NeedsYou.color(),
                    maxLines = 1,
                )
                if (folded) {
                    Spacer(Modifier.width(8.dp))
                    Text(
                        previewText(ix),
                        fontSize = 15.sp,
                        color = c.label,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                } else {
                    ix.toolName?.takeIf { ix.kind == "permission" }?.let {
                        Text(
                            " · " + ToolText.baseName(it),
                            style = MaterialTheme.typography.labelLarge,
                            color = c.secondaryLabel,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f, fill = false),
                        )
                    }
                    Spacer(Modifier.weight(1f))
                }
                if (busy) CircularProgressIndicator(Modifier.size(16.dp).padding(end = 4.dp), strokeWidth = 2.dp)
                if (ix.kind == "plan" && !folded) {
                    IconButton(onClick = { fullPlan = true }) {
                        Icon(Icons.Rounded.OpenInFull, contentDescription = "全屏查看计划", tint = c.secondaryLabel, modifier = Modifier.size(18.dp))
                    }
                }
                Box {
                    IconButton(onClick = { menu = true }) { Icon(Icons.Filled.MoreVert, contentDescription = "更多", tint = c.secondaryLabel) }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(
                            text = { Text("交回电脑处理", maxLines = 1) },
                            enabled = allowed,
                            onClick = { menu = false; submit(Resolutions.dismiss()) },
                        )
                    }
                }
                if (collapsible) {
                    Icon(
                        Icons.Rounded.KeyboardArrowDown, contentDescription = null, tint = c.secondaryLabel,
                        modifier = Modifier.padding(end = 4.dp).size(22.dp).rotate(if (folded) 180f else 0f),
                    )
                }
            }
            if (!folded) {
                Column(Modifier.padding(end = 8.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    when (ix.kind) {
                        "question" -> QuestionBody(ix, busy || !allowed, submit)
                        "plan" -> PlanBody(ix, busy || !allowed, submit)
                        "permission" -> PermissionBody(ix, busy || !allowed, submit)
                        "screen" -> ScreenBody(ix, busy || !allowed, submit)
                        else -> Text(
                            "这个请求手机还不支持，在电脑上处理",
                            style = MaterialTheme.typography.bodyMedium,
                            color = c.secondaryLabel,
                        )
                    }
                    error?.let { Text(it, color = c.danger, style = MaterialTheme.typography.bodySmall) }
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
    FullScreenPage(
        title = planTitle(plan),
        onBack = onDone,
        actions = { TextButton(onClick = onDone) { Text("完成", fontWeight = FontWeight.SemiBold, maxLines = 1) } },
    ) { pad ->
        Box(Modifier.fillMaxSize().padding(pad).verticalScroll(rememberScrollState()).padding(16.dp)) {
            Markdown(plan.ifBlank { "（计划是空的）" })
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
    if (meta.isNotEmpty()) Text(meta, fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = c.secondaryLabel, maxLines = 1)
    Text(q.question, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, color = c.label)
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(c.fillWell)) {
        q.options.forEachIndexed { i, opt ->
            if (i > 0) HorizontalDivider(Modifier.padding(start = 50.dp), color = c.separator)
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
        HorizontalDivider(Modifier.padding(start = 50.dp), color = c.separator)
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
            if (page > 0) ThemedButton(onClick = { page -= 1 }, enabled = !busy) { Text("上一题", maxLines = 1) }
            Spacer(Modifier.weight(1f))
            ThemedButton(
                onClick = { if (isLast) send() else page += 1 },
                prominent = true,
                enabled = !busy && answer(q) != null,
            ) { Text(if (isLast) "提交" else "下一题", maxLines = 1) }
        }
    }
}

private fun answerFor(q: Question, picked: Set<String>): String =
    q.options.map { it.label }.filter { it in picked }.joinToString(", ")

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
            .padding(horizontal = 14.dp, vertical = 11.dp)
            .semantics { if (selected) stateDescription = "已选" },
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier.size(24.dp).clip(CircleShape).background(if (selected || busy) c.accent else c.fillChip),
            contentAlignment = Alignment.Center,
        ) {
            when {
                busy -> CircularProgressIndicator(Modifier.size(12.dp), strokeWidth = 1.5.dp, color = Color.White)
                selected -> Icon(Icons.Filled.Check, contentDescription = null, tint = Color.White, modifier = Modifier.size(14.dp))
                else -> Text(number, fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = c.secondaryLabel, maxLines = 1)
            }
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(label, fontSize = 17.sp, fontWeight = FontWeight.Medium, color = c.label, modifier = Modifier.weight(1f, fill = false))
                if (recommended) {
                    Spacer(Modifier.width(6.dp))
                    Text(
                        "推荐", fontSize = 11.sp, fontWeight = FontWeight.SemiBold, color = c.ai, maxLines = 1,
                        modifier = Modifier.background(c.ai.copy(alpha = 0.14f), RoundedCornerShape(6.dp)).padding(horizontal = 6.dp, vertical = 1.dp),
                    )
                }
            }
            description?.takeIf { it.isNotBlank() }?.let { Text(it, fontSize = 13.sp, color = c.secondaryLabel) }
        }
    }
}

/** 其他回答…: a quiet row with a pencil; typing turns it into the answer (with its own send arrow when [onSend]). */
@Composable
private fun OtherRow(value: String, enabled: Boolean, onChange: (String) -> Unit, onSend: (() -> Unit)?, placeholder: String = "其他回答…") {
    val c = chatColors
    Row(
        Modifier.fillMaxWidth().padding(start = 14.dp, end = 6.dp, top = 4.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(Modifier.size(24.dp).clip(CircleShape).background(c.fillChip), contentAlignment = Alignment.Center) {
            Icon(Icons.Rounded.Edit, null, tint = c.secondaryLabel, modifier = Modifier.size(13.dp))
        }
        BasicTextField(
            value = value,
            onValueChange = onChange,
            enabled = enabled,
            maxLines = 4,
            textStyle = TextStyle(fontSize = 17.sp, color = c.label),
            cursorBrush = SolidColor(c.accent),
            modifier = Modifier.weight(1f).padding(vertical = 8.dp),
            decorationBox = { inner ->
                Box {
                    if (value.isEmpty()) Text(placeholder, fontSize = 17.sp, color = c.secondaryLabel, maxLines = 1)
                    inner()
                }
            },
        )
        if (onSend != null && value.isNotBlank()) {
            IconButton(onClick = onSend, enabled = enabled) {
                Icon(Icons.Rounded.ArrowCircleUp, contentDescription = "发送回答", tint = c.accent, modifier = Modifier.size(28.dp))
            }
        }
    }
}

// --- Plan ------------------------------------------------------------------------------------------------------------

@Composable
private fun PlanBody(ix: Interaction, busy: Boolean, submit: (Resolution) -> Unit) {
    var rejecting by remember(ix.id) { mutableStateOf(false) }
    var feedback by remember(ix.id) { mutableStateOf("") }
    Box(
        Modifier
            .fillMaxWidth()
            .heightIn(max = 280.dp)
            .background(chatColors.fillWell, RoundedCornerShape(12.dp))
            .verticalScroll(rememberScrollState())
            .padding(12.dp),
    ) {
        Markdown(ix.plan.orEmpty().ifBlank { "（计划是空的）" })
    }
    if (rejecting) {
        OutlinedTextField(
            value = feedback,
            onValueChange = { feedback = it },
            placeholder = { Text("修改意见（可以不写）", maxLines = 1) },
            modifier = Modifier.fillMaxWidth(),
            enabled = !busy,
            maxLines = 5,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { rejecting = false }, enabled = !busy, modifier = Modifier.weight(1f)) {
                Text("取消", maxLines = 1)
            }
            Button(onClick = { submit(Resolutions.rejectPlan(feedback.trim())) }, enabled = !busy, modifier = Modifier.weight(1f)) {
                Text("打回修改", maxLines = 1)
            }
        }
    } else {
        Button(onClick = { submit(Resolutions.approvePlanAuto()) }, enabled = !busy, modifier = Modifier.fillMaxWidth()) {
            Text("批准并自动执行", maxLines = 1)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilledTonalButton(onClick = { submit(Resolutions.approvePlanAcceptEdits()) }, enabled = !busy, modifier = Modifier.weight(1f)) {
                Text("自动接受编辑", maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            OutlinedButton(onClick = { rejecting = true }, enabled = !busy, modifier = Modifier.weight(1f)) {
                Text("打回修改", maxLines = 1)
            }
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
            Modifier
                .fillMaxWidth()
                .heightIn(max = 200.dp)
                .background(chatColors.fillWell, RoundedCornerShape(12.dp))
                .verticalScroll(rememberScrollState())
                .padding(12.dp),
        ) {
            Text(detail, fontFamily = FontFamily.Monospace, fontSize = 13.sp, lineHeight = 18.sp)
        }
    }
    if (denying) {
        OutlinedTextField(
            value = reason,
            onValueChange = { reason = it },
            placeholder = { Text("告诉它为什么（可以不写）", maxLines = 1) },
            modifier = Modifier.fillMaxWidth(),
            enabled = !busy,
            maxLines = 4,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { denying = false }, enabled = !busy, modifier = Modifier.weight(1f)) { Text("取消", maxLines = 1) }
            Button(onClick = { submit(Resolutions.deny(reason)) }, enabled = !busy, modifier = Modifier.weight(1f)) { Text("拒绝", maxLines = 1) }
        }
    } else {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { denying = true }, enabled = !busy, modifier = Modifier.weight(1f)) { Text("拒绝", maxLines = 1) }
            Button(onClick = { submit(Resolutions.allow()) }, enabled = !busy, modifier = Modifier.weight(1f)) { Text("允许", maxLines = 1) }
        }
        if (ix.canAlwaysAllow) {
            TextButton(onClick = { submit(Resolutions.allow(always = true)) }, enabled = !busy, modifier = Modifier.fillMaxWidth()) {
                Text("本会话总是允许", maxLines = 1)
            }
        }
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
        Text(it, fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = c.secondaryLabel, maxLines = 1)
    }
    dialog.question.takeIf { it.isNotBlank() }?.let {
        Text(it, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, color = c.label)
    }
    dialog.detail?.takeIf { it.isNotBlank() }?.let {
        Box(
            Modifier.fillMaxWidth().heightIn(max = 180.dp)
                .background(c.fillWell, RoundedCornerShape(12.dp))
                .verticalScroll(rememberScrollState()).padding(10.dp),
        ) { Text(it, fontFamily = FontFamily.Monospace, fontSize = 13.sp, lineHeight = 18.sp, color = c.secondaryLabel) }
    }
    val choices = dialog.options.filter { it.kind != "chat" }
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(c.fillWell)) {
        choices.forEachIndexed { i, opt ->
            if (i > 0) HorizontalDivider(Modifier.padding(start = 50.dp), color = c.separator)
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
            TextButton(onClick = { sending = chat.key; submit(Resolutions.screenOption(chat.key)) }, enabled = !busy) {
                Icon(Icons.AutoMirrored.Rounded.Chat, null, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(6.dp))
                Text("改在对话里说", fontSize = 13.sp, fontWeight = FontWeight.Medium, maxLines = 1)
            }
        }
        Spacer(Modifier.weight(1f))
        TextButton(onClick = { showTerminal = !showTerminal }) {
            Icon(Icons.Rounded.Terminal, null, modifier = Modifier.size(16.dp))
            Spacer(Modifier.width(6.dp))
            Text(if (showTerminal) "收起终端" else "查看终端", fontSize = 13.sp, fontWeight = FontWeight.Medium, maxLines = 1)
        }
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
        ix.screen?.lineSequence()?.lastOrNull { it.isNotBlank() }?.let {
            Text(it.trim(), fontFamily = FontFamily.Monospace, fontSize = 13.sp, color = c.label)
        }
        OutlinedTextField(
            value = text,
            onValueChange = { text = it },
            placeholder = { Text("密码", maxLines = 1) },
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            singleLine = true,
            enabled = !busy,
            modifier = Modifier.fillMaxWidth(),
        )
        Text("只用这一次，不会保存", style = MaterialTheme.typography.labelSmall, color = c.secondaryLabel)
        Button(onClick = { submit(Resolutions.screenText(text)) }, enabled = !busy && text.isNotEmpty(), modifier = Modifier.fillMaxWidth()) {
            Text("发送", maxLines = 1)
        }
        return
    }
    ix.screen?.takeIf { it.isNotBlank() }?.let {
        Box(
            Modifier.fillMaxWidth().heightIn(max = 220.dp)
                .background(Color(0xFF111418), RoundedCornerShape(12.dp))
                .verticalScroll(rememberScrollState()).padding(10.dp),
        ) { Text(it.trimEnd(), fontFamily = FontFamily.Monospace, fontSize = 12.sp, lineHeight = 16.sp, softWrap = true, color = Color(0xFFE5E7EB)) }
    }
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        listOf("1" to listOf("1"), "2" to listOf("2"), "3" to listOf("3"), "回车" to listOf("enter"), "Esc" to listOf("esc"))
            .forEach { (label, keys) ->
                OutlinedButton(onClick = { submit(Resolutions.screenKeys(keys)) }, enabled = !busy) { Text(label, maxLines = 1) }
            }
    }
    OutlinedTextField(
        value = text,
        onValueChange = { text = it },
        placeholder = { Text("输入文字发送到终端", maxLines = 1) },
        modifier = Modifier.fillMaxWidth(),
        enabled = !busy,
        maxLines = 3,
        trailingIcon = if (text.isNotEmpty()) {
            { TextButton(onClick = { submit(Resolutions.screenText(text)) }, enabled = !busy) { Text("发送", maxLines = 1) } }
        } else null,
    )
}

/** The app's button: tonal, or filled when [prominent]. */
@Composable
private fun ThemedButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    prominent: Boolean = false,
    content: @Composable androidx.compose.foundation.layout.RowScope.() -> Unit,
) {
    if (prominent) Button(onClick, modifier, enabled, content = content)
    else FilledTonalButton(onClick, modifier, enabled, content = content)
}
