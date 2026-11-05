package persona.feature.assistant.proactive

import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import persona.core.contracts.Machine
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.contracts.api.Followup
import persona.core.contracts.api.Routine
import persona.core.contracts.api.Run
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.feature.assistant.R
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
 * 主动做事: what runs next, the routines with their switches, the follow-ups Persona scheduled, and the latest runs
 * (iOS AssistantRoutinesView). Each part opens a page of its own.
 */
@Composable
internal fun OverviewPage(
    state: ProactiveState, machines: List<Machine>, now: Instant, zone: ZoneId, onBack: () -> Unit, actions: ProactiveActions,
) {
    val routines = state.shownRoutines
    val followups = state.followupList
    val runs = state.runList
    val say = rememberProactiveText()
    val retry = stringResource(R.string.assistant_proactive_retry)
    val timeline = remember(routines, followups, say) { upcoming(say, routines.orEmpty(), followups.orEmpty()) }
    val coming = timeline.filter { it.at.isAfter(now.minusSeconds(60)) }.take(4)
    val readErrors = listOfNotNull(state.routines.error, state.followups.error).distinct()
    JListPage(
        title = stringResource(R.string.assistant_proactive_title), summary = summaryLine(say, routines, followups, now, zone), onBack = onBack,
        largeTitle = true, actions = { JIconButton(PhIcons.Plus, stringResource(R.string.assistant_proactive_new_routine), actions.newRoutine) },
        refreshing = state.refreshing && state.loaded, onRefresh = actions.refresh,
    ) {
        if (!state.loaded) {
            if (readErrors.isEmpty()) item("loading") { JLoading(text = stringResource(R.string.assistant_proactive_loading)) }
            else item("read-error") {
                JNotice(readErrors.first(), title = stringResource(R.string.assistant_proactive_read_failed), status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                    actionLabel = retry, onAction = actions.refresh)
            }
            return@JListPage
        }
        val nothing = routines?.isEmpty() == true && followups?.isEmpty() == true
        if (nothing) item("empty") {
            JEmptyState(
                stringResource(R.string.assistant_proactive_empty_title), stringResource(R.string.assistant_proactive_empty_body),
                icon = PhIcons.CalendarBlank,
                action = { JButton(stringResource(R.string.assistant_proactive_new_routine), actions.newRoutine, icon = PhIcons.Plus) },
            )
        }
        if (coming.isNotEmpty()) {
            item("upcoming-header") { JSectionHeader(stringResource(R.string.assistant_proactive_upcoming)) }
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
            item("routines-header") { JSectionHeader(stringResource(R.string.assistant_proactive_routines), count = routines?.size?.takeIf { it > 0 }) }
            state.routines.error?.let { error ->
                item("routines-error") { JNotice(error, status = Status.NeedsYou, icon = PhIcons.WarningCircle, actionLabel = retry, onAction = actions.refresh) }
            }
            if (routines?.isEmpty() == true) item("routines-empty") {
                JGroupItem(GroupPosition.Single) {
                    JRow(stringResource(R.string.assistant_proactive_no_routines), subtitle = stringResource(R.string.assistant_proactive_no_routines_body), leadingIcon = PhIcons.Plus, onClick = actions.newRoutine)
                }
            }
            itemsIndexed(routines.orEmpty(), key = { index, routine -> "routine-$index-${routine.id}" }) { index, routine ->
                RoutineRow(routine, state.lastRunOf(routine), say, now, zone, GroupPosition.of(index, routines.orEmpty().size), state.routineBusy(routine.id),
                    onToggle = { actions.toggle(routine.id, it) }, onOpen = { actions.openRoutine(routine.id) },
                    onMore = { actions.routineMenu(routine) })
            }
            if (routines != null) item("routines-note") { SectionNote(stringResource(R.string.assistant_proactive_routines_note)) }

            item("followups-header") { JSectionHeader(stringResource(R.string.assistant_proactive_followups), count = followups?.size?.takeIf { it > 0 }) }
            state.followups.error?.let { error ->
                item("followups-error") { JNotice(error, status = Status.NeedsYou, icon = PhIcons.WarningCircle, actionLabel = retry, onAction = actions.refresh) }
            }
            if (followups?.isEmpty() == true) item("followups-empty") {
                JGroupItem(GroupPosition.Single) {
                    JRow(stringResource(R.string.assistant_proactive_no_followups), subtitle = stringResource(R.string.assistant_proactive_no_followups_body), leadingIcon = PhIcons.BellSimple)
                }
            }
            val shown = followups.orEmpty().take(FollowupsShown)
            val more = followups.orEmpty().size > FollowupsShown
            itemsIndexed(shown, key = { index, followup -> "followup-$index-${followup.id}" }) { index, followup ->
                FollowupRow(followup, machines, say, now, zone, GroupPosition(first = index == 0, last = index == shown.lastIndex && !more),
                    state.followupBusy(followup.id), onOpen = followup.conversation()?.let { key -> { actions.openConversation(key) } },
                    onMore = { actions.followupMenu(followup) })
            }
            if (more) item("followups-more") { MoreRow(pluralStringResource(R.plurals.assistant_proactive_all_followups, followups.orEmpty().size, followups.orEmpty().size), GroupPosition(first = false, last = true), actions.allFollowups) }
            if (!followups.isNullOrEmpty()) item("followups-note") { SectionNote(stringResource(R.string.assistant_proactive_followups_note)) }
        }
        if (!runs.isNullOrEmpty()) {
            item("runs-header") { JSectionHeader(stringResource(R.string.assistant_proactive_recent_runs)) }
            val latest = runs.take(RunsShown)
            itemsIndexed(latest, key = { index, run -> "run-$index-${run.id}" }) { index, run ->
                RunRow(run, run.displayTitle(say), parseTime(run.at)?.let { say.moment(it, now, zone) }.orEmpty(),
                    GroupPosition(first = index == 0, last = false), run.conversation(state.assistantMachine), say, actions.openConversation)
            }
            item("runs-more") { MoreRow(stringResource(R.string.assistant_proactive_all_runs), GroupPosition(first = false, last = true)) { actions.allRuns(null) } }
        } else state.runs.error?.let { error ->
            item("runs-header") { JSectionHeader(stringResource(R.string.assistant_proactive_recent_runs)) }
            item("runs-error") { JNotice(error, status = Status.NeedsYou, icon = PhIcons.WarningCircle, actionLabel = retry, onAction = actions.refresh) }
        }
    }
}

/** Every follow-up Persona has scheduled, soonest first. */
@Composable
internal fun FollowupsPage(
    state: ProactiveState, machines: List<Machine>, now: Instant, zone: ZoneId, onBack: () -> Unit, actions: ProactiveActions,
) {
    val followups = state.followupList
    val say = rememberProactiveText()
    val retry = stringResource(R.string.assistant_proactive_retry)
    JListPage(title = stringResource(R.string.assistant_proactive_followups), onBack = onBack, refreshing = state.refreshing && followups != null, onRefresh = actions.refresh) {
        state.followups.error?.let { error ->
            item("error") {
                JNotice(error, title = if (followups == null) stringResource(R.string.assistant_proactive_followups_read_failed) else null, status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                    actionLabel = retry, onAction = actions.refresh)
            }
        }
        when {
            followups == null -> if (state.followups.error == null) item("loading") { JLoading() }
            followups.isEmpty() -> item("empty") {
                JEmptyState(stringResource(R.string.assistant_proactive_no_followups), stringResource(R.string.assistant_proactive_followups_empty_body), icon = PhIcons.BellSimple)
            }
            else -> {
                itemsIndexed(followups, key = { index, followup -> "followup-$index-${followup.id}" }) { index, followup ->
                    FollowupRow(followup, machines, say, now, zone, GroupPosition.of(index, followups.size), state.followupBusy(followup.id),
                        onOpen = followup.conversation()?.let { key -> { actions.openConversation(key) } }, onMore = { actions.followupMenu(followup) })
                }
                item("note") { SectionNote(stringResource(R.string.assistant_proactive_followups_page_note)) }
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
    val say = rememberProactiveText()
    val retry = stringResource(R.string.assistant_proactive_retry)
    val unknownTime = stringResource(R.string.assistant_proactive_unknown_time)
    val days = remember(runs, now, say) {
        runs.orEmpty().fold(mutableListOf<Pair<String, MutableList<Run>>>()) { groups, run ->
            val day = parseTime(run.at)?.let { say.day(it, now, zone) } ?: unknownTime
            if (groups.lastOrNull()?.first == day) groups.last().second += run else groups += day to mutableListOf(run)
            groups
        }
    }
    JListPage(
        title = routine?.title?.takeIf { it.isNotBlank() }?.let { stringResource(R.string.assistant_proactive_routine_runs, it) } ?: stringResource(R.string.assistant_proactive_runs), onBack = onBack,
        refreshing = state.refreshing && runs != null, onRefresh = actions.refresh,
    ) {
        state.runs.error?.let { error ->
            item("error") {
                JNotice(error, title = if (runs == null) stringResource(R.string.assistant_proactive_runs_read_failed) else null, status = Status.NeedsYou, icon = PhIcons.WarningCircle,
                    actionLabel = retry, onAction = actions.refresh)
            }
        }
        when {
            runs == null -> if (state.runs.error == null) item("loading") { JLoading() }
            runs.isEmpty() -> item("empty") {
                JEmptyState(stringResource(R.string.assistant_proactive_no_runs),
                    if (ref == null) stringResource(R.string.assistant_proactive_no_runs_body) else stringResource(R.string.assistant_proactive_no_routine_runs_body),
                    icon = PhIcons.ListChecks)
            }
            else -> days.forEach { (day, list) ->
                item("day-$day") { JSectionHeader(day, count = list.size) }
                itemsIndexed(list, key = { index, run -> "run-$day-$index-${run.id}" }) { index, run ->
                    val time = parseTime(run.at)?.let { say.hm(it, zone) }.orEmpty()
                    RunRow(run, if (ref == null) run.displayTitle(say) else time.ifBlank { run.displayTitle(say) }, if (ref == null) time else "",
                        GroupPosition.of(index, list.size), run.conversation(state.assistantMachine), say, actions.openConversation)
                }
            }
        }
    }
}
