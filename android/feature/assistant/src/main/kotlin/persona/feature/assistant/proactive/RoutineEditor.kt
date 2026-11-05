package persona.feature.assistant.proactive

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.contracts.api.Routine
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.feature.assistant.R
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

    val say = rememberProactiveText()
    val title = when {
        isNew -> stringResource(R.string.assistant_proactive_new_routine)
        else -> routine?.title?.takeIf { it.isNotBlank() } ?: stringResource(R.string.assistant_proactive_routine)
    }
    val showBar = routine != null || isNew
    JListPage(
        title = title, modifier = Modifier.imePadding(), onBack = ::back,
        bottomBar = if (showBar && (isNew || dirty)) ({
            JBottomBar {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                    if (!draft.valid) Text(
                        if (!validTime(normalizeTime(draft.time))) stringResource(R.string.assistant_proactive_time_format) else stringResource(R.string.assistant_proactive_fill_in),
                        style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2,
                    )
                    JButton(if (isNew) stringResource(R.string.assistant_proactive_add) else stringResource(R.string.assistant_proactive_save), ::save, Modifier.fillMaxWidth(), enabled = draft.valid && !running,
                        loading = saving, loadingText = stringResource(R.string.assistant_proactive_saving))
                }
            }
        }) else null,
    ) {
        if (!isNew && routine == null) {
            when {
                state.routines.value == null && state.routines.error != null -> item("error") {
                    JNotice(state.routines.error, title = stringResource(R.string.assistant_proactive_routine_read_failed), status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                        actionLabel = stringResource(R.string.assistant_proactive_retry), onAction = { scope.launch { store.loadRoutines() } })
                }
                state.routines.value == null -> item("loading") { JLoading() }
                else -> item("gone") {
                    JEmptyState(stringResource(R.string.assistant_proactive_error_no_routine), stringResource(R.string.assistant_proactive_gone_body), icon = PhIcons.Clock,
                        action = { JButton(stringResource(R.string.assistant_proactive_back), ::close, kind = JButtonKind.Secondary) })
                }
            }
            return@JListPage
        }
        if (routine?.byAssistant == true) item("by-assistant") {
            JNotice(stringResource(R.string.assistant_proactive_by_assistant_notice), icon = PhIcons.Sparkle)
        }
        item("name") {
            JTextField(draft.title, { edit(draft.copy(title = it)) }, Modifier.padding(top = Tokens.Space.s), label = stringResource(R.string.assistant_proactive_name),
                placeholder = stringResource(R.string.assistant_proactive_name_placeholder), singleLine = true, enabled = !busy)
        }
        item("enabled") {
            Spacer(Modifier.height(Tokens.Space.m))
            JGroupItem(GroupPosition.Single) {
                JSwitchRow(stringResource(R.string.assistant_proactive_enabled), draft.enabled, { edit(draft.copy(enabled = it)) },
                    subtitle = if (draft.enabled) stringResource(R.string.assistant_proactive_enabled_on) else stringResource(R.string.assistant_proactive_enabled_off), enabled = !busy)
            }
        }
        item("when-header") { JSectionHeader(stringResource(R.string.assistant_proactive_when)) }
        item("when") {
            JCard {
                TimeWheel(draft.time, { edit(draft.copy(time = it)) })
                Spacer(Modifier.height(Tokens.Space.l))
                WeekdayPicker(draft.weekdays, { edit(draft.copy(weekdays = it)) })
            }
            val next = parseTime(routine?.nextRun)?.takeIf { !dirty && draft.enabled && routine?.enabled == true }
            SectionNote(next?.let { stringResource(R.string.assistant_proactive_next_run, say.moment(it, now, zone), say.until(it, now, zone)) }
                ?: stringResource(R.string.assistant_proactive_rule_note, say.sentence(draft.time, draft.weekdays)))
        }
        item("prompt-header") { JSectionHeader(stringResource(R.string.assistant_proactive_what)) }
        item("prompt") {
            JTextField(draft.prompt, { edit(draft.copy(prompt = it)) }, Modifier.fillMaxWidth(),
                placeholder = stringResource(R.string.assistant_proactive_prompt_placeholder), minLines = 6, maxLines = Int.MAX_VALUE, enabled = !busy,
                supportingText = stringResource(R.string.assistant_proactive_prompt_note))
        }
        if (routine == null) return@JListPage
        val runs = state.runsOf(routine.id).orEmpty()
        val last = state.lastRunOf(routine)
        val lastKey = last?.conversation
        val lastLine = last?.line(say, now, zone)
        val runRows = 1 + if (lastLine != null) 1 else 0
        item("run-header") { JSectionHeader(stringResource(R.string.assistant_proactive_run)) }
        item("run-now") {
            JGroupItem(GroupPosition.of(0, runRows)) {
                JRow(stringResource(R.string.assistant_proactive_run_now), subtitle = if (dirty) stringResource(R.string.assistant_proactive_run_now_dirty) else stringResource(R.string.assistant_proactive_run_now_body),
                    leadingIcon = PhIcons.Play, onClick = ::runNow, enabled = !busy && draft.valid, chevron = false,
                    trailing = if (running) ({ JSpinner(Modifier.size(Tokens.Space.xxl)) }) else null)
            }
        }
        if (lastLine != null) item("last") {
            JGroupItem(GroupPosition.of(1, runRows)) {
                JRow(if (lastKey != null) stringResource(R.string.assistant_proactive_last_result) else stringResource(R.string.assistant_proactive_last_run_title), subtitle = lastLine, subtitleLines = 3,
                    leadingIcon = if (last?.failed == true) PhIcons.WarningCircle else PhIcons.ChatTeardropText,
                    onClick = lastKey?.let { key -> { onOpenConversation(key) } })
            }
        }
        if (runs.isNotEmpty()) {
            item("history-header") { JSectionHeader(stringResource(R.string.assistant_proactive_history), count = runs.size) }
            val shown = runs.take(HistoryShown)
            val more = runs.size > HistoryShown
            itemsIndexed(shown, key = { index, run -> "history-$index-${run.id}" }) { index, run ->
                RunRow(run, parseTime(run.at)?.let { say.moment(it, now, zone) } ?: run.displayTitle(say), "",
                    GroupPosition(first = index == 0, last = index == shown.lastIndex && !more), run.conversation(state.assistantMachine), say, onOpenConversation)
            }
            if (more) item("history-more") { MoreRow(pluralStringResource(R.plurals.assistant_proactive_all_times, runs.size, runs.size), GroupPosition(first = false, last = true)) { onAllRuns(routine.id) } }
        }
        item("delete") {
            Spacer(Modifier.height(Tokens.Space.xxl))
            JGroupItem(GroupPosition.Single) {
                JRow(stringResource(R.string.assistant_proactive_delete_routine), leadingIcon = PhIcons.Trash, destructive = true, onClick = { askDelete = true }, enabled = !busy, chevron = false)
            }
        }
    }
    if (askDiscard) JDialog(
        onDismiss = { askDiscard = false }, title = stringResource(R.string.assistant_proactive_discard_title), text = stringResource(R.string.assistant_proactive_discard_body),
        destructive = true, confirm = stringResource(R.string.assistant_proactive_discard) to { askDiscard = false; forget(); close() },
        dismiss = stringResource(R.string.assistant_proactive_keep_editing) to { askDiscard = false },
    )
    if (askDelete && routine != null) JDialog(
        onDismiss = { if (!saving) askDelete = false }, title = stringResource(R.string.assistant_proactive_delete_title, routine.title),
        text = stringResource(R.string.assistant_proactive_delete_body), destructive = true, confirmEnabled = !saving,
        confirm = stringResource(R.string.assistant_proactive_delete) to {
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
        dismiss = stringResource(R.string.assistant_proactive_cancel) to { askDelete = false },
    )
}
