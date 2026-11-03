package persona.feature.assistant.proactive

import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.ContentTransform
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.Machine
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.contracts.api.AssistantManagement
import persona.core.contracts.api.Followup
import persona.core.contracts.api.Routine
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.feature.assistant.LocalAssistantHost
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.Duration
import java.time.Instant
import java.time.ZoneId

/**
 * 设置 › 助手 › 主动做事: the assistant's routines, the follow-ups it scheduled for itself, and the history of runs
 * (docs/assistant.md). The screen keeps its own sub-pages (a routine, a new one, all follow-ups, all runs) and back.
 *
 * [onOpenConversation] opens an assistant conversation: the one a run started, the one a follow-up continues, or the
 * new one 立即运行 started. [management] and [machines] default to the paired hub's (the Persona tab's host).
 * [initialPage] opens a sub-page directly, for debug captures: `routine:<id>`, `new`, `new:<template>`, `followups`,
 * `runs`, `runs:<routine id>`.
 */
@Composable
fun AssistantProactiveScreen(
    onBack: () -> Unit,
    onOpenConversation: (SessionKey) -> Unit,
    modifier: Modifier = Modifier,
    management: AssistantManagement? = LocalAssistantHost.current?.management,
    machines: List<Machine> = LocalAssistantHost.current?.machines.orEmpty(),
    initialPage: String? = null,
) {
    if (management == null) {
        JListPage(title = "主动做事", modifier = modifier, onBack = onBack) {
            item("unconnected") {
                JEmptyState("还没有连上 hub", "连上 hub 后，可以在这里管理 Persona 的定时任务、跟进和运行记录。", icon = PhIcons.PlugsConnected)
            }
        }
        return
    }
    // Writes and runs the user started go on in the pairing's scope when the page is left halfway.
    val hostScope = LocalAssistantHost.current?.coroutineScope
    val pageScope = rememberCoroutineScope()
    val store = remember(management, hostScope) { ProactiveStore(management.proactive(), hostScope ?: pageScope) }
    ProactiveScreen(store, onBack, onOpenConversation, modifier, machines, initialPage)
}

/** One page of the screen; the key is what survives rotation and process death. */
internal sealed interface ProactivePage {
    val key: String

    data object Overview : ProactivePage { override val key = "overview" }
    data class EditRoutine(val id: String) : ProactivePage { override val key get() = "routine:$id" }
    /** [template] indexes [RoutineTemplates]; anything else is a blank routine. */
    data class AddRoutine(val template: Int) : ProactivePage { override val key get() = "new:$template" }
    data object FollowupList : ProactivePage { override val key = "followups" }
    data class RunList(val ref: String?) : ProactivePage { override val key get() = ref?.let { "runs:$it" } ?: "runs" }

    companion object {
        fun parse(key: String): ProactivePage? = when {
            key == "overview" -> Overview
            key == "followups" -> FollowupList
            key == "runs" -> RunList(null)
            key == "new" -> AddRoutine(-1)
            key.startsWith("routine:") -> key.removePrefix("routine:").takeIf { it.isNotBlank() }?.let(::EditRoutine)
            key.startsWith("new:") -> key.removePrefix("new:").toIntOrNull()?.let(::AddRoutine)
            key.startsWith("runs:") -> RunList(key.removePrefix("runs:").ifBlank { null })
            else -> null
        }
    }
}

internal fun startPages(initial: String?): List<ProactivePage> =
    listOf(ProactivePage.Overview) + listOfNotNull(initial?.let(ProactivePage::parse)?.takeIf { it != ProactivePage.Overview })

private val PagesSaver = Saver<List<ProactivePage>, ArrayList<String>>(
    save = { pages -> ArrayList(pages.map { it.key }) },
    restore = { keys -> keys.mapNotNull(ProactivePage::parse).ifEmpty { listOf(ProactivePage.Overview) } },
)

@Composable
internal fun ProactiveScreen(
    store: ProactiveStore,
    onBack: () -> Unit,
    onOpenConversation: (SessionKey) -> Unit,
    modifier: Modifier = Modifier,
    machines: List<Machine> = emptyList(),
    initialPage: String? = null,
) {
    val state by store.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    var notice by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(store) { store.refresh() }
    LaunchedEffect(store, feedback) { store.messages.collect { message -> if (feedback?.notice(message) != true) notice = message } }
    LaunchedEffect(notice) { if (notice != null) { delay(5_000); notice = null } }
    // Relative times ("25 分钟后") stay true while the page is open.
    val now by produceState(Instant.now()) { while (true) { delay(30_000); value = Instant.now() } }
    val zone = remember { ZoneId.systemDefault() }
    // Once the soonest thing is due, read again: it moves into the history and the routine's next run moves on.
    // Counted from the last read, not the ticking clock, so a tick cannot cancel the wait.
    val nextDue = remember(state.routines.value, state.followups.value, state.readAt) {
        state.readAt?.let { read -> upcoming(state.routines.value.orEmpty(), state.followups.value.orEmpty()).firstOrNull { it.at.isAfter(read) }?.at }
    }
    LaunchedEffect(nextDue) {
        val due = nextDue ?: return@LaunchedEffect
        val wait = Duration.between(Instant.now(), due).toMillis() + 5_000
        if (wait in 1..Duration.ofHours(6).toMillis()) { delay(wait); store.refresh() }
    }

    var pages by rememberSaveable(stateSaver = PagesSaver) { mutableStateOf(startPages(initialPage)) }
    val holder = rememberSaveableStateHolder()
    fun push(page: ProactivePage) { if (pages.last() != page) pages = pages + page }
    fun pop() {
        if (pages.size <= 1) return onBack()
        holder.removeState(pages.last().key)
        pages = pages.dropLast(1)
    }
    BackHandler(enabled = pages.size > 1) { pop() }

    val runRoutine: (String) -> Unit = { id -> store.launch { runRoutine(id).onSuccess(onOpenConversation) } }
    var templates by remember { mutableStateOf(false) }
    var routineMenu by remember { mutableStateOf<Routine?>(null) }
    var followupMenu by remember { mutableStateOf<Followup?>(null) }
    var deleting by remember { mutableStateOf<Routine?>(null) }
    var cancelling by remember { mutableStateOf<Followup?>(null) }
    val actions = ProactiveActions(
        refresh = { scope.launch { store.refresh() } },
        newRoutine = { templates = true },
        openRoutine = { push(ProactivePage.EditRoutine(it)) },
        toggle = store::toggle,
        routineMenu = { routineMenu = it },
        followupMenu = { followupMenu = it },
        openConversation = onOpenConversation,
        allFollowups = { push(ProactivePage.FollowupList) },
        allRuns = { push(ProactivePage.RunList(it)) },
    )

    val reduceMotion = PersonaTheme.reduceMotion
    Box(modifier.fillMaxSize()) {
        // The same slide as the app's own pages: in from the right over the page below, back out to the right.
        AnimatedContent(
            pages, Modifier.fillMaxSize(), label = "proactive-pages", contentKey = { it.last().key },
            transitionSpec = {
                when {
                    reduceMotion -> ContentTransform(EnterTransition.None, ExitTransition.None, sizeTransform = null)
                    targetState.size >= initialState.size -> ContentTransform(
                        slideInHorizontally(tween(300, easing = FastOutSlowInEasing)) { width -> width },
                        slideOutHorizontally(tween(300, easing = FastOutSlowInEasing)) { width -> -width / 4 }, sizeTransform = null)
                    else -> ContentTransform(
                        slideInHorizontally(tween(260, easing = FastOutSlowInEasing)) { width -> -width / 4 },
                        slideOutHorizontally(tween(260, easing = FastOutSlowInEasing)) { width -> width },
                        targetContentZIndex = -1f, sizeTransform = null)
                }
            },
        ) { stack ->
            val page = stack.last()
            holder.SaveableStateProvider(page.key) {
                when (page) {
                    ProactivePage.Overview -> OverviewPage(state, machines, now, zone, ::pop, actions)
                    is ProactivePage.EditRoutine -> RoutineEditorPage(page.id, null, state, store, now, zone, ::pop, onOpenConversation,
                        onAllRuns = { push(ProactivePage.RunList(it)) }, onRunNow = runRoutine)
                    is ProactivePage.AddRoutine -> RoutineEditorPage(null, RoutineTemplates.getOrNull(page.template)?.draft ?: BlankRoutine,
                        state, store, now, zone, ::pop, onOpenConversation, onAllRuns = { push(ProactivePage.RunList(it)) }, onRunNow = runRoutine)
                    ProactivePage.FollowupList -> FollowupsPage(state, machines, now, zone, ::pop, actions)
                    is ProactivePage.RunList -> RunsPage(state, page.ref, now, zone, ::pop, actions)
                }
            }
        }
        notice?.let { message ->
            JNotice(message, Modifier.align(Alignment.BottomCenter).navigationBarsPadding().padding(horizontal = pagePadding, vertical = Tokens.Space.l),
                status = Status.NeedsYou, icon = PhIcons.WarningCircle, actionLabel = "知道了", onAction = { notice = null })
        }
    }

    if (templates) JActionSheet(
        title = "新建定时任务", note = "从模板开始，保存前都可以改。", onDismiss = { templates = false },
        actions = RoutineTemplates.mapIndexed { index, template -> JAction(template.name, template.icon) { push(ProactivePage.AddRoutine(index)) } } +
            JAction("空白", PhIcons.NotePencil) { push(ProactivePage.AddRoutine(-1)) },
    )
    routineMenu?.let { routine ->
        JActionSheet(
            title = routine.title, note = ScheduleText.sentence(routine.time, routine.weekdays), onDismiss = { routineMenu = null },
            actions = listOf(
                JAction("立即运行", PhIcons.Play, enabled = !state.routineBusy(routine.id)) { runRoutine(routine.id) },
                JAction("编辑", PhIcons.PencilSimple) { push(ProactivePage.EditRoutine(routine.id)) },
                JAction("删除", PhIcons.Trash, destructive = true) { deleting = routine },
            ),
        )
    }
    followupMenu?.let { followup ->
        JActionSheet(
            title = followup.displayTitle(), onDismiss = { followupMenu = null },
            note = parseTime(followup.at)?.let { "到点：${ScheduleText.moment(it, now, zone)}，${ScheduleText.until(it, now, zone)}" },
            actions = listOfNotNull(
                JAction("立即运行", PhIcons.Play, enabled = !state.followupBusy(followup.id)) {
                    store.launch { runFollowup(followup.id).onSuccess(onOpenConversation) }
                },
                followup.conversation()?.let { key -> JAction("打开原对话", PhIcons.ChatCircle) { onOpenConversation(key) } },
                JAction("取消这个跟进", PhIcons.XCircle, destructive = true, enabled = !state.followupBusy(followup.id)) { cancelling = followup },
            ),
        )
    }
    deleting?.let { routine ->
        JDialog(
            onDismiss = { deleting = null }, title = "删除「${routine.title}」？", text = "删除后不再定时运行，已经跑过的对话还在。",
            destructive = true, confirm = "删除" to { deleting = null; store.launch { save(RoutineChange.Delete(routine.id)) } },
            dismiss = "取消" to { deleting = null },
        )
    }
    cancelling?.let { followup ->
        JDialog(
            onDismiss = { cancelling = null }, title = "取消这个跟进？", text = "「${followup.displayTitle()}」到点就不会再做了。",
            destructive = true, confirm = "取消跟进" to { cancelling = null; store.launch { cancelFollowup(followup.id) } },
            dismiss = "保留" to { cancelling = null },
        )
    }
}
