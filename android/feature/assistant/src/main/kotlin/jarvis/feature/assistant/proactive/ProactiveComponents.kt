package jarvis.feature.assistant.proactive

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.snapping.rememberSnapFlingBehavior
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.setProgress
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import jarvis.core.contracts.Machine
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.parseTime
import jarvis.core.contracts.api.Followup
import jarvis.core.contracts.api.Routine
import jarvis.core.contracts.api.Run
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.ZoneId
import java.util.Locale
import kotlin.math.abs
import kotlin.math.roundToInt

private val WheelRow = Tokens.Space.xxxl + Tokens.Space.s
private val DayButton = Tokens.Space.xxxl + Tokens.Space.s
private val TimeColumn = Tokens.Space.xxxl * 2 + Tokens.Space.s

/** A caption under a section, in the page's quiet ink. */
@Composable
internal fun SectionNote(text: String) {
    Text(text, Modifier.fillMaxWidth().padding(start = Tokens.Space.xs, end = Tokens.Space.xs, top = Tokens.Space.s),
        style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink3)
}

/** A timeline row, like Reminders: the time large on the left, what happens on the right, how long until. */
@Composable
internal fun UpcomingRow(item: Upcoming, now: Instant, zone: ZoneId, position: GroupPosition, onClick: (() -> Unit)?) {
    JGroupItem(position) {
        JRow(
            item.title, subtitle = item.detail, value = ScheduleText.until(item.at, now, zone),
            leading = {
                Column(Modifier.widthIn(min = TimeColumn)) {
                    Text(ScheduleText.hm(item.at, zone), style = JarvisTheme.type.number, color = JarvisTheme.palette.ink, maxLines = 1)
                    Text(ScheduleText.day(item.at, now, zone), style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2,
                        maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            },
            onClick = onClick, chevron = false,
        )
    }
}

/**
 * A routine: its rule in words, the next and last run, who set it up, and its switch. The picture carries a mark
 * when the last run failed (danger) or when Jarvis made it in a conversation.
 */
@Composable
internal fun RoutineRow(
    routine: Routine, lastRun: LastRun?, now: Instant, zone: ZoneId, position: GroupPosition, busy: Boolean,
    onToggle: (Boolean) -> Unit, onOpen: () -> Unit, onMore: () -> Unit,
) {
    val palette = JarvisTheme.palette
    val failed = lastRun?.failed == true
    val lines = listOfNotNull(
        ScheduleText.sentence(routine.time, routine.weekdays),
        routine.nextLine(now, zone),
        lastRun?.line(now, zone),
        "Jarvis 在对话里建的".takeIf { routine.byAssistant },
    )
    JGroupItem(position) {
        JRow(
            // Lines may wrap (a long reason), so leave room for them.
            routine.title.ifBlank { "定时任务" }, subtitle = lines.joinToString("\n"), subtitleLines = lines.size + 2,
            leading = {
                Box(Modifier.stableAlpha(if (routine.enabled) 1f else 0.55f)) {
                    JAvatar(routine.title, icon = routineIcon(routine.title),
                        badge = when { failed -> PhIcons.Warning; routine.byAssistant -> PhIcons.Sparkle; else -> null },
                        badgeColor = if (failed) palette.danger.dot else null)
                }
            },
            trailing = {
                if (busy) JSpinner(Modifier.size(Tokens.Space.xxl))
                else JSwitch(routine.enabled, onToggle, Modifier.semantics { contentDescription = "自动运行「${routine.title}」" })
            },
            onClick = onOpen, onLongClick = onMore,
        )
    }
}

/** A follow-up Jarvis scheduled for itself: when, where it goes, what it will do. ⋯ holds 立即运行 and 取消. */
@Composable
internal fun FollowupRow(
    followup: Followup, machines: List<Machine>, now: Instant, zone: ZoneId, position: GroupPosition, busy: Boolean,
    onOpen: (() -> Unit)?, onMore: () -> Unit,
) {
    val title = followup.displayTitle()
    val at = parseTime(followup.at)
    val lines = listOfNotNull(
        at?.let { "${ScheduleText.moment(it, now, zone)}，${ScheduleText.until(it, now, zone)}" },
        followup.whereLine(machines),
        followup.prompt.lineSequence().firstOrNull { it.isNotBlank() }?.trim()?.takeIf { it != title },
    )
    JGroupItem(position) {
        JRow(
            title, subtitle = lines.joinToString("\n"), subtitleLines = lines.size + 1,
            leading = { JAvatar(title, icon = if (followup.conversation() != null) PhIcons.ArrowBendUpLeft else PhIcons.BellRinging) },
            trailing = {
                if (busy) JSpinner(Modifier.size(Tokens.Space.xxl))
                else JIconButton(PhIcons.DotsThree, "「$title」的操作", onMore)
            },
            onClick = onOpen ?: onMore, onLongClick = onMore, chevron = false,
        )
    }
}

/** One run: how it went, when, and why when it did not run. Opens the conversation it started. */
@Composable
internal fun RunRow(
    run: Run, title: String, time: String, position: GroupPosition, conversation: SessionKey?, onOpen: (SessionKey) -> Unit,
) {
    val palette = JarvisTheme.palette
    val outcome = run.outcome
    val (icon, tint) = when (outcome) {
        RunOutcome.Ran -> PhIcons.CheckCircle to palette.done.dot
        RunOutcome.Failed -> PhIcons.WarningCircle to palette.danger.dot
        RunOutcome.Skipped -> PhIcons.ClockCountdown to palette.ink3
        RunOutcome.Unknown -> PhIcons.Question to palette.ink3
    }
    JGroupItem(position) {
        JRow(
            title, subtitle = listOf(time, run.detail()).filter { it.isNotBlank() && it != title }.joinToString("\n"), subtitleLines = 3,
            leading = { JIcon(icon, outcome.word, size = Tokens.Space.xxl, tint = tint, selected = true) },
            onClick = conversation?.let { key -> { onOpen(key) } },
        )
    }
}

/** A row that leads somewhere else in the page ("全部 5 个跟进"). */
@Composable
internal fun MoreRow(title: String, position: GroupPosition, onClick: () -> Unit) {
    JGroupItem(position) { JRow(title, onClick = onClick) }
}

/**
 * Hours and minutes as two wheels, 24-hour, like the clock app. Scrolling snaps to a value; tapping a value moves
 * there; TalkBack adjusts each wheel like a slider.
 */
@Composable
internal fun TimeWheel(time: String, onTimeChange: (String) -> Unit, modifier: Modifier = Modifier) {
    val parts = time.split(":").mapNotNull { it.toIntOrNull() }
    val hour = parts.getOrNull(0)?.coerceIn(0, 23) ?: 9
    val minute = parts.getOrNull(1)?.coerceIn(0, 59) ?: 0
    val latest by rememberUpdatedState(hour to minute)
    Box(modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
        // The band behind the chosen row.
        Box(Modifier.fillMaxWidth().height(WheelRow).clip(JarvisTheme.shapes.controlShape).background(JarvisTheme.palette.surfaceStrong))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Wheel(24, hour, "小时") { onTimeChange(String.format(Locale.ROOT, "%02d:%02d", it, latest.second)) }
            Text(":", style = JarvisTheme.type.number, color = JarvisTheme.palette.ink2, modifier = Modifier.padding(horizontal = Tokens.Space.s))
            Wheel(60, minute, "分钟") { onTimeChange(String.format(Locale.ROOT, "%02d:%02d", latest.first, it)) }
        }
    }
}

@Composable
private fun Wheel(count: Int, value: Int, description: String, onValue: (Int) -> Unit) {
    val state = rememberLazyListState(initialFirstVisibleItemIndex = value)
    val scope = rememberCoroutineScope()
    val report by rememberUpdatedState(onValue)
    val current by rememberUpdatedState(value)
    val selected by remember(state) {
        derivedStateOf {
            val info = state.layoutInfo
            val center = (info.viewportStartOffset + info.viewportEndOffset) / 2
            info.visibleItemsInfo.minByOrNull { abs(it.offset + it.size / 2 - center) }?.index ?: state.firstVisibleItemIndex
        }
    }
    // Report the value the wheel settles on; follow outside changes (a template, a reset) when it is still.
    LaunchedEffect(state) {
        snapshotFlow { state.isScrollInProgress }.filter { !it }.collect { if (selected != current) report(selected) }
    }
    LaunchedEffect(value) { if (!state.isScrollInProgress && selected != value) state.scrollToItem(value) }
    LazyColumn(
        Modifier.height(WheelRow * 5).width(WheelRow * 2).semantics {
            contentDescription = description
            stateDescription = "$selected"
            progressBarRangeInfo = ProgressBarRangeInfo(selected.toFloat(), 0f..(count - 1).toFloat(), steps = count - 2)
            setProgress { target -> scope.launch { state.animateScrollToItem(target.roundToInt().coerceIn(0, count - 1)) }; true }
        },
        state = state, flingBehavior = rememberSnapFlingBehavior(state),
        contentPadding = PaddingValues(vertical = WheelRow * 2), horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        items(count) { index ->
            val distance = abs(index - selected)
            Box(
                Modifier.height(WheelRow).fillMaxWidth().clickable(role = Role.Button) { scope.launch { state.animateScrollToItem(index) } },
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    String.format(Locale.ROOT, "%02d", index),
                    style = if (distance == 0) JarvisTheme.type.number else JarvisTheme.type.title,
                    color = when (distance) { 0 -> JarvisTheme.palette.ink; 1 -> JarvisTheme.palette.ink2; else -> JarvisTheme.palette.ink3 },
                    modifier = Modifier.stableAlpha(if (distance > 1) 0.6f else 1f), maxLines = 1,
                )
            }
        }
    }
}

/** 一 … 日 as round buttons (squares on the board) plus 每天 / 工作日 / 周末. The last day cannot be turned off. */
@Composable
internal fun WeekdayPicker(days: List<Int>, onDaysChange: (List<Int>) -> Unit, modifier: Modifier = Modifier) {
    val palette = JarvisTheme.palette
    val chosen = normalizeDays(days).ifEmpty { (1..7).toList() }.toSet()
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        Row(Modifier.fillMaxWidth()) {
            (1..7).forEach { day ->
                val on = day in chosen
                Box(Modifier.weight(1f), contentAlignment = Alignment.Center) {
                    Box(
                        Modifier.size(DayButton).clip(JarvisTheme.shapes.chipShape).background(if (on) palette.action else palette.surfaceStrong)
                            .clickable(role = Role.Checkbox) {
                                val next = if (on) chosen - day else chosen + day
                                if (next.isNotEmpty()) onDaysChange(normalizeDays(next.toList()))
                            }
                            .semantics { selected = on; contentDescription = "周${ScheduleText.dayNames[day - 1]}" },
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(ScheduleText.dayNames[day - 1], style = JarvisTheme.type.label, color = if (on) palette.onAction else palette.ink)
                    }
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            JChip("每天", selected = chosen.size == 7, onClick = { onDaysChange(emptyList()) })
            JChip("工作日", selected = chosen == Workdays.toSet(), onClick = { onDaysChange(Workdays) })
            JChip("周末", selected = chosen == setOf(6, 7), onClick = { onDaysChange(listOf(6, 7)) })
        }
    }
}
