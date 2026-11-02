package jarvis.android.ui

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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import jarvis.android.data.AgentState
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.parseTime
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.ChoiceChip
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.DOT_SEP
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.GroupFooter
import jarvis.android.ui.design.IconAction
import jarvis.android.ui.design.IconTile
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.JarvisMark
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.PageLoading
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.settings.KitGroup
import jarvis.android.ui.settings.KitPage
import jarvis.android.ui.settings.KitRow
import jarvis.android.ui.settings.jSwitchColors
import kotlinx.coroutines.launch
import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit
import kotlin.math.abs

/**
 * The assistant's profile (iOS AssistantProfileView): who it is and where it runs, what's coming up, its routines and
 * follow-ups with their run history, what it knows about the user (「关于我」, edited here) and remembers (memory
 * files), and what it can reach. Each part is a pushed page of its own, with the round back button.
 */
enum class AssistantPage { Profile, Routines, Followups, Runs, Notes, Memory }

/**
 * What the pages show before their own load comes back: the screenshot tests read it from the mock hub first (a
 * page can't wait for the network there). Each page still loads when it opens.
 */
class AssistantPreload(
    val routines: List<ApiClient.Routine>? = null,
    val followups: List<ApiClient.Followup>? = null,
    val runs: List<ApiClient.Run>? = null,
    val notes: String? = null,
    val memory: List<ApiClient.MemoryFile>? = null,
)

@Composable
fun AssistantProfile(
    hub: Hub, machineName: String, onBack: () -> Unit, onOpenConversation: (String) -> Unit,
    /** Where it opens (the drawer's 定时任务 opens on the routines); back from there leaves. */
    start: AssistantPage = AssistantPage.Profile,
    /** Data to show at once (the screenshots); by default each page starts empty and loads. */
    preload: AssistantPreload? = null,
) {
    var page by remember { mutableStateOf(start) }
    val back = { if (page == start) onBack() else page = start }
    androidx.activity.compose.BackHandler { back() }
    when (page) {
        AssistantPage.Profile -> ProfileHome(hub, machineName, preload, back) { page = it }
        AssistantPage.Routines -> RoutinesPage(hub, preload, back, onOpenConversation)
        AssistantPage.Followups -> FollowupsPage(hub, preload, back, onOpenConversation)
        AssistantPage.Runs -> RunsPage(hub, preload, back, onOpenConversation)
        AssistantPage.Notes -> NotesPage(hub, preload, back)
        AssistantPage.Memory -> MemoryPage(hub, preload, back)
    }
}

// --- 资料 ---

@Composable
private fun ProfileHome(hub: Hub, machineName: String, preload: AssistantPreload?, onBack: () -> Unit, go: (AssistantPage) -> Unit) {
    var routines by remember { mutableStateOf(preload?.routines) }
    var followups by remember { mutableStateOf(preload?.followups) }
    LaunchedEffect(Unit) {
        runCatching { routines = hub.api.routines() }
        runCatching { followups = hub.api.followups() }
    }
    val machines by hub.machines.collectAsState()
    val byMachine by hub.sessions.collectAsState()
    // The shell passes the machine's name (or its id when the hub has no name for it).
    val machine = machines.firstOrNull { it.displayName == machineName || it.id == machineName }
    val working = byMachine.values.flatten().any { it.assistant && it.agentState == AgentState.Working && (machine == null || it.machineId == machine.id) }
    // Next three things it will do on its own: enabled routines' next runs and pending follow-ups.
    val upcoming = Upcoming.of(routines.orEmpty(), followups.orEmpty()).take(3)
    val c = JTheme.colors
    val hue = JTheme.hue

    KitPage("Jarvis", onBack) {
        item(key = "head") { ProfileHeader(machineName, machine?.online, working, upcoming.firstOrNull()) }
        if (upcoming.isNotEmpty()) item(key = "next") {
            KitGroup(header = "接下来") { upcoming.forEach { u -> row { UpcomingRow(u) } } }
        }
        item(key = "proactive") {
            KitGroup(header = "主动做事", footer = "到点自己开始工作、跟进答应你的事，跑完推送到手机。在对话里说「每天 9 点给我早报」「一小时后提醒我」就会加到这里。") {
                row {
                    KitRow("定时任务", { go(AssistantPage.Routines) }, icon = JIcons.schedule, iconColor = hue.orange,
                        value = routines?.count { it.enabled }?.let { if (it == 0) "未开启" else "$it 个开启" })
                }
                row {
                    KitRow("跟进", { go(AssistantPage.Followups) }, icon = JIcons.followOn, iconColor = hue.blue,
                        value = followups?.let { if (it.isEmpty()) "没有" else "${it.size} 个" })
                }
                row { KitRow("运行记录", { go(AssistantPage.Runs) }, icon = JIcons.recent, iconColor = hue.green) }
            }
        }
        item(key = "you") {
            KitGroup(header = "了解你", footer = "「关于我」是你告诉 Jarvis 的；「记忆」是它在对话里自己记下的，都可以改。") {
                row { KitRow("关于我", { go(AssistantPage.Notes) }, icon = JIcons.aboutMe, iconColor = c.accent) }
                row { KitRow("记忆", { go(AssistantPage.Memory) }, icon = JIcons.memory, iconColor = hue.purple) }
            }
        }
        item(key = "reach") {
            val names = machines.map { it.displayName }
            KitGroup(header = "能访问", footer = "发飞书消息、发邮件、新建日程这类会替你出面的操作，都先在手机上问你。", separatorStart = ReachTextColumn) {
                row { ReachRow(JIcons.machines, if (names.isEmpty()) "hub 接入的各台电脑（ssh）" else names.joinToString("、") + "（ssh）") }
                row { ReachRow(JIcons.agents, "各台电脑上的 Agent：看状态、读对话、发消息、开新的") }
                row { ReachRow(JIcons.feishuChats, "飞书：消息、日历、文档、会议纪要、任务、邮件") }
                row { ReachRow(JIcons.mail, "你添加的所有邮箱") }
                row { ReachRow(JIcons.notifications, "在手机上问你、给你推送") }
            }
        }
    }
}

/** Jarvis' face and name, where it runs, and its state: online, at work, what it does next (iOS's header). */
@Composable
private fun ProfileHeader(machineName: String, online: Boolean?, working: Boolean, next: Upcoming?) {
    val c = JTheme.colors
    Column(
        Modifier.fillMaxWidth().padding(start = JSpace.page, end = JSpace.page, top = JSpace.xl, bottom = JSpace.xs),
        horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        // The mark glows in its own coral (the one place the brand's color shows).
        JarvisMark(JSize.box80, Modifier.shadow(JSpace.l, CircleShape, ambientColor = c.brand, spotColor = c.brand))
        Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            Text("Jarvis", style = JTheme.type.display, color = c.label, maxLines = 1)
            Text("你的私人助手 · 运行在 $machineName", style = JTheme.type.secondary, color = c.secondaryText, maxLines = 1,
                overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalAlignment = Alignment.CenterVertically) {
            if (online != null) Chip(if (online) "在线" else "离线", icon = if (online) JIcons.success else JIcons.offline,
                look = if (online) ChipLook.Tint(c.positive) else ChipLook.Neutral)
            if (working) Chip("工作中", icon = JIcons.more, look = ChipLook.Tint(c.accent))
            next?.let { Chip("下一件 ${moment(it.at)}", icon = JIcons.schedule) }
        }
    }
}

/** One thing Jarvis will do on its own: a routine's next run, or a follow-up (iOS `AssistantUpcoming`). */
private data class Upcoming(val at: Instant, val title: String, val routine: Boolean, val detail: String) {
    companion object {
        /** Soonest first; what is switched off is not coming up. */
        fun of(routines: List<ApiClient.Routine>, followups: List<ApiClient.Followup>): List<Upcoming> =
            (routines.filter { it.enabled }.mapNotNull { r -> parseTime(r.nextRun)?.let { Upcoming(it, r.title, true, sentence(r)) } } +
                followups.mapNotNull { f ->
                    parseTime(f.at)?.let {
                        Upcoming(it, f.title?.takeIf { t -> t.isNotBlank() } ?: f.prompt.take(30), false,
                            if (f.sessionId == null) "跟进 · 新对话" else "跟进 · 接着原来的对话")
                    }
                })
                .sortedBy { it.at }
    }
}

/** A line like Reminders' (iOS `AssistantUpcomingRow`): the time big on the left, what happens on the right. */
@Composable
private fun UpcomingRow(u: Upcoming) {
    val c = JTheme.colors
    Row(Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m), horizontalArrangement = Arrangement.spacedBy(JSpace.l)) {
        Column(Modifier.width(JSize.box56).alignByBaseline(), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(hm(u.at), style = JTheme.type.heading, color = c.label, maxLines = 1)
            Text(day(u.at), style = JTheme.type.label, color = c.secondaryText, maxLines = 1)
        }
        Column(Modifier.weight(1f).alignByBaseline(), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                Icon(if (u.routine) JIcons.recurring else JIcons.followOn, null, Modifier.size(JSize.markS), tint = c.accent)
                Text(u.title, style = JTheme.type.heading, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Text(u.detail + DOT_SEP + until(u.at), style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** Something Jarvis can reach: its symbol and what it can do there. */
@Composable
private fun ReachRow(icon: ImageVector, text: String) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.box50).padding(horizontal = JSpace.l, vertical = JSpace.m),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Icon(icon, null, Modifier.size(JSize.iconS), tint = JTheme.colors.accent)
        Text(text, style = JTheme.type.body, color = JTheme.colors.label)
    }
}

/** Where the words of the pages' rows start (after the symbol, the tile): their separators start there. */
private val ReachTextColumn: Dp = JSpace.l + JSize.iconS + JSpace.m
private val TileTextColumn: Dp = JSpace.l + JSize.wellM + JSpace.m

// --- Formatting (iOS `AssistantScheduleText`) ---

private val zone: ZoneId get() = ZoneId.systemDefault()
private val HM: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")
private val WEEKDAYS = listOf("一", "二", "三", "四", "五", "六", "日")
private val WORKDAYS = setOf(1, 2, 3, 4, 5)
private val WEEKEND = setOf(6, 7)
private val EVERY_DAY = (1..7).toSet()

private fun weekday(d: Int) = WEEKDAYS.getOrNull(d - 1) ?: "$d"

/** "17:05", always 24-hour. */
private fun hm(at: Instant): String = at.atZone(zone).format(HM)

/** The day part of a moment: 今天 / 明天 / 昨天 / 周三 (within a week either way) / 11月2日. */
private fun day(at: Instant, today: LocalDate = LocalDate.now(zone)): String {
    val d = at.atZone(zone).toLocalDate()
    val days = ChronoUnit.DAYS.between(today, d)
    return when {
        days == 0L -> "今天"
        days == 1L -> "明天"
        days == -1L -> "昨天"
        abs(days) < 7 -> "周" + weekday(d.dayOfWeek.value)
        else -> "${d.monthValue}月${d.dayOfMonth}日"
    }
}

/** 今天 15:30 / 明天 09:00 / 周三 09:00 (later this week) / 11月2日 09:00. */
private fun moment(at: Instant, today: LocalDate = LocalDate.now(zone)): String {
    val d = at.atZone(zone).toLocalDate()
    val days = ChronoUnit.DAYS.between(today, d)
    val day = when {
        days == 0L -> "今天"
        days == 1L -> "明天"
        days in 2..6 -> "周" + weekday(d.dayOfWeek.value)
        else -> "${d.monthValue}月${d.dayOfMonth}日"
    }
    return "$day ${hm(at)}"
}

/** A future moment, roughly: 马上 / 25 分钟后 / 2 小时后 / 1 小时 30 分钟后 / 3 天后; 已过时间 once past. */
private fun until(at: Instant, now: Instant = Instant.now()): String {
    val s = Duration.between(now, at).seconds
    if (s < 60) return if (s < -60) "已过时间" else "马上"
    if (s < 3600) return "${s / 60} 分钟后"
    if (s < 86400) {
        val h = s / 3600
        val m = s % 3600 / 60
        return if (h < 3 && m >= 10) "$h 小时 $m 分钟后" else "$h 小时后"
    }
    return "${maxOf(ChronoUnit.DAYS.between(now.atZone(zone).toLocalDate(), at.atZone(zone).toLocalDate()), 1)} 天后"
}

/** "09:00" → "早上 9:00", "21:30" → "晚上 9:30", "00:15" → "凌晨 12:15". */
private fun clock(hhmm: String): String {
    val parts = hhmm.split(":").mapNotNull { it.toIntOrNull() }
    if (parts.size != 2) return hhmm
    val (h, m) = parts
    val part = when (h) {
        in 0..4 -> "凌晨"
        in 5..8 -> "早上"
        in 9..11 -> "上午"
        12 -> "中午"
        in 13..17 -> "下午"
        else -> "晚上"
    }
    return "%s %d:%02d".format(part, if (h % 12 == 0) 12 else h % 12, m)
}

/** The rule in words, as Reminders says it: 每个工作日 早上 9:00 / 每天 晚上 9:30 / 每周五 下午 5:00. */
private fun sentence(r: ApiClient.Routine): String {
    val d = r.weekdays.toSet()
    val days = when {
        d.isEmpty() || d == EVERY_DAY -> "每天"
        d == WORKDAYS -> "每个工作日"
        d == WEEKEND -> "每个周末"
        else -> "每周" + d.sorted().joinToString("、") { weekday(it) }
    }
    return "$days ${clock(r.time)}"
}

/** The last run in a few words and whether it failed: 上次 今天 09:00 · 已运行. */
private fun lastRun(r: ApiClient.Routine): Pair<String, Boolean>? {
    val failed = r.lastStatus == "failed"
    val at = parseTime(r.lastRun)
    return when {
        at != null -> "上次 ${moment(at)}$DOT_SEP" + when (r.lastStatus) { "failed" -> "失败"; "skipped" -> "已跳过"; else -> "已运行" } to failed
        failed -> "上次没跑起来" to true
        r.lastStatus == "skipped" -> "上次跳过了" to false
        else -> null
    }
}

// --- 定时任务 ---

@Composable
private fun RoutinesPage(hub: Hub, preload: AssistantPreload?, onBack: () -> Unit, onOpenConversation: (String) -> Unit) {
    var list by remember { mutableStateOf(preload?.routines) }
    var editing by remember { mutableStateOf<ApiClient.Routine?>(null) }
    var adding by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf<ApiClient.Routine?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { runCatching { list = hub.api.routines() }.onFailure { error = it.message } }
    fun save(new: List<ApiClient.Routine>) {
        val before = list
        list = new
        scope.launch {
            runCatching { list = hub.api.setRoutines(new) }.onFailure {
                android.util.Log.w(Hub.TAG, "save routines", it)
                list = before; error = "没保存上：${it.message ?: it.javaClass.simpleName}"
            }
        }
    }
    KitPage("定时任务", onBack, actions = { CircleButton(JIcons.add, "新建定时任务", { adding = true }) }) {
        error?.let { item(key = "error") { ErrorLine(it, Modifier.padding(start = JSpace.page + JSpace.xs, end = JSpace.page, top = JSpace.s)) } }
        val l = list
        when {
            l == null -> if (error == null) item(key = "loading") { PageLoading() }
            l.isEmpty() -> item(key = "empty") {
                EmptyState(JIcons.schedule, "还没有定时任务", "点右上角的 + 新建，或者在对话里说「每天 9 点给我早报」。", Modifier.padding(top = JSpace.xxl))
            }
            else -> item(key = "list") {
                KitGroup(footer = "到点时 Jarvis 按提示词去做，跑完推送到手机。时间按手机所在的时区。", separatorStart = TileTextColumn) {
                    l.forEach { r ->
                        row {
                            RoutineRow(r, onOpen = { editing = r }) { on -> save(list.orEmpty().map { if (it.id == r.id) it.copy(enabled = on) else it }) }
                        }
                    }
                }
            }
        }
    }
    val shown = editing ?: if (adding) ApiClient.Routine(time = "09:00", weekdays = listOf(1, 2, 3, 4, 5)) else null
    shown?.let { r ->
        RoutineEditor(
            r, isNew = r.id.isEmpty(),
            onDismiss = { editing = null; adding = false },
            onSave = { updated ->
                editing = null; adding = false
                save(if (r.id.isEmpty()) list.orEmpty() + updated else list.orEmpty().map { if (it.id == r.id) updated else it })
            },
            onDelete = { editing = null; deleting = r },
            onRunNow = {
                editing = null
                scope.launch {
                    runCatching { hub.api.runRoutine(r.id) }.onSuccess { onOpenConversation(it.sessionId) }.onFailure { error = "没跑起来：${it.message}" }
                }
            },
        )
    }
    deleting?.let { r ->
        AlertDialog(
            onDismissRequest = { deleting = null },
            title = { Text("删除「${r.title}」？") },
            text = { Text("删除后不再定时运行，已经跑过的对话还在。") },
            confirmButton = {
                TextButton({ deleting = null; save(list.orEmpty().filterNot { it.id == r.id }) }) { Text("删除", color = JTheme.colors.danger, maxLines = 1) }
            },
            dismissButton = { TextButton({ deleting = null }) { Text("取消", maxLines = 1) } },
        )
    }
}

/** A routine's symbol, from its title (iOS `AssistantScheduleText.symbol`). */
private fun routineIcon(r: ApiClient.Routine): ImageVector {
    val t = r.title
    return when {
        "早" in t -> JIcons.morning
        "晚" in t || "回顾" in t -> JIcons.evening
        "周报" in t || "报告" in t -> JIcons.document
        "会" in t -> JIcons.people
        "盘点" in t || "待办" in t -> JIcons.todo
        "巡检" in t || "检查" in t -> JIcons.patrol
        else -> JIcons.schedule
    }
}

/**
 * One routine (iOS `AssistantTaskRow`): its symbol on the accent (gray when off), the title and who made it, the
 * rule in words, the next run, the last one; the switch at the end. Tapping elsewhere opens the editor.
 */
@Composable
private fun RoutineRow(r: ApiClient.Routine, onOpen: () -> Unit, onToggle: (Boolean) -> Unit) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onOpen).padding(horizontal = JSpace.l, vertical = JSpace.m),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Row(Modifier.weight(1f).alpha(if (r.enabled) 1f else 0.75f), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
            IconTile(routineIcon(r), if (r.enabled) c.accent else c.secondaryText, size = JSize.wellM)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    Text(r.title, style = JTheme.type.heading, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                    if (r.createdBy == "assistant") Chip("Jarvis 建的", look = ChipLook.Tint(c.accent))
                }
                Text(sentence(r), style = JTheme.type.secondary, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val next = parseTime(r.nextRun)
                val nextText = when {
                    !r.enabled -> "已关闭"
                    next != null -> "下次 ${moment(next)}$DOT_SEP${until(next)}"
                    else -> null
                }
                nextText?.let { Text(it, style = JTheme.type.label, color = c.secondaryText, maxLines = 1) }
                lastRun(r)?.let { (text, failed) ->
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                        Icon(if (failed) JIcons.error else JIcons.done, null, Modifier.size(JSize.markXS), tint = if (failed) c.danger else c.secondaryText)
                        Text(text, style = JTheme.type.label, color = if (failed) c.danger else c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
        }
        Switch(r.enabled, onToggle, colors = jSwitchColors())
    }
}

@Composable
private fun RoutineEditor(
    r: ApiClient.Routine, isNew: Boolean, onDismiss: () -> Unit, onSave: (ApiClient.Routine) -> Unit, onDelete: () -> Unit, onRunNow: () -> Unit,
) {
    JSheet(onDismiss, title = if (isNew) "新建定时任务" else "定时任务", onClose = onDismiss) {
        RoutineForm(r, isNew, onSave, onDelete, onRunNow)
    }
}

/**
 * The routine editor's body (drawn alone by the screenshots, which can't show a sheet's own window): its name, what
 * Jarvis does, when and on which days; 立即运行 and 删除 for one that exists; 保存.
 */
@Composable
internal fun RoutineForm(r: ApiClient.Routine, isNew: Boolean, onSave: (ApiClient.Routine) -> Unit, onDelete: () -> Unit, onRunNow: () -> Unit) {
    var title by remember { mutableStateOf(r.title) }
    var prompt by remember { mutableStateOf(r.prompt) }
    var time by remember { mutableStateOf(r.time) }
    var days by remember { mutableStateOf(r.weekdays.ifEmpty { (1..7).toList() }.toSet()) }
    val timeOk = Regex("^([01]\\d|2[0-3]):[0-5]\\d$").matches(time)
    val c = JTheme.colors
    Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).imePadding()) {
        SectionHeader("名称")
        JTextField(title, { title = it }, "比如「早报」", Modifier.padding(horizontal = JSpace.page), singleLine = true, fill = c.card)
        SectionHeader("到点让 Jarvis 做什么")
        JTextField(prompt, { prompt = it }, "像在对话里一样写：看哪些数据、怎么汇报", Modifier.padding(horizontal = JSpace.page),
            minLines = 4, maxLines = 10, fill = c.card)
        SectionHeader("时间")
        JTextField(time, { time = it.take(5) }, "09:00", Modifier.padding(horizontal = JSpace.page), singleLine = true, fill = c.card)
        if (!timeOk) ErrorLine("按 24 小时写成「时:分」，比如 09:00", Modifier.padding(start = JSpace.page + JSpace.xs, end = JSpace.page, top = JSpace.s))
        SectionHeader("重复")
        WeekdayPicker(days, { days = it }, Modifier.padding(horizontal = JSpace.page))
        if (!isNew) {
            Row(Modifier.padding(start = JSpace.page, end = JSpace.page, top = JSpace.xl), horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                PillButton("立即运行", onRunNow, Modifier.weight(1f), kind = ButtonKind.Tinted, icon = JIcons.run)
                PillButton("删除", onDelete, Modifier.weight(1f), kind = ButtonKind.Destructive, icon = JIcons.delete)
            }
        }
        PillButton(
            "保存",
            { onSave(r.copy(title = title.trim(), prompt = prompt.trim(), time = time, weekdays = if (days.size == 7) emptyList() else days.sorted())) },
            Modifier.fillMaxWidth().padding(start = JSpace.page, end = JSpace.page, top = if (isNew) JSpace.xl else JSpace.m),
            kind = ButtonKind.Prominent, large = true,
            enabled = title.isNotBlank() && prompt.isNotBlank() && timeOk && days.isNotEmpty(),
        )
    }
}

/** Seven round day buttons (一 … 日), then 每天 / 工作日 / 周末 (iOS `AssistantWeekdayPicker`). */
@Composable
private fun WeekdayPicker(days: Set<Int>, onChange: (Set<Int>) -> Unit, modifier: Modifier = Modifier) {
    val c = JTheme.colors
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            (1..7).forEach { d ->
                val on = d in days
                Box(
                    Modifier.size(JSize.wellXL).clip(CircleShape).background(if (on) c.accent else c.fillChip)
                        .clickable(role = Role.Checkbox) { onChange(if (on) days - d else days + d) }
                        .semantics { contentDescription = "周" + weekday(d); selected = on },
                    contentAlignment = Alignment.Center,
                ) { Text(weekday(d), style = JTheme.type.secondaryStrong, color = if (on) Color.White else c.label, maxLines = 1) }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
            listOf("每天" to EVERY_DAY, "工作日" to WORKDAYS, "周末" to WEEKEND).forEach { (t, v) -> ChoiceChip(t, days == v, { onChange(v) }) }
        }
    }
}

// --- 跟进 ---

@Composable
private fun FollowupsPage(hub: Hub, preload: AssistantPreload?, onBack: () -> Unit, onOpenConversation: (String) -> Unit) {
    var list by remember { mutableStateOf(preload?.followups) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    suspend fun load() { runCatching { list = hub.api.followups() }.onFailure { error = it.message } }
    LaunchedEffect(Unit) { load() }
    KitPage("跟进", onBack) {
        error?.let { item(key = "error") { ErrorLine(it, Modifier.padding(start = JSpace.page + JSpace.xs, end = JSpace.page, top = JSpace.s)) } }
        val l = list
        when {
            l == null -> if (error == null) item(key = "loading") { PageLoading() }
            l.isEmpty() -> item(key = "empty") {
                EmptyState(JIcons.followOn, "没有跟进", "对话里说「一小时后提醒我…」「明天上午再看看…」时会出现在这里。", Modifier.padding(top = JSpace.xxl))
            }
            else -> item(key = "list") {
                KitGroup(footer = "Jarvis 在对话里替自己安排的，到点自己醒来接着做。", separatorStart = TileTextColumn) {
                    l.forEach { f ->
                        row {
                            FollowupRow(
                                f,
                                onRun = {
                                    scope.launch {
                                        runCatching { hub.api.runFollowup(f.id) }.onSuccess { onOpenConversation(it.sessionId) }.onFailure { error = it.message }
                                    }
                                },
                                onCancel = { scope.launch { runCatching { hub.api.cancelFollowup(f.id) }.onFailure { error = it.message }; load() } },
                            )
                        }
                    }
                }
            }
        }
    }
}

/** One follow-up: what Jarvis will come back to, when and in which conversation; 现在做 and 取消 under it. */
@Composable
private fun FollowupRow(f: ApiClient.Followup, onRun: () -> Unit, onCancel: () -> Unit) {
    val c = JTheme.colors
    Row(Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
        IconTile(JIcons.followOn, c.accent, size = JSize.wellM)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(f.title?.takeIf { it.isNotBlank() } ?: f.prompt, style = JTheme.type.heading, color = c.label, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(if (f.sessionId == null) "开一段新对话" else "接着原来的对话", style = JTheme.type.secondary, color = c.secondaryText, maxLines = 1)
            parseTime(f.at)?.let { Text("${moment(it)}$DOT_SEP${until(it)}", style = JTheme.type.label, color = c.secondaryText, maxLines = 1) }
            Row(Modifier.padding(top = JSpace.s), horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                PillButton("现在做", onRun, kind = ButtonKind.Tinted, icon = JIcons.run)
                PillButton("取消", onCancel, kind = ButtonKind.Neutral)
            }
        }
    }
}

// --- 运行记录 ---

@Composable
private fun RunsPage(hub: Hub, preload: AssistantPreload?, onBack: () -> Unit, onOpenConversation: (String) -> Unit) {
    var list by remember { mutableStateOf(preload?.runs) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { runCatching { list = hub.api.runs() }.onFailure { error = it.message } }
    KitPage("运行记录", onBack) {
        error?.let { item(key = "error") { ErrorLine(it, Modifier.padding(start = JSpace.page + JSpace.xs, end = JSpace.page, top = JSpace.s)) } }
        val l = list
        when {
            l == null -> if (error == null) item(key = "loading") { PageLoading() }
            l.isEmpty() -> item(key = "empty") {
                EmptyState(JIcons.recent, "还没有运行过", "定时任务和跟进每跑一次（包括失败和跳过的）都记在这里。", Modifier.padding(top = JSpace.xxl))
            }
            // One group per day, newest first (iOS).
            else -> byDay(l).forEachIndexed { i, (day, runs) ->
                item(key = "day:$i") {
                    KitGroup(header = day, separatorStart = RunTextColumn) {
                        runs.forEach { r -> row { RunRow(r, r.sessionId?.takeIf { it.isNotBlank() }?.let { id -> { onOpenConversation(id) } }) } }
                    }
                }
            }
        }
    }
}

private val RunTextColumn: Dp = JSpace.l + JSize.iconS + JSpace.m

/** Runs newest first, cut into days (今天, 昨天, 周三…). */
private fun byDay(runs: List<ApiClient.Run>): List<Pair<String, List<ApiClient.Run>>> {
    val out = ArrayList<Pair<String, MutableList<ApiClient.Run>>>()
    for (r in runs.sortedByDescending { parseTime(it.at) ?: Instant.EPOCH }) {
        val d = parseTime(r.at)?.let { day(it) } ?: "更早"
        if (out.lastOrNull()?.first == d) out.last().second += r else out += d to mutableListOf(r)
    }
    return out
}

/** What a run came to: 已运行, 手动运行, or why it didn't (iOS `AssistantRunText.detail`). */
private fun runDetail(r: ApiClient.Run): String {
    val word = when (r.status) { "ok" -> "已运行"; "skipped" -> "已跳过"; "failed" -> "失败"; else -> r.status }
    val why = r.error?.takeIf { it.isNotBlank() && r.status != "ok" }
    return listOfNotNull(if (r.manual) "手动运行" else word.takeIf { why == null }, why).joinToString(DOT_SEP)
}

/** One run (iOS `AssistantRunRow`): how it went as a symbol, what ran, when and how; it opens its conversation. */
@Composable
private fun RunRow(r: ApiClient.Run, onOpen: (() -> Unit)?) {
    val c = JTheme.colors
    val (icon, tint) = when (r.status) {
        "ok" -> JIcons.success to c.positive
        "skipped" -> JIcons.skipped to c.secondaryText
        else -> JIcons.error to c.danger
    }
    Row(
        Modifier.fillMaxWidth().then(if (onOpen != null) Modifier.clickable(onClick = onOpen) else Modifier)
            .heightIn(min = JSize.box50).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Icon(icon, null, Modifier.size(JSize.iconS), tint = tint)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(r.title.ifBlank { if (r.kind == "followup") "跟进" else "定时任务" }, style = JTheme.type.heading, color = c.label,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(listOfNotNull(parseTime(r.at)?.let(::hm), runDetail(r).ifEmpty { null }).joinToString(DOT_SEP), style = JTheme.type.label,
                color = if (r.status == "failed") c.danger else c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (onOpen != null) Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = c.tertiaryText)
    }
}

// --- 关于我 ---

@Composable
private fun NotesPage(hub: Hub, preload: AssistantPreload?, onBack: () -> Unit) {
    var text by remember { mutableStateOf(preload?.notes) }
    var saved by remember { mutableStateOf(preload?.notes.orEmpty()) }
    var status by remember { mutableStateOf<String?>(null) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) {
        runCatching { hub.api.notes() }.onSuccess { n -> (n.notes ?: n.effective).let { text = it; saved = it } }.onFailure { status = it.message; failed = true }
    }
    fun save() {
        val t = text ?: return
        scope.launch {
            runCatching { hub.api.setNotes(t) }
                .onSuccess { saved = t; status = "已保存，新对话起生效"; failed = false }
                .onFailure { status = "没保存上：${it.message}"; failed = true }
        }
    }
    val c = JTheme.colors
    Column(Modifier.fillMaxSize().background(c.page)) {
        PageHeader("关于我", onBack) { PillButton("保存", ::save, kind = ButtonKind.Prominent, enabled = text != null && text != saved) }
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).imePadding().navigationBarsPadding().padding(top = JSpace.xs, bottom = JSpace.xxl)) {
            JTextField(
                text.orEmpty(), { text = it }, "代码放在哪、常用哪些工具查工作数据、哪些机器或目录别碰、你习惯的汇报方式……",
                Modifier.padding(horizontal = JSpace.page), enabled = text != null, minLines = 14, maxLines = Int.MAX_VALUE, fill = c.card,
            )
            GroupFooter("告诉 Jarvis 你的情况：代码放在哪、用哪些工具查工作数据、哪些目录不能碰……它每次开对话都会读到。")
            status?.let {
                val m = Modifier.padding(start = JSpace.page + JSpace.xs, end = JSpace.page, top = JSpace.m)
                if (failed) ErrorLine(it, m) else SavedLine(it, m)
            }
        }
    }
}

/** Something saved, said in a line: the check in green, the words quiet. */
@Composable
private fun SavedLine(text: String, modifier: Modifier = Modifier) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        Icon(JIcons.success, null, Modifier.padding(top = JSpace.xxs).size(JSize.markS), tint = JTheme.colors.positive)
        Text(text, style = JTheme.type.caption, color = JTheme.colors.secondaryText)
    }
}

// --- 记忆 ---

/** The memory's table of contents, Jarvis' own (Claude Code auto memory). */
private const val MEMORY_INDEX = "MEMORY.md"

@Composable
private fun MemoryPage(hub: Hub, preload: AssistantPreload?, onBack: () -> Unit) {
    var files by remember { mutableStateOf(preload?.memory) }
    var error by remember { mutableStateOf<String?>(null) }
    var editing by remember { mutableStateOf<ApiClient.MemoryFile?>(null) }
    var deleting by remember { mutableStateOf<ApiClient.MemoryFile?>(null) }
    val scope = rememberCoroutineScope()
    suspend fun load() { runCatching { files = hub.api.memory() }.onFailure { error = it.message } }
    LaunchedEffect(Unit) { load() }
    val all = files
    val index = all?.firstOrNull { it.name == MEMORY_INDEX }
    val titles = remember(index?.content) { indexTitles(index?.content.orEmpty()) }
    val entries = all.orEmpty().filter { it.name != MEMORY_INDEX }.sortedByDescending { parseTime(it.updatedAt) ?: Instant.EPOCH }
    KitPage("记忆", onBack) {
        error?.let { item(key = "error") { ErrorLine("读不到记忆：$it", Modifier.padding(start = JSpace.page + JSpace.xs, end = JSpace.page, top = JSpace.s)) } }
        when {
            all == null -> if (error == null) item(key = "loading") { PageLoading() }
            entries.isEmpty() -> item(key = "empty") {
                EmptyState(JIcons.memory, "还没有记忆", "你纠正它、告诉它习惯时，它会自己记下来。", Modifier.padding(top = JSpace.xxl))
            }
            else -> {
                item(key = "files") {
                    KitGroup(footer = "Jarvis 记住的事，一条一个文件，新对话开始时会读。说错的地方可以直接改，或者在对话里告诉它。") {
                        entries.forEach { f -> row { MemoryRow(f, titles[f.name], onOpen = { editing = f }, onDelete = { deleting = f }) } }
                    }
                }
                if (index != null) item(key = "index") {
                    KitGroup(footer = "$MEMORY_INDEX：Jarvis 自己维护的目录，删除记忆时会一起更新。") {
                        row { KitRow("索引", { editing = index }, icon = JIcons.list, iconColor = JTheme.colors.secondaryText) }
                    }
                }
            }
        }
    }
    editing?.let { f ->
        MemoryEditor(f, if (f.name == MEMORY_INDEX) "索引" else memoryTitle(f, titles[f.name]), onDismiss = { editing = null }) { content ->
            editing = null
            scope.launch { runCatching { hub.api.saveMemory(f.name, content) }.onFailure { error = it.message }; load() }
        }
    }
    deleting?.let { f ->
        AlertDialog(
            onDismissRequest = { deleting = null },
            title = { Text("删掉这条记忆？") },
            text = { Text("「${memoryTitle(f, titles[f.name])}」会从 Jarvis 的记忆里去掉，之后的对话不再知道。") },
            confirmButton = {
                TextButton({ deleting = null; scope.launch { runCatching { hub.api.deleteMemory(f.name) }.onFailure { error = it.message }; load() } }) {
                    Text("删除", color = JTheme.colors.danger, maxLines = 1)
                }
            },
            dismissButton = { TextButton({ deleting = null }) { Text("取消", maxLines = 1) } },
        )
    }
}

/**
 * One memory (iOS's row): its title (the index's, else its own name), what kind it is, what it says; the trash can
 * at the end. Tapping it opens the file to edit.
 */
@Composable
private fun MemoryRow(f: ApiClient.MemoryFile, indexed: Pair<String, String?>?, onOpen: () -> Unit, onDelete: () -> Unit) {
    val c = JTheme.colors
    val meta = remember(f.content) { MemoryMeta.of(f.content) }
    val detail = meta.description ?: indexed?.second ?: memoryPreview(f.content).ifBlank { null }
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onOpen).padding(start = JSpace.l, end = JSpace.xs, top = JSpace.m, bottom = JSpace.m),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                Text(memoryTitle(f, indexed, meta), style = JTheme.type.heading, color = c.label, maxLines = 1,
                    overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                meta.typeTitle?.let { Chip(it, look = ChipLook.Tint(memoryTypeColor(meta.type))) }
            }
            detail?.let { Text(it, style = JTheme.type.secondary, color = c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis) }
        }
        IconAction(JIcons.delete, "删除", onDelete, tint = c.tertiaryText, size = JSize.iconXS)
    }
}

/** A memory's title: the index's, else the name in its front matter, else its file's. */
private fun memoryTitle(f: ApiClient.MemoryFile, indexed: Pair<String, String?>?, meta: MemoryMeta = MemoryMeta.of(f.content)): String =
    indexed?.first ?: meta.name ?: f.name.removeSuffix(".md")

/** The kinds of memory told apart by color (iOS `typeColor`). */
@Composable
private fun memoryTypeColor(type: String?): Color = when (type) {
    "user" -> JTheme.hue.purple
    "feedback" -> JTheme.hue.orange
    "project" -> JTheme.hue.green
    "reference" -> JTheme.hue.blue
    else -> JTheme.colors.secondaryText
}

/** A memory file's front matter (`---` … `---`): its name, description and type (iOS `AssistantMemoryEntry`). */
private data class MemoryMeta(val name: String?, val description: String?, val type: String?) {
    val typeTitle: String?
        get() = when (type) {
            "user" -> "用户"
            "feedback" -> "反馈"
            "project" -> "项目"
            "reference" -> "参考"
            else -> type
        }

    companion object {
        fun of(content: String): MemoryMeta {
            val lines = content.lines()
            if (lines.firstOrNull()?.trim() != "---") return MemoryMeta(null, null, null)
            val end = lines.drop(1).indexOfFirst { it.trim() == "---" }
            if (end < 0) return MemoryMeta(null, null, null)
            val values = HashMap<String, String>()
            // `type` sits under `metadata:` (indented); older files have it at the top level.
            for (line in lines.subList(1, end + 1)) {
                val colon = line.indexOf(':').takeIf { it > 0 } ?: continue
                var value = line.substring(colon + 1).trim()
                if (value.length >= 2 && (value[0] == '"' || value[0] == '\'') && value.last() == value[0]) value = value.substring(1, value.length - 1)
                if (value.isNotEmpty()) values[line.substring(0, colon).trim()] = value
            }
            return MemoryMeta(values["name"], values["description"], values["type"])
        }
    }
}

/** The index's lines 「- [标题](file.md) — 说明」 by file: titles friendlier than the files' own names. */
private fun indexTitles(index: String): Map<String, Pair<String, String?>> {
    val out = HashMap<String, Pair<String, String?>>()
    for (line in index.lines()) {
        val open = line.indexOf('[').takeIf { it >= 0 } ?: continue
        val close = line.indexOf("](", open).takeIf { it >= 0 } ?: continue
        val paren = line.indexOf(')', close).takeIf { it >= 0 } ?: continue
        val hook = line.substring(paren + 1).trim().trimStart('—', '–', '-', ':', ' ').trim()
        out[line.substring(close + 2, paren)] = line.substring(open + 1, close) to hook.ifEmpty { null }
    }
    return out
}

@Composable
private fun MemoryEditor(f: ApiClient.MemoryFile, title: String, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    JSheet(onDismiss, title = title, onClose = onDismiss) { MemoryForm(f, onSave) }
}

/** The memory editor's body: the whole file, front matter included, as text; 保存 once it changed. */
@Composable
internal fun MemoryForm(f: ApiClient.MemoryFile, onSave: (String) -> Unit) {
    var content by remember(f.name) { mutableStateOf(f.content) }
    Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).imePadding()) {
        Text(f.name, style = JTheme.type.code, color = JTheme.colors.tertiaryText, maxLines = 1, overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(start = JSpace.page + JSpace.xs, end = JSpace.page, bottom = JSpace.s))
        JTextField(content, { content = it }, "（空）", Modifier.padding(horizontal = JSpace.page), minLines = 10, maxLines = Int.MAX_VALUE, fill = JTheme.colors.card)
        PillButton(
            "保存", { onSave(content) }, Modifier.fillMaxWidth().padding(start = JSpace.page, end = JSpace.page, top = JSpace.xl),
            kind = ButtonKind.Prominent, large = true, enabled = content != f.content,
        )
    }
}

/** The first line of a memory's text, after its front matter (`---` … `---`, name / description / type). */
fun memoryPreview(content: String): String {
    val lines = content.lines()
    val body = if (lines.firstOrNull()?.trim() == "---") {
        val end = lines.drop(1).indexOfFirst { it.trim() == "---" }
        if (end >= 0) lines.drop(end + 2) else lines
    } else lines
    return body.firstOrNull { it.isNotBlank() }?.trim().orEmpty()
}
