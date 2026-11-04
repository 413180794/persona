package persona.feature.chat

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.foundation.background
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Checkbox
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import persona.core.contracts.FeedItem
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JButtonSize
import persona.core.designsystem.JCard
import persona.core.designsystem.JChip
import persona.core.designsystem.JIcon
import persona.core.designsystem.JStatusDot
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Status
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.tasks.AssistantExtra
import persona.core.domain.tasks.AssistantLines
import persona.core.domain.tasks.QuickResult
import persona.core.domain.tasks.TaskState
import persona.core.domain.tasks.TasksStore

// The unified entry in the assistant's conversation (docs/unified-entry.md; iOS Views/Assistant/Tasks, web
// views/assistant/Tasks.tsx): the lines the assistant's host adds — plan cards, task cards, receipts, answered lines,
// routed bubbles, the 今天 card, message cards, quick results, day lines — and the dock and route chip above the composer.

/** What the assistant's chat needs to draw its tasks: the store, the sessions, and how to open an agent's chat. */
internal class AssistantTaskHost(
    val tasks: TasksStore,
    val conversation: String,
    val sessions: List<Session>,
    val machineName: (String) -> String,
    val onOpen: (SessionKey) -> Unit,
    val onError: (Throwable) -> Unit,
    val say: (String) -> Unit,
)

private fun TaskState.status(): Status = when (this) {
    TaskState.NeedsYou -> Status.NeedsYou
    TaskState.Working, TaskState.NotStarted -> Status.Working
    TaskState.Unread -> Status.Unread
    TaskState.Followup -> Status.Idle
    TaskState.Done -> Status.Offline
}

@Composable
private fun TaskMark(state: TaskState) {
    if (state == TaskState.Done || state == TaskState.Unread) {
        JIcon(PhIcons.CheckCircle, decorative = true, size = Tokens.Space.l,
            tint = if (state == TaskState.Done) PersonaTheme.palette.ink3 else PersonaTheme.palette.positive)
    } else JStatusDot(state.status())
}

private fun Todo.place(host: AssistantTaskHost, lead: Session?): String = buildList {
    val a = activeAgents.firstOrNull() ?: agents.firstOrNull()
    if (a != null) {
        add(host.machineName(a.machineId))
        a.cwd?.trimEnd('/')?.substringAfterLast('/')?.takeIf(String::isNotBlank)?.let(::add)
    }
    lead?.agentLabel?.let(::add)
}.joinToString(" · ")

private fun since(iso: String?): String? {
    val t = AssistantLines.time(iso) ?: return null
    val m = ((Instant.now().toEpochMilli() - t.toEpochMilli()) / 60_000).coerceAtLeast(0)
    return when {
        m < 1 -> "刚开始"
        m < 60 -> "$m 分钟"
        m % 60 == 0L -> "${m / 60} 小时"
        else -> "${m / 60} 小时 ${m % 60} 分"
    }
}

private fun openTask(host: AssistantTaskHost, todo: Todo) {
    val lead = host.tasks.lead(todo, host.sessions)
    val key = lead?.key ?: todo.activeAgents.firstOrNull()?.let { SessionKey(it.machineId, it.sessionId) } ?: return
    host.onOpen(key)
}

/** One of the host's lines in the conversation. */
@Composable
internal fun AssistantExtraLine(payload: Any, host: AssistantTaskHost?) {
    if (host == null) return
    when (payload) {
        is AssistantExtra.Day -> Text(AssistantLines.dayLabel(payload.date), Modifier.fillMaxWidth().padding(vertical = Tokens.Space.xs),
            style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3, textAlign = androidx.compose.ui.text.style.TextAlign.Center)
        is AssistantExtra.Quick -> QuickResultCard(payload.result)
        is AssistantExtra.Feed -> FeedLine(payload.item, host)
    }
}

@Composable
private fun FeedLine(item: FeedItem, host: AssistantTaskHost) {
    val todos by host.tasks.todos.collectAsStateWithLifecycle()
    val plans by host.tasks.plans.collectAsStateWithLifecycle()
    val later by host.tasks.later.collectAsStateWithLifecycle()
    val title = todos.firstOrNull { it.id == item.ref }?.title ?: "任务"
    when (item.kind) {
        "plan" -> plans[item.ref]?.let { PlanCard(it, host) }
        "task" -> todos.firstOrNull { it.id == item.ref }?.let { todo ->
            val today = AssistantLines.time(item.at)?.atZone(ZoneId.systemDefault())?.toLocalDate() == LocalDate.now()
            if (today) TaskCard(todo, host) else TaskLine(todo, host)
        }
        "answered" -> AnsweredLine(item, title, todos.firstOrNull { it.id == item.ref }?.let { todo -> { openTask(host, todo) } })
        "receipt" -> ReceiptCard(item, host, todos)
        "routed" -> Column(Modifier.fillMaxWidth().padding(start = Tokens.Space.xxxl), horizontalAlignment = Alignment.End,
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            Text("对「$title」说", style = PersonaTheme.type.caption, color = PersonaTheme.palette.accent, maxLines = 1)
            Text(item.field("text").orEmpty(), Modifier.clip(PersonaTheme.shapes.controlShape).background(PersonaTheme.palette.surfaceStrong)
                .padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.s), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
        }
        "today" -> TodayCard(item, host, todos)
        "message" -> if (item.ref != null && item.ref !in later) MessageCard(item, host, todos)
    }
}

/**
 * A card of a task's agent the user dealt with, kept as a line「✓ 任务 · 回答」. Tap it for every question with its
 * answer in full (#1231), selectable, and again to fold it; the task's name opens the task.
 */
@Composable
internal fun AnsweredLine(item: FeedItem, title: String, onOpenTask: (() -> Unit)?) {
    var open by rememberSaveable(item.id) { mutableStateOf(false) }
    val items = remember(item) { item.answeredItems() }
    val expandable = items.isNotEmpty()
    val shape = if (open) PersonaTheme.shapes.cardShape else CircleShape
    Column(Modifier.then(if (open) Modifier.fillMaxWidth() else Modifier).clip(shape).background(PersonaTheme.palette.surfaceStrong)
        .animateContentSize()) {
        Row(Modifier.then(if (expandable) Modifier.clickable(onClickLabel = if (open) "收起" else "展开") { open = !open } else Modifier)
            .semantics { if (expandable) stateDescription = if (open) "已展开" else "已收起" }
            .padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.xs),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            JIcon(PhIcons.Check, decorative = true, size = Tokens.Space.m, tint = PersonaTheme.palette.ink2)
            Text(title, (if (onOpenTask != null) Modifier.clickable(onClickLabel = "打开任务", onClick = onOpenTask) else Modifier)
                .weight(1f, fill = false), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text("· ${item.field("answer") ?: "回答了"}", Modifier.weight(1f, fill = false), style = PersonaTheme.type.caption,
                color = PersonaTheme.palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (expandable) JIcon(PhIcons.CaretRight, decorative = true, size = Tokens.Space.m, tint = PersonaTheme.palette.ink3,
                modifier = Modifier.rotate(if (open) 90f else 0f))
        }
        if (open) SelectionContainer {
            Column(Modifier.fillMaxWidth().padding(start = Tokens.Space.m, end = Tokens.Space.m, bottom = Tokens.Space.m)
                .testTag("answered-detail"), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                items.forEach { qa ->
                    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
                        if (qa.question.isNotEmpty()) Text(qa.question, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
                        if (qa.answer.isNotEmpty()) Text(qa.answer, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink)
                    }
                }
            }
        }
    }
}

// ── plans ──

private val PERMISSION = mapOf("acceptEdits" to "可以改文件", "plan" to "先出方案", "bypassPermissions" to "全部放行", "default" to "每步都问")

@Composable
private fun PlanCard(plan: TaskPlan, host: AssistantTaskHost) {
    val busy by host.tasks.busy.collectAsStateWithLifecycle()
    var remember by remember(plan.id) { mutableStateOf(false) }
    if (plan.state != "open") {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            Text(if (plan.state == "started") "✓ 开了 ${plan.tasks.size} 个任务" else "没开 · ${plan.tasks.size} 个任务",
                style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
            plan.tasks.filter { it.error != null }.forEach {
                Text("「${it.title}」没开起来：${it.error}", style = PersonaTheme.type.caption, color = PersonaTheme.palette.danger.text)
            }
        }
        return
    }
    JCard {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            Text(if (plan.tasks.size > 1) "拆成 ${plan.tasks.size} 件事，这样开" else "这样开", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            plan.tasks.forEachIndexed { n, t ->
                if (n > 0) HorizontalDivider(color = PersonaTheme.palette.line)
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                    Text(if (plan.tasks.size > 1) "${n + 1}. ${t.title}" else t.title, style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                        JChip(host.machineName(t.machine), icon = PhIcons.Laptop)
                        JChip(t.project, icon = PhIcons.Folder)
                        JChip(t.model?.takeIf(String::isNotBlank) ?: t.launcher?.takeIf(String::isNotBlank) ?: "默认模型", icon = PhIcons.Cpu)
                        JChip(if (t.readonly == true) "只读" else PERMISSION[t.permission ?: "default"] ?: t.permission.orEmpty(), selected = t.readonly == true)
                    }
                    t.why?.takeIf(String::isNotBlank)?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
                }
            }
            Row(Modifier.clickable { remember = !remember }, verticalAlignment = Alignment.CenterVertically) {
                Checkbox(remember, { remember = it })
                Text("这类以后直接开", style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JButton(if (plan.tasks.size > 1) "开始 ${plan.tasks.size} 个任务" else "开始", { host.tasks.startPlan(plan, remember, host.onError) },
                    Modifier.weight(1f), size = JButtonSize.Medium, loading = plan.id in busy)
                JButton("不开", { host.tasks.dismissPlan(plan, host.onError) }, kind = JButtonKind.Secondary, size = JButtonSize.Medium,
                    enabled = plan.id !in busy)
            }
        }
    }
}

// ── tasks ──

@Composable
private fun TaskCard(todo: Todo, host: AssistantTaskHost) {
    val state = host.tasks.stateOf(todo, host.sessions)
    val lead = host.tasks.lead(todo, host.sessions)
    val now = when {
        state == TaskState.NeedsYou -> "有事等你回答"
        !lead?.lastText.isNullOrBlank() -> lead?.lastText.orEmpty().trim()
        todo.agents.any { !it.startError.isNullOrBlank() } -> "没开起来：" + todo.agents.first { !it.startError.isNullOrBlank() }.startError
        state == TaskState.NotStarted -> "正在开 Agent"
        else -> "还没有动静"
    }
    JCard(onClick = { openTask(host, todo) }, status = if (state == TaskState.NeedsYou) Status.NeedsYou else null) {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                TaskMark(state)
                Text(todo.title, Modifier.weight(1f), style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (state != TaskState.Done) since(todo.createdAt)?.let { Text(it, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3) }
            }
            Text(todo.place(host, lead), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(now, Modifier.fillMaxWidth().clip(PersonaTheme.shapes.controlShape).background(PersonaTheme.palette.sunken)
                .padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.s), style = PersonaTheme.type.callout, maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                color = if (state == TaskState.NeedsYou) PersonaTheme.palette.status(Status.NeedsYou).text else PersonaTheme.palette.ink)
            if (state != TaskState.Done) Row(verticalAlignment = Alignment.CenterVertically) {
                Text(state.label, Modifier.weight(1f), style = PersonaTheme.type.caption, color = PersonaTheme.palette.status(state.status()).text)
                JButton("说一句", { host.tasks.setRoute(host.conversation, todo.id) }, kind = JButtonKind.Quiet, size = JButtonSize.Small,
                    icon = PhIcons.PaperPlaneTilt)
            }
        }
    }
}

/** A task of another day, folded into a line. */
@Composable
private fun TaskLine(todo: Todo, host: AssistantTaskHost) {
    val state = host.tasks.stateOf(todo, host.sessions)
    Row(Modifier.fillMaxWidth().clip(PersonaTheme.shapes.controlShape).background(PersonaTheme.palette.sunken)
        .clickable { openTask(host, todo) }.padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.s),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        TaskMark(state)
        Text(todo.title, Modifier.weight(1f), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(state.label, style = PersonaTheme.type.caption, color = PersonaTheme.palette.status(state.status()).text, maxLines = 1)
    }
}

@Composable
private fun ReceiptCard(item: FeedItem, host: AssistantTaskHost, todos: List<Todo>) {
    val failed = item.field("state") == "failed"
    val tone = if (failed) PersonaTheme.palette.danger else PersonaTheme.palette.done
    Row(Modifier.fillMaxWidth().clip(PersonaTheme.shapes.cardShape).background(tone.tint).padding(Tokens.Space.l),
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        JIcon(if (failed) PhIcons.WarningCircle else PhIcons.CheckCircle, decorative = true, tint = tone.dot)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            Text((item.field("title") ?: "任务") + if (failed) "没开起来" else " 完成", style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
            item.field("summary")?.let { Text(it, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2, maxLines = 4, overflow = TextOverflow.Ellipsis) }
            val todo = todos.firstOrNull { it.id == item.ref }
            if (!failed && todo != null) JButton("看看", { openTask(host, todo) }, kind = JButtonKind.Secondary, size = JButtonSize.Small)
        }
    }
}

@Composable
private fun QuickResultCard(result: QuickResult) {
    val time = AssistantLines.time(result.time)?.atZone(ZoneId.systemDefault())
        ?.format(DateTimeFormatter.ofPattern("M月d日 HH:mm"))
    Row(Modifier.fillMaxWidth().clip(PersonaTheme.shapes.cardShape).background(PersonaTheme.palette.sunken).padding(Tokens.Space.m),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        JIcon(if (result.reminder) PhIcons.BellSimple else PhIcons.CalendarCheck, decorative = true,
            tint = if (result.reminder) PersonaTheme.palette.caution else PersonaTheme.palette.accent)
        Column(Modifier.weight(1f)) {
            Text(if (result.reminder) "已加提醒" else "已加到日历", style = PersonaTheme.type.bodyStrong, color = PersonaTheme.palette.ink)
            Text(listOfNotNull(time, result.title, result.place).joinToString(" · "), style = PersonaTheme.type.caption,
                color = PersonaTheme.palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Text("在 iPhone 上", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
    }
}

// ── 今天 ──

private fun JsonObject.list(name: String): List<JsonObject> = (this[name] as? JsonArray)?.mapNotNull { it as? JsonObject }.orEmpty()
private fun JsonObject.str(name: String): String? = (this[name] as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull

@Composable
private fun TodayCard(item: FeedItem, host: AssistantTaskHost, todos: List<Todo>) {
    val body = item.body as? JsonObject ?: return
    data class TodayRow(val icon: PhIcon, val status: Status?, val title: String, val detail: String?, val action: String?, val run: (() -> Unit)?)
    val rows = buildList {
        val done = body.list("done")
        if (done.isNotEmpty()) add(TodayRow(PhIcons.CheckCircle, Status.Unread,
            if (done.size == 1) "做完了：${done[0].str("title")}" else "做完了 ${done.size} 个任务",
            done.takeIf { it.size > 1 }?.joinToString("、") { it.str("title").orEmpty() }, "看看",
            todos.firstOrNull { it.id == done[0].str("id") }?.let { t -> { openTask(host, t) } }))
        val waiting = body.list("waiting")
        if (waiting.isNotEmpty()) add(TodayRow(PhIcons.Hand, Status.NeedsYou,
            if (waiting.size == 1) "等你：${waiting[0].str("title")}" else "${waiting.size} 件等你", null, "去看",
            todos.firstOrNull { it.id == waiting[0].str("id") }?.let { t -> { openTask(host, t) } }))
        item.number("working")?.takeIf { it > 0 }?.let { add(TodayRow(PhIcons.GearSix, Status.Working, "$it 个任务在做", null, null, null)) }
        body.list("meetings").take(3).forEach { m ->
            val t = AssistantLines.time(m.str("start"))?.atZone(ZoneId.systemDefault())?.format(DateTimeFormatter.ofPattern("HH:mm")).orEmpty()
            val name = m.str("title").orEmpty()
            add(TodayRow(PhIcons.CalendarBlank, null, "$t $name", null, "准备") { host.say("帮我准备一下 $t 的「$name」：要看的文档、上次的结论、要我拍板的事") })
        }
        val atMe = body.list("at_me")
        if (atMe.isNotEmpty()) add(TodayRow(PhIcons.At, null,
            "飞书 ${atMe.size} 个会话 @你" + (item.number("unread")?.takeIf { it > 0 }?.let { " · 其他未读 $it" } ?: ""),
            atMe.take(2).joinToString("\n") { c -> c.str("name").orEmpty() + (c.str("preview")?.let { "：$it" } ?: "") }, "过一遍")
            { host.say("帮我过一遍飞书里 @我 的消息：哪些要我回，能起草的先起草") })
    }
    JCard {
        Text("今天要知道的", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        if (rows.isEmpty()) Text("没有等你的事，日程也空着。", style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
        rows.forEachIndexed { n, r ->
            if (n > 0) HorizontalDivider(color = PersonaTheme.palette.line)
            Row(Modifier.padding(vertical = Tokens.Space.s), verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                JIcon(r.icon, decorative = true, tint = r.status?.let { PersonaTheme.palette.status(it).text } ?: PersonaTheme.palette.accent)
                Column(Modifier.weight(1f)) {
                    Text(r.title, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    r.detail?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2, maxLines = 2, overflow = TextOverflow.Ellipsis) }
                }
                if (r.action != null && r.run != null) JButton(r.action, r.run, kind = JButtonKind.Secondary, size = JButtonSize.Small)
            }
        }
    }
}

// ── message cards ──

@Composable
private fun MessageCard(item: FeedItem, host: AssistantTaskHost, todos: List<Todo>) {
    val itemId = item.ref ?: return
    val sent by host.tasks.sent.collectAsStateWithLifecycle()
    val busy by host.tasks.busy.collectAsStateWithLifecycle()
    var draft by remember(item.id) { mutableStateOf(item.field("draft").orEmpty()) }
    val done = itemId in sent
    val todo = todos.firstOrNull { it.id == item.field("todo_id") }
    JCard {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            Text(listOfNotNull(item.field("sender"), item.field("chat")).joinToString(" · "), style = PersonaTheme.type.caption,
                color = PersonaTheme.palette.ink2, maxLines = 1)
            Text(item.field("text").orEmpty(), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink, maxLines = 4, overflow = TextOverflow.Ellipsis)
            if (todo != null) {
                val state = host.tasks.stateOf(todo, host.sessions)
                Row(Modifier.fillMaxWidth().clip(PersonaTheme.shapes.controlShape).background(PersonaTheme.palette.sunken)
                    .clickable { openTask(host, todo) }.padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.s),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    TaskMark(state)
                    Text(todo.title, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink, maxLines = 1)
                    Text(host.tasks.lead(todo, host.sessions)?.lastText ?: state.label, Modifier.weight(1f), style = PersonaTheme.type.caption,
                        color = PersonaTheme.palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
            Column(Modifier.fillMaxWidth().clip(PersonaTheme.shapes.controlShape)
                .background(if (done) PersonaTheme.palette.done.tint else PersonaTheme.palette.accentSoft).padding(Tokens.Space.m),
                verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                Text(if (done) "已以你的名义回复" else "按任务进度起草的回复", style = PersonaTheme.type.caption,
                    color = if (done) PersonaTheme.palette.done.text else PersonaTheme.palette.accent)
                if (done) Text(draft, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
                else BasicTextField(draft, { draft = it }, Modifier.fillMaxWidth(),
                    textStyle = PersonaTheme.type.body.copy(color = PersonaTheme.palette.ink),
                    cursorBrush = SolidColor(PersonaTheme.palette.accent))
            }
            if (!done) Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JButton("发送", { host.tasks.sendMessage(itemId, draft, host.onError) }, Modifier.weight(1f), size = JButtonSize.Medium,
                    loading = itemId in busy)
                JButton("稍后", { host.tasks.putOff(itemId) }, kind = JButtonKind.Secondary, size = JButtonSize.Medium)
            }
        }
    }
}

// ── above the composer ──

/** The dock (a pill for each task going on, waiting first) and the route chip. */
@Composable
internal fun AssistantTaskBar(host: AssistantTaskHost) {
    val todos by host.tasks.todos.collectAsStateWithLifecycle()
    val routes by host.tasks.routes.collectAsStateWithLifecycle()
    val mine = todos.filter { it.origin == host.conversation }
    val going = mine.map { it to host.tasks.stateOf(it, host.sessions) }
        .filter { (_, s) -> s == TaskState.NeedsYou || s == TaskState.Working || s == TaskState.NotStarted || s == TaskState.Unread }
        .sortedWith(compareBy({ it.second.rank }, { it.first.createdAt?.let { c -> -(AssistantLines.time(c)?.toEpochMilli() ?: 0) } ?: 0 }))
    Column(Modifier.padding(horizontal = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        if (going.isNotEmpty()) Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            going.forEach { (todo, state) ->
                JChip(if (state == TaskState.NeedsYou) "${todo.title} · 等你" else todo.title, onClick = { openTask(host, todo) }, status = state.status())
            }
        }
        val routed = routes[host.conversation]?.let { id -> mine.firstOrNull { it.id == id } }
        if (routed != null) JChip("对「${routed.title}」说 ×", selected = true, onClick = { host.tasks.setRoute(host.conversation, null) }, icon = PhIcons.PaperPlaneTilt)
    }
}

/** The assistant chat's tasks, for its lines (set by the chat page when it draws an assistant conversation). */
internal val LocalAssistantTaskHost = compositionLocalOf<AssistantTaskHost?> { null }
