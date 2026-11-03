package persona.feature.assistant.proactive

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.contracts.api.Routine
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.ZoneId

/** Survives rotation and process death; null until the person changes something. */
private val DraftSaver = Saver<RoutineDraft?, Any>(
    save = { draft -> draft?.let { arrayListOf<Any>(it.title, it.prompt, it.time, it.weekdays.joinToString(","), it.enabled) } },
    restore = { saved ->
        (saved as? List<*>)?.let { values ->
            RoutineDraft(values[0] as String, values[1] as String, values[2] as String,
                (values[3] as String).split(",").mapNotNull { it.toIntOrNull() }, values[4] as Boolean)
        }
    },
)

private const val HistoryShown = 5

/**
 * One routine: name, on / off, when (a time wheel and the days), what to do, and for an existing one 立即运行, its last
 * result, its history and 删除. Nothing reaches the hub until 保存 / 添加; leaving with changes asks first.
 */
@Composable
internal fun RoutineEditorPage(
    id: String?,
    template: RoutineDraft?,
    state: ProactiveState,
    store: ProactiveStore,
    now: Instant,
    zone: ZoneId,
    onClose: () -> Unit,
    onOpenConversation: (SessionKey) -> Unit,
    onAllRuns: (String) -> Unit,
    /** Starts a run on the screen's scope: it carries on, and opens the conversation, even if this page is left. */
    onRunNow: (String) -> Unit,
) {
    val isNew = id == null
    val found = id?.let { key -> state.shownRoutines?.firstOrNull { it.id == key } }
    var known by remember { mutableStateOf<Routine?>(null) }
    LaunchedEffect(found) { if (found != null) known = found }
    var closing by remember { mutableStateOf(false) }
    // After 删除 the page slides away showing what it showed, not "already gone".
    val routine = found ?: known.takeIf { closing }
    val baseline = routine?.draft() ?: template ?: BlankRoutine
    var edited by rememberSaveable(stateSaver = DraftSaver) { mutableStateOf<RoutineDraft?>(null) }
    // The routine as it was when the person started changing it: only their changes are written, so what Persona
    // changed meanwhile (it can edit routines from a conversation) stays.
    var editedFrom by rememberSaveable(stateSaver = DraftSaver) { mutableStateOf<RoutineDraft?>(null) }
    // A new routine's id is made once: a retry after a lost answer writes the same routine instead of a second one.
    val newId = rememberSaveable { newRoutineId() }
    val draft = edited ?: baseline
    val dirty = edited?.let { !it.sameAs(editedFrom ?: baseline) } ?: false
    var saving by remember { mutableStateOf(false) }
    var askDiscard by remember { mutableStateOf(false) }
    var askDelete by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val running = id != null && state.routineRunning(id)
    val busy = saving || running
    fun edit(next: RoutineDraft) {
        if (edited == null) editedFrom = baseline
        edited = next
    }
    fun forget() { edited = null; editedFrom = null }
    fun close() { closing = true; onClose() }
    fun back() { if (!saving) { if (dirty) askDiscard = true else close() } }
    BackHandler(enabled = dirty || saving) { back() }

    fun save() {
        if (busy || !draft.valid) return
        saving = true
        scope.launch {
            try {
                val change = if (id == null) RoutineChange.Add(newId, draft) else RoutineChange.Edit(id, editedFrom ?: baseline, draft)
                if (store.save(change).isSuccess) { forget(); close() }
            } finally {
                saving = false
            }
        }
    }
    fun runNow() {
        val target = routine ?: return
        if (busy || !draft.valid) return
        if (!dirty) return onRunNow(target.id)
        // Changes go first, so the run uses what is on screen.
        saving = true
        scope.launch {
            try {
                if (store.save(RoutineChange.Edit(target.id, editedFrom ?: baseline, draft)).isSuccess) { forget(); onRunNow(target.id) }
            } finally {
                saving = false
            }
        }
    }

    val title = when {
        isNew -> "新建定时任务"
        else -> routine?.title?.takeIf { it.isNotBlank() } ?: "定时任务"
    }
    val showBar = routine != null || isNew
    JListPage(
        title = title, modifier = Modifier.imePadding(), onBack = ::back,
        bottomBar = if (showBar && (isNew || dirty)) ({
            JBottomBar {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                    if (!draft.valid) Text(
                        if (!validTime(normalizeTime(draft.time))) "时间要写成 HH:MM" else "名称和要做的事都填好才能保存",
                        style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2,
                    )
                    JButton(if (isNew) "添加" else "保存", ::save, Modifier.fillMaxWidth(), enabled = draft.valid && !running,
                        loading = saving, loadingText = "正在保存…")
                }
            }
        }) else null,
    ) {
        if (!isNew && routine == null) {
            when {
                state.routines.value == null && state.routines.error != null -> item("error") {
                    JNotice(state.routines.error, title = "读不到定时任务", status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                        actionLabel = "重试", onAction = { scope.launch { store.loadRoutines() } })
                }
                state.routines.value == null -> item("loading") { JLoading() }
                else -> item("gone") {
                    JEmptyState("这个定时任务已经不在了", "可能刚在对话里被 Persona 删掉了。", icon = PhIcons.Clock,
                        action = { JButton("返回", ::close, kind = JButtonKind.Secondary) })
                }
            }
            return@JListPage
        }
        if (routine?.byAssistant == true) item("by-assistant") {
            JNotice("Persona 在对话里替你建的，可以照常修改、关掉或删除。", icon = PhIcons.Sparkle)
        }
        item("name") {
            JTextField(draft.title, { edit(draft.copy(title = it)) }, Modifier.padding(top = Tokens.Space.s), label = "名称",
                placeholder = "比如「早报」", singleLine = true, enabled = !busy)
        }
        item("enabled") {
            Spacer(Modifier.height(Tokens.Space.m))
            JGroupItem(GroupPosition.Single) {
                JSwitchRow("开启", draft.enabled, { edit(draft.copy(enabled = it)) },
                    subtitle = if (draft.enabled) "到点自动运行" else "关着时不会自动运行，仍然可以立即运行", enabled = !busy)
            }
        }
        item("when-header") { JSectionHeader("什么时候") }
        item("when") {
            JCard {
                TimeWheel(draft.time, { edit(draft.copy(time = it)) })
                Spacer(Modifier.height(Tokens.Space.l))
                WeekdayPicker(draft.weekdays, { edit(draft.copy(weekdays = it)) })
            }
            val next = parseTime(routine?.nextRun)?.takeIf { !dirty && draft.enabled && routine?.enabled == true }
            SectionNote(next?.let { "下次运行：${ScheduleText.moment(it, now, zone)}（${ScheduleText.until(it, now, zone)}）" }
                ?: "${ScheduleText.sentence(draft.time, draft.weekdays)}，按助手设置里的时区。")
        }
        item("prompt-header") { JSectionHeader("要 Persona 做什么") }
        item("prompt") {
            JTextField(draft.prompt, { edit(draft.copy(prompt = it)) }, Modifier.fillMaxWidth(),
                placeholder = "像在对话里一样写：看哪些数据、怎么汇报。", minLines = 6, maxLines = Int.MAX_VALUE, enabled = !busy,
                supportingText = "到点 Persona 开一段新对话照着做，做完推送到手机。它还可以再安排一次性的跟进，比如会前 10 分钟提醒你。")
        }
        if (routine == null) return@JListPage
        val runs = state.runsOf(routine.id).orEmpty()
        val last = state.lastRunOf(routine)
        val lastKey = last?.conversation
        val lastLine = last?.line(now, zone)
        val runRows = 1 + if (lastLine != null) 1 else 0
        item("run-header") { JSectionHeader("运行") }
        item("run-now") {
            JGroupItem(GroupPosition.of(0, runRows)) {
                JRow("立即运行", subtitle = if (dirty) "先保存修改，再开一段新对话" else "开一段新对话，跑完在 Persona 里看结果",
                    leadingIcon = PhIcons.Play, onClick = ::runNow, enabled = !busy && draft.valid, chevron = false,
                    trailing = if (running) ({ JSpinner(Modifier.size(Tokens.Space.xxl)) }) else null)
            }
        }
        if (lastLine != null) item("last") {
            JGroupItem(GroupPosition.of(1, runRows)) {
                JRow(if (lastKey != null) "查看上次结果" else "上次运行", subtitle = lastLine, subtitleLines = 3,
                    leadingIcon = if (last?.failed == true) PhIcons.WarningCircle else PhIcons.ChatTeardropText,
                    onClick = lastKey?.let { key -> { onOpenConversation(key) } })
            }
        }
        if (runs.isNotEmpty()) {
            item("history-header") { JSectionHeader("历史记录", count = runs.size) }
            val shown = runs.take(HistoryShown)
            val more = runs.size > HistoryShown
            itemsIndexed(shown, key = { index, run -> "history-$index-${run.id}" }) { index, run ->
                RunRow(run, parseTime(run.at)?.let { ScheduleText.moment(it, now, zone) } ?: run.displayTitle(), "",
                    GroupPosition(first = index == 0, last = index == shown.lastIndex && !more), run.conversation(state.assistantMachine), onOpenConversation)
            }
            if (more) item("history-more") { MoreRow("全部 ${runs.size} 次", GroupPosition(first = false, last = true)) { onAllRuns(routine.id) } }
        }
        item("delete") {
            Spacer(Modifier.height(Tokens.Space.xxl))
            JGroupItem(GroupPosition.Single) {
                JRow("删除定时任务", leadingIcon = PhIcons.Trash, destructive = true, onClick = { askDelete = true }, enabled = !busy, chevron = false)
            }
        }
    }
    if (askDiscard) JDialog(
        onDismiss = { askDiscard = false }, title = "放弃这次修改？", text = "改动还没保存，离开后就没有了。", destructive = true,
        confirm = "放弃" to { askDiscard = false; forget(); close() }, dismiss = "继续编辑" to { askDiscard = false },
    )
    if (askDelete && routine != null) JDialog(
        onDismiss = { if (!saving) askDelete = false }, title = "删除「${routine.title}」？",
        text = "删除后不再定时运行，已经跑过的对话还在。", destructive = true, confirmEnabled = !saving,
        confirm = "删除" to {
            saving = true
            scope.launch {
                try {
                    if (store.save(RoutineChange.Delete(routine.id)).isSuccess) close()
                } finally {
                    saving = false
                    askDelete = false
                }
            }
        },
        dismiss = "取消" to { askDelete = false },
    )
}
