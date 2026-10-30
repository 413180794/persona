package jarvis.android.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.parseTime
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * The assistant's profile (iOS AssistantProfileView): what's coming up, its routines and follow-ups with their run
 * history, what it knows about the user (「关于我」, edited here) and remembers (memory files), and what it can reach.
 */
enum class AssistantPage { Profile, Routines, Followups, Runs, Notes, Memory }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AssistantProfile(hub: Hub, machineName: String, onBack: () -> Unit, onOpenConversation: (String) -> Unit) {
    var page by remember { mutableStateOf(AssistantPage.Profile) }
    val title = when (page) {
        AssistantPage.Profile -> "Jarvis"
        AssistantPage.Routines -> "定时任务"
        AssistantPage.Followups -> "跟进"
        AssistantPage.Runs -> "运行记录"
        AssistantPage.Notes -> "关于我"
        AssistantPage.Memory -> "记忆"
    }
    androidx.activity.compose.BackHandler { if (page == AssistantPage.Profile) onBack() else page = AssistantPage.Profile }
    var addRoutine by remember { mutableStateOf(false) }
    Scaffold(topBar = {
        TopAppBar(
            title = { Text(title) },
            navigationIcon = {
                IconButton({ if (page == AssistantPage.Profile) onBack() else page = AssistantPage.Profile }) {
                    Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回")
                }
            },
            actions = { if (page == AssistantPage.Routines) IconButton({ addRoutine = true }) { Icon(Icons.Default.Add, "新建定时任务") } },
        )
    }) { pad ->
        val m = Modifier.padding(pad).fillMaxSize()
        when (page) {
            AssistantPage.Profile -> ProfileHome(hub, machineName, m) { page = it }
            AssistantPage.Routines -> RoutinesPage(hub, m, addRoutine, onAddDone = { addRoutine = false }, onOpenConversation)
            AssistantPage.Followups -> FollowupsPage(hub, m, onOpenConversation)
            AssistantPage.Runs -> RunsPage(hub, m, onOpenConversation)
            AssistantPage.Notes -> NotesPage(hub, m)
            AssistantPage.Memory -> MemoryPage(hub, m)
        }
    }
}

@Composable
private fun ProfileHome(hub: Hub, machineName: String, modifier: Modifier, go: (AssistantPage) -> Unit) {
    var routines by remember { mutableStateOf<List<ApiClient.Routine>>(emptyList()) }
    var followups by remember { mutableStateOf<List<ApiClient.Followup>>(emptyList()) }
    LaunchedEffect(Unit) {
        runCatching { routines = hub.api.routines() }
        runCatching { followups = hub.api.followups() }
    }
    // Next three things it will do on its own: enabled routines' next runs and pending follow-ups.
    val upcoming = (routines.filter { it.enabled }.mapNotNull { r -> parseTime(r.nextRun)?.let { it to r.title } } +
        followups.mapNotNull { f -> parseTime(f.at)?.let { it to (f.title?.takeIf { t -> t.isNotBlank() } ?: f.prompt.take(30)) } })
        .sortedBy { it.first }.take(3)
    Column(modifier.verticalScroll(rememberScrollState())) {
        Text("你的私人助手 · 运行在 $machineName", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
        Header("接下来")
        if (upcoming.isEmpty()) Text("没有安排", Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
        upcoming.forEach { (at, t) -> ListItem(headlineContent = { Text(t, maxLines = 1, overflow = TextOverflow.Ellipsis) }, supportingContent = { Text(whenText(at)) }) }
        Header("主动做事")
        Text(
            "到点自己开始工作、跟进答应你的事，跑完推送到手机。在对话里说「每天 9 点给我早报」「一小时后提醒我」就会加到这里。",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp),
        )
        Nav("定时任务", "${routines.count { it.enabled }} 个开着") { go(AssistantPage.Routines) }
        Nav("跟进", if (followups.isEmpty()) "没有" else "${followups.size} 个") { go(AssistantPage.Followups) }
        Nav("运行记录", null) { go(AssistantPage.Runs) }
        Header("了解你")
        Text(
            "「关于我」是你告诉 Jarvis 的；「记忆」是它在对话里自己记下的，都可以改。",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp),
        )
        Nav("关于我", null) { go(AssistantPage.Notes) }
        Nav("记忆", null) { go(AssistantPage.Memory) }
        Header("能访问")
        listOf(
            "各台电脑上的 Agent：看状态、读对话、发消息、开新的",
            "飞书：消息、日历、文档、会议纪要、任务、邮件",
            "你添加的所有邮箱",
            "在手机上问你、给你推送",
        ).forEach { Text("· $it", Modifier.padding(horizontal = 16.dp, vertical = 2.dp)) }
        Text(
            "发飞书消息、发邮件、新建日程这类会替你出面的操作，都先在手机上问你。",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp),
        )
    }
}

@Composable
private fun Header(t: String) =
    Text(t, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(start = 16.dp, top = 20.dp, bottom = 4.dp))

@Composable
private fun Nav(t: String, detail: String?, onClick: () -> Unit) =
    ListItem(headlineContent = { Text(t) }, trailingContent = detail?.let { { Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant) } }, modifier = Modifier.clickable(onClick = onClick))

private val zone: ZoneId get() = ZoneId.systemDefault()
private fun whenText(at: Instant): String {
    val local = at.atZone(zone)
    val today = java.time.LocalDate.now(zone)
    val day = when (local.toLocalDate()) {
        today -> "今天"
        today.plusDays(1) -> "明天"
        else -> local.format(DateTimeFormatter.ofPattern("M月d日"))
    }
    return day + " " + local.format(DateTimeFormatter.ofPattern("HH:mm"))
}

private val WEEKDAYS = listOf("一", "二", "三", "四", "五", "六", "日")
private fun cadence(r: ApiClient.Routine): String {
    val days = r.weekdays.sorted()
    val d = when {
        days.isEmpty() || days.size == 7 -> "每天"
        days == listOf(1, 2, 3, 4, 5) -> "工作日"
        days == listOf(6, 7) -> "周末"
        else -> "每周" + days.joinToString("、") { WEEKDAYS[it - 1] }
    }
    return "$d ${r.time}"
}

@Composable
private fun RoutinesPage(hub: Hub, modifier: Modifier, adding: Boolean, onAddDone: () -> Unit, onOpenConversation: (String) -> Unit) {
    var list by remember { mutableStateOf<List<ApiClient.Routine>?>(null) }
    var editing by remember { mutableStateOf<ApiClient.Routine?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { runCatching { list = hub.api.routines() }.onFailure { error = it.message } }
    fun save(new: List<ApiClient.Routine>) {
        val before = list
        list = new
        scope.launch {
            runCatching { list = hub.api.setRoutines(new) }.onFailure {
                android.util.Log.w(jarvis.android.data.Hub.TAG, "save routines", it)
                list = before; error = "没保存上：${it.message ?: it.javaClass.simpleName}"
            }
        }
    }
    LazyColumn(modifier) {
        error?.let { item { Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(16.dp)) } }
        if (list?.isEmpty() == true) item { Text("还没有定时任务。右上角 + 新建，或者在对话里说「每天 9 点给我早报」。", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        items(list.orEmpty(), key = { it.id }) { r ->
            ListItem(
                headlineContent = { Text(r.title + if (r.createdBy == "assistant") " · Jarvis 建的" else "", maxLines = 1, overflow = TextOverflow.Ellipsis) },
                supportingContent = {
                    Text(
                        listOfNotNull(
                            cadence(r),
                            parseTime(r.nextRun)?.takeIf { r.enabled }?.let { "下次 " + whenText(it) },
                            when (r.lastStatus) { "failed" -> "上次没跑起来"; "skipped" -> "上次跳过了"; else -> null },
                        ).joinToString(" · "),
                    )
                },
                trailingContent = { Switch(r.enabled, { on -> save(list!!.map { if (it.id == r.id) it.copy(enabled = on) else it }) }) },
                modifier = Modifier.clickable { editing = r },
            )
            HorizontalDivider()
        }
    }
    val shown = editing ?: if (adding) ApiClient.Routine(time = "09:00", weekdays = listOf(1, 2, 3, 4, 5)) else null
    shown?.let { r ->
        RoutineEditor(
            r, isNew = r.id.isEmpty(),
            onDismiss = { editing = null; onAddDone() },
            onSave = { updated ->
                editing = null; onAddDone()
                save(if (r.id.isEmpty()) list.orEmpty() + updated else list.orEmpty().map { if (it.id == r.id) updated else it })
            },
            onDelete = { editing = null; save(list.orEmpty().filterNot { it.id == r.id }) },
            onRunNow = {
                editing = null
                scope.launch {
                    runCatching { hub.api.runRoutine(r.id) }.onSuccess { onOpenConversation(it.sessionId) }.onFailure { error = "没跑起来：${it.message}" }
                }
            },
        )
    }
}

@Composable
private fun RoutineEditor(
    r: ApiClient.Routine, isNew: Boolean, onDismiss: () -> Unit, onSave: (ApiClient.Routine) -> Unit, onDelete: () -> Unit, onRunNow: () -> Unit,
) {
    var title by remember { mutableStateOf(r.title) }
    var prompt by remember { mutableStateOf(r.prompt) }
    var time by remember { mutableStateOf(r.time) }
    var days by remember { mutableStateOf(r.weekdays.ifEmpty { (1..7).toList() }.toSet()) }
    val timeOk = Regex("^([01]\\d|2[0-3]):[0-5]\\d$").matches(time)
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (isNew) "新建定时任务" else "定时任务") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.verticalScroll(rememberScrollState())) {
                OutlinedTextField(title, { title = it }, label = { Text("名称") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(prompt, { prompt = it }, label = { Text("到点让 Jarvis 做什么") }, modifier = Modifier.fillMaxWidth().heightIn(min = 96.dp))
                OutlinedTextField(time, { time = it.take(5) }, label = { Text("时间（HH:MM）") }, singleLine = true, isError = !timeOk, modifier = Modifier.fillMaxWidth())
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    (1..7).forEach { d ->
                        FilterChip(d in days, { days = if (d in days) days - d else days + d }, label = { Text(WEEKDAYS[d - 1], maxLines = 1) })
                    }
                }
                if (!isNew) Row {
                    TextButton(onRunNow) { Text("立即运行", maxLines = 1) }
                    TextButton(onDelete) { Text("删除", color = MaterialTheme.colorScheme.error, maxLines = 1) }
                }
            }
        },
        confirmButton = {
            TextButton(
                { onSave(r.copy(title = title.trim(), prompt = prompt.trim(), time = time, weekdays = if (days.size == 7) emptyList() else days.sorted())) },
                enabled = title.isNotBlank() && prompt.isNotBlank() && timeOk && days.isNotEmpty(),
            ) { Text("保存", maxLines = 1) }
        },
        dismissButton = { TextButton(onDismiss) { Text("取消", maxLines = 1) } },
    )
}

@Composable
private fun FollowupsPage(hub: Hub, modifier: Modifier, onOpenConversation: (String) -> Unit) {
    var list by remember { mutableStateOf<List<ApiClient.Followup>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    suspend fun load() { runCatching { list = hub.api.followups() }.onFailure { error = it.message } }
    LaunchedEffect(Unit) { load() }
    LazyColumn(modifier) {
        error?.let { item { Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(16.dp)) } }
        if (list?.isEmpty() == true) item { Text("没有跟进。对话里说「一小时后提醒我…」「明天上午再看看…」时会出现在这里。", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        items(list.orEmpty(), key = { it.id }) { f ->
            ListItem(
                headlineContent = { Text(f.title?.takeIf { it.isNotBlank() } ?: f.prompt, maxLines = 2, overflow = TextOverflow.Ellipsis) },
                supportingContent = { parseTime(f.at)?.let { Text(whenText(it)) } },
                trailingContent = {
                    Row {
                        TextButton({ scope.launch { runCatching { hub.api.runFollowup(f.id) }.onSuccess { onOpenConversation(it.sessionId) }.onFailure { error = it.message } } }) { Text("现在做", maxLines = 1) }
                        TextButton({ scope.launch { runCatching { hub.api.cancelFollowup(f.id) }.onFailure { error = it.message }; load() } }) { Text("取消", maxLines = 1) }
                    }
                },
            )
            HorizontalDivider()
        }
    }
}

@Composable
private fun RunsPage(hub: Hub, modifier: Modifier, onOpenConversation: (String) -> Unit) {
    var list by remember { mutableStateOf<List<ApiClient.Run>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { runCatching { list = hub.api.runs() }.onFailure { error = it.message } }
    LazyColumn(modifier) {
        error?.let { item { Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(16.dp)) } }
        if (list?.isEmpty() == true) item { Text("定时任务和跟进每跑一次（包括失败和跳过的）都记在这里。", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        items(list.orEmpty(), key = { it.id }) { r ->
            val status = when (r.status) { "ok" -> "跑了"; "failed" -> "没跑起来"; "skipped" -> "跳过了"; else -> r.status }
            ListItem(
                headlineContent = { Text(r.title.ifBlank { if (r.kind == "followup") "跟进" else "定时任务" }, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                supportingContent = {
                    Text(listOfNotNull(parseTime(r.at)?.let { whenText(it) }, status, "手动".takeIf { r.manual }, r.error?.takeIf { it.isNotBlank() }).joinToString(" · "), maxLines = 2)
                },
                modifier = if (!r.sessionId.isNullOrBlank()) Modifier.clickable { onOpenConversation(r.sessionId) } else Modifier,
            )
            HorizontalDivider()
        }
    }
}

@Composable
private fun NotesPage(hub: Hub, modifier: Modifier) {
    var text by remember { mutableStateOf<String?>(null) }
    var saved by remember { mutableStateOf("") }
    var status by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) {
        runCatching { hub.api.notes() }.onSuccess { n -> (n.notes ?: n.effective).let { text = it; saved = it } }.onFailure { status = it.message }
    }
    Column(modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("告诉 Jarvis 你的情况：代码放在哪、用哪些工具查工作数据、哪些目录不能碰……它每次开对话都会读到。", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        OutlinedTextField(text.orEmpty(), { text = it }, enabled = text != null, modifier = Modifier.fillMaxWidth().weight(1f))
        Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
            status?.let { Text(it, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f)) } ?: androidx.compose.foundation.layout.Spacer(Modifier.weight(1f))
            TextButton(
                { val t = text ?: return@TextButton; scope.launch { runCatching { hub.api.setNotes(t) }.onSuccess { saved = t; status = "已保存，新对话起生效" }.onFailure { status = "没保存上：${it.message}" } } },
                enabled = text != null && text != saved,
            ) { Text("保存", maxLines = 1) }
        }
    }
}

@Composable
private fun MemoryPage(hub: Hub, modifier: Modifier) {
    var files by remember { mutableStateOf<List<ApiClient.MemoryFile>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var editing by remember { mutableStateOf<ApiClient.MemoryFile?>(null) }
    var deleting by remember { mutableStateOf<ApiClient.MemoryFile?>(null) }
    val scope = rememberCoroutineScope()
    suspend fun load() { runCatching { files = hub.api.memory() }.onFailure { error = it.message } }
    LaunchedEffect(Unit) { load() }
    LazyColumn(modifier) {
        error?.let { item { Text("读不到记忆：$it", color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(16.dp)) } }
        if (files?.isEmpty() == true) item { Text("还没有记忆。你纠正它、告诉它习惯时，它会自己记下来。", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        items(files.orEmpty(), key = { it.name }) { f ->
            ListItem(
                headlineContent = { Text(f.name.removeSuffix(".md")) },
                supportingContent = { Text(memoryPreview(f.content), maxLines = 2, overflow = TextOverflow.Ellipsis) },
                trailingContent = { TextButton({ deleting = f }) { Text("删除", color = MaterialTheme.colorScheme.error, maxLines = 1) } },
                modifier = Modifier.clickable { editing = f },
            )
            HorizontalDivider()
        }
    }
    editing?.let { f ->
        var content by remember(f.name) { mutableStateOf(f.content) }
        AlertDialog(
            onDismissRequest = { editing = null },
            title = { Text(f.name.removeSuffix(".md")) },
            text = { OutlinedTextField(content, { content = it }, modifier = Modifier.fillMaxWidth().heightIn(min = 200.dp)) },
            confirmButton = {
                TextButton({ editing = null; scope.launch { runCatching { hub.api.saveMemory(f.name, content) }.onFailure { error = it.message }; load() } }, enabled = content != f.content) { Text("保存", maxLines = 1) }
            },
            dismissButton = { TextButton({ editing = null }) { Text("取消", maxLines = 1) } },
        )
    }
    deleting?.let { f ->
        AlertDialog(
            onDismissRequest = { deleting = null },
            title = { Text("删掉这条记忆？") },
            text = { Text("「${f.name.removeSuffix(".md")}」会从 Jarvis 的记忆里去掉，之后的对话不再知道。") },
            confirmButton = { TextButton({ deleting = null; scope.launch { runCatching { hub.api.deleteMemory(f.name) }.onFailure { error = it.message }; load() } }) { Text("删除", color = MaterialTheme.colorScheme.error, maxLines = 1) } },
            dismissButton = { TextButton({ deleting = null }) { Text("取消", maxLines = 1) } },
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
