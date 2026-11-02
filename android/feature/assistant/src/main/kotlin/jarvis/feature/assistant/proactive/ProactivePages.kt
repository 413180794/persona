package jarvis.feature.assistant.proactive

import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import jarvis.core.contracts.Machine
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.parseTime
import jarvis.core.contracts.api.Followup
import jarvis.core.contracts.api.Routine
import jarvis.core.contracts.api.Run
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import java.time.Instant
import java.time.ZoneId

/** What the pages can ask for; the screen turns these into hub calls, navigation and confirmations. */
internal class ProactiveActions(
    val refresh: () -> Unit,
    val newRoutine: () -> Unit,
    val openRoutine: (String) -> Unit,
    val toggle: (String, Boolean) -> Unit,
    val routineMenu: (Routine) -> Unit,
    val followupMenu: (Followup) -> Unit,
    val openConversation: (SessionKey) -> Unit,
    val allFollowups: () -> Unit,
    val allRuns: (String?) -> Unit,
)

/** How many follow-ups and runs the overview shows before "全部 …". */
private const val FollowupsShown = 3
private const val RunsShown = 3

/**
 * 主动做事: what runs next, the routines with their switches, the follow-ups Jarvis scheduled, and the latest runs
 * (iOS AssistantRoutinesView). Each part opens a page of its own.
 */
@Composable
internal fun OverviewPage(
    state: ProactiveState, machines: List<Machine>, now: Instant, zone: ZoneId, onBack: () -> Unit, actions: ProactiveActions,
) {
    val routines = state.shownRoutines
    val followups = state.followupList
    val runs = state.runList
    val timeline = remember(routines, followups) { upcoming(routines.orEmpty(), followups.orEmpty()) }
    val coming = timeline.filter { it.at.isAfter(now.minusSeconds(60)) }.take(4)
    val readErrors = listOfNotNull(state.routines.error, state.followups.error).distinct()
    JListPage(
        title = "主动做事", summary = summaryLine(routines, followups, now, zone), onBack = onBack, largeTitle = true,
        actions = { JIconButton(PhIcons.Plus, "新建定时任务", actions.newRoutine) },
        refreshing = state.refreshing && state.loaded, onRefresh = actions.refresh,
    ) {
        if (!state.loaded) {
            if (readErrors.isEmpty()) item("loading") { JLoading(text = "正在读取安排…") }
            else item("read-error") {
                JNotice(readErrors.first(), title = "读不到安排", status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                    actionLabel = "重试", onAction = actions.refresh)
            }
            return@JListPage
        }
        val nothing = routines?.isEmpty() == true && followups?.isEmpty() == true
        if (nothing) item("empty") {
            JEmptyState(
                "还没有安排", "点右上角的 ＋ 新建定时任务，或者在对话里对 Jarvis 说「每天 9 点给我早报」「一小时后提醒我」。",
                icon = PhIcons.CalendarBlank,
                action = { JButton("新建定时任务", actions.newRoutine, icon = PhIcons.Plus) },
            )
        }
        if (coming.isNotEmpty()) {
            item("upcoming-header") { JSectionHeader("接下来") }
            itemsIndexed(coming, key = { _, item -> "upcoming-${item.id}" }) { index, item ->
                val followup = followups?.firstOrNull { "f-${it.id}" == item.id }
                UpcomingRow(item, now, zone, GroupPosition.of(index, coming.size), onClick = when {
                    item.routine -> ({ actions.openRoutine(item.id.removePrefix("r-")) })
                    item.conversation != null -> ({ actions.openConversation(item.conversation) })
                    followup != null -> ({ actions.followupMenu(followup) })
                    else -> null
                })
            }
        }
        if (!nothing) {
            item("routines-header") { JSectionHeader("定时任务", count = routines?.size?.takeIf { it > 0 }) }
            state.routines.error?.let { error ->
                item("routines-error") { JNotice(error, status = Status.NeedsYou, icon = PhIcons.WarningCircle, actionLabel = "重试", onAction = actions.refresh) }
            }
            if (routines?.isEmpty() == true) item("routines-empty") {
                JGroupItem(GroupPosition.Single) {
                    JRow("还没有定时任务", subtitle = "让 Jarvis 每天或每周固定的时间做一件事。", leadingIcon = PhIcons.Plus, onClick = actions.newRoutine)
                }
            }
            itemsIndexed(routines.orEmpty(), key = { index, routine -> "routine-$index-${routine.id}" }) { index, routine ->
                RoutineRow(routine, state.lastRunOf(routine), now, zone, GroupPosition.of(index, routines.orEmpty().size), state.routineBusy(routine.id),
                    onToggle = { actions.toggle(routine.id, it) }, onOpen = { actions.openRoutine(routine.id) },
                    onMore = { actions.routineMenu(routine) })
            }
            if (routines != null) item("routines-note") { SectionNote("到点 Jarvis 会开一段新对话做这件事，做完推送到手机。时间按助手设置里的时区。") }

            item("followups-header") { JSectionHeader("已安排的跟进", count = followups?.size?.takeIf { it > 0 }) }
            state.followups.error?.let { error ->
                item("followups-error") { JNotice(error, status = Status.NeedsYou, icon = PhIcons.WarningCircle, actionLabel = "重试", onAction = actions.refresh) }
            }
            if (followups?.isEmpty() == true) item("followups-empty") {
                JGroupItem(GroupPosition.Single) {
                    JRow("没有安排的跟进", subtitle = "在对话里说「一小时后提醒我」，Jarvis 会安排在这里。", leadingIcon = PhIcons.BellSimple)
                }
            }
            val shown = followups.orEmpty().take(FollowupsShown)
            val more = followups.orEmpty().size > FollowupsShown
            itemsIndexed(shown, key = { index, followup -> "followup-$index-${followup.id}" }) { index, followup ->
                FollowupRow(followup, machines, now, zone, GroupPosition(first = index == 0, last = index == shown.lastIndex && !more),
                    state.followupBusy(followup.id), onOpen = followup.conversation()?.let { key -> { actions.openConversation(key) } },
                    onMore = { actions.followupMenu(followup) })
            }
            if (more) item("followups-more") { MoreRow("全部 ${followups.orEmpty().size} 个跟进", GroupPosition(first = false, last = true), actions.allFollowups) }
            if (!followups.isNullOrEmpty()) item("followups-note") { SectionNote("Jarvis 在对话里替自己安排的一次性跟进，到点自己回来接着做。") }
        }
        if (!runs.isNullOrEmpty()) {
            item("runs-header") { JSectionHeader("最近运行") }
            val latest = runs.take(RunsShown)
            itemsIndexed(latest, key = { index, run -> "run-$index-${run.id}" }) { index, run ->
                RunRow(run, run.displayTitle(), parseTime(run.at)?.let { ScheduleText.moment(it, now, zone) }.orEmpty(),
                    GroupPosition(first = index == 0, last = false), run.conversation(state.assistantMachine), actions.openConversation)
            }
            item("runs-more") { MoreRow("全部运行记录", GroupPosition(first = false, last = true)) { actions.allRuns(null) } }
        } else state.runs.error?.let { error ->
            item("runs-header") { JSectionHeader("最近运行") }
            item("runs-error") { JNotice(error, status = Status.NeedsYou, icon = PhIcons.WarningCircle, actionLabel = "重试", onAction = actions.refresh) }
        }
    }
}

/** Every follow-up Jarvis has scheduled, soonest first. */
@Composable
internal fun FollowupsPage(
    state: ProactiveState, machines: List<Machine>, now: Instant, zone: ZoneId, onBack: () -> Unit, actions: ProactiveActions,
) {
    val followups = state.followupList
    JListPage(title = "已安排的跟进", onBack = onBack, refreshing = state.refreshing && followups != null, onRefresh = actions.refresh) {
        state.followups.error?.let { error ->
            item("error") {
                JNotice(error, title = if (followups == null) "读不到跟进" else null, status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                    actionLabel = "重试", onAction = actions.refresh)
            }
        }
        when {
            followups == null -> if (state.followups.error == null) item("loading") { JLoading() }
            followups.isEmpty() -> item("empty") {
                JEmptyState("没有安排的跟进", "在对话里说「一小时后提醒我……」「明早会前帮我准备」，Jarvis 会安排在这里。", icon = PhIcons.BellSimple)
            }
            else -> {
                itemsIndexed(followups, key = { index, followup -> "followup-$index-${followup.id}" }) { index, followup ->
                    FollowupRow(followup, machines, now, zone, GroupPosition.of(index, followups.size), state.followupBusy(followup.id),
                        onOpen = followup.conversation()?.let { key -> { actions.openConversation(key) } }, onMore = { actions.followupMenu(followup) })
                }
                item("note") { SectionNote("到点 Jarvis 自己回来接着做。点 ⋯ 可以立即运行或取消；接着原来对话的，点一下打开那段对话。") }
            }
        }
    }
}

/** Runs newest first, one section per day; [ref] narrows it to one routine (from its page). */
@Composable
internal fun RunsPage(
    state: ProactiveState, ref: String?, now: Instant, zone: ZoneId, onBack: () -> Unit, actions: ProactiveActions,
) {
    val runs = if (ref == null) state.runList else state.runsOf(ref)
    val routine = ref?.let { id -> state.shownRoutines?.firstOrNull { it.id == id } }
    val days = remember(runs, now) {
        runs.orEmpty().fold(mutableListOf<Pair<String, MutableList<Run>>>()) { groups, run ->
            val day = parseTime(run.at)?.let { ScheduleText.day(it, now, zone) } ?: "时间不详"
            if (groups.lastOrNull()?.first == day) groups.last().second += run else groups += day to mutableListOf(run)
            groups
        }
    }
    JListPage(
        title = routine?.title?.takeIf { it.isNotBlank() }?.let { "${it}的运行记录" } ?: "运行记录", onBack = onBack,
        refreshing = state.refreshing && runs != null, onRefresh = actions.refresh,
    ) {
        state.runs.error?.let { error ->
            item("error") {
                JNotice(error, title = if (runs == null) "读不到运行记录" else null, status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                    actionLabel = "重试", onAction = actions.refresh)
            }
        }
        when {
            runs == null -> if (state.runs.error == null) item("loading") { JLoading() }
            runs.isEmpty() -> item("empty") {
                JEmptyState("还没有运行过", if (ref == null) "定时任务和跟进每跑一次（包括失败和跳过的）都记在这里。" else "这个定时任务每跑一次（包括失败和跳过的）都记在这里。",
                    icon = PhIcons.ListChecks)
            }
            else -> days.forEach { (day, list) ->
                item("day-$day") { JSectionHeader(day, count = list.size) }
                itemsIndexed(list, key = { index, run -> "run-$day-$index-${run.id}" }) { index, run ->
                    val time = parseTime(run.at)?.let { ScheduleText.hm(it, zone) }.orEmpty()
                    RunRow(run, if (ref == null) run.displayTitle() else time.ifBlank { run.displayTitle() }, if (ref == null) time else "",
                        GroupPosition.of(index, list.size), run.conversation(state.assistantMachine), actions.openConversation)
                }
            }
        }
    }
}
