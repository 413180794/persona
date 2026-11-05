package persona.feature.assistant.proactive

import android.content.Context
import android.content.res.Resources
import androidx.annotation.PluralsRes
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import persona.core.contracts.Machine
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.contracts.api.Run
import persona.core.contracts.api.ScheduledTask
import persona.core.designsystem.LocalizedFormats
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.designsystem.operationFeedback
import persona.core.domain.FailureAction
import persona.feature.assistant.R
import java.time.DayOfWeek
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.TextStyle
import java.time.temporal.ChronoUnit
import java.util.Locale
import kotlin.math.abs

/**
 * How the page says things, in the language the page is in (docs/i18n.md): the schedule, times, runs and failures.
 * Rules read like Reminders; a clock says the part of the day the way the language does (「上午 9:00」, "9:00 AM").
 */
internal class ProactiveText(private val res: Resources, val locale: Locale) {
    constructor(context: Context) : this(context.resources, LocalizedFormats.locale(context))

    fun string(@StringRes id: Int, vararg args: Any): String = res.getString(id, *args)
    private fun plural(@PluralsRes id: Int, count: Long, vararg args: Any): String =
        res.getQuantityString(id, count.toInt(), *args)

    private val dayParts = res.getStringArray(R.array.assistant_proactive_day_parts)
    /** Whether a list of days uses the narrow names (一、三、五 · 月・水・金) or the short ones (Mon, Wed, Fri). */
    private val narrowDayLists = res.getBoolean(R.bool.assistant_proactive_narrow_day_lists)

    /** A day's name on its button: 一 · M · 月 ([day] is 1 for Monday … 7 for Sunday). */
    fun dayName(day: Int): String = DayOfWeek.of(day).getDisplayName(TextStyle.NARROW, locale)

    /** A day on its own: 周三 · Wed · 水曜日. */
    fun weekday(day: Int): String =
        string(R.string.assistant_proactive_weekday, DayOfWeek.of(day).getDisplayName(TextStyle.SHORT, locale))

    private fun dayList(days: List<Int>): String =
        string(R.string.assistant_proactive_every_week_on, days.joinToString(string(R.string.assistant_proactive_day_separator)) {
            DayOfWeek.of(it).getDisplayName(if (narrowDayLists) TextStyle.NARROW else TextStyle.SHORT, locale)
        })

    /** "每天" / "工作日" / "周末" / "每周一、三、五". */
    fun repeats(days: List<Int>): String = when (val d = normalizeDays(days)) {
        emptyList<Int>() -> string(R.string.assistant_proactive_every_day)
        Workdays -> string(R.string.assistant_proactive_weekdays)
        listOf(6, 7) -> string(R.string.assistant_proactive_weekends)
        else -> dayList(d)
    }

    /** The rule in words: "每个工作日 上午 9:00", "每天 晚上 9:30", "每周五 下午 5:00". */
    fun sentence(time: String, days: List<Int>): String {
        val when_ = when (val d = normalizeDays(days)) {
            emptyList<Int>() -> string(R.string.assistant_proactive_every_day)
            Workdays -> string(R.string.assistant_proactive_rule_weekdays)
            listOf(6, 7) -> string(R.string.assistant_proactive_rule_weekends)
            else -> dayList(d)
        }
        return string(R.string.assistant_proactive_rule, when_, clock(time))
    }

    /** "每 30 分钟" / "每 2 小时" / "每 1 小时 30 分钟". */
    fun every(minutes: Int): String = when {
        minutes < 60 -> plural(R.plurals.assistant_proactive_every_minutes, minutes.toLong(), minutes)
        minutes % 60 == 0 -> plural(R.plurals.assistant_proactive_every_hours, (minutes / 60).toLong(), minutes / 60)
        else -> string(R.string.assistant_proactive_every_hours_minutes, minutes / 60, minutes % 60)
    }

    /** An interval on its own, for its chip: "30 分钟" / "2 小时" / "1 小时 30 分钟". */
    fun interval(minutes: Int): String = when {
        minutes < 60 -> plural(R.plurals.assistant_proactive_interval_minutes, minutes.toLong(), minutes)
        minutes % 60 == 0 -> plural(R.plurals.assistant_proactive_interval_hours, (minutes / 60).toLong(), minutes / 60)
        else -> string(R.string.assistant_proactive_interval_hours_minutes, minutes / 60, minutes % 60)
    }

    /** A task's rule in words: "每个工作日 上午 9:00", "每 30 分钟", or for a one-off when it runs ("明天 09:00"). */
    fun rule(task: ScheduledTask, now: Instant, zone: ZoneId): String = when (task.repeat) {
        TaskRepeat.Daily -> sentence(task.time.orEmpty().ifBlank { "09:00" }, task.weekdays)
        TaskRepeat.Interval -> every(task.everyMinutes.takeIf { it > 0 } ?: DefaultInterval)
        TaskRepeat.Once -> parseTime(task.at)?.let { moment(it, now, zone) } ?: string(R.string.assistant_proactive_repeat_once)
    }

    /** "09:00" → "上午 9:00" / "9:00 AM", "21:30" → "晚上 9:30", "00:15" → "凌晨 12:15". */
    fun clock(time: String): String {
        val parts = time.split(":").mapNotNull { it.toIntOrNull() }
        if (parts.size != 2 || parts[0] !in 0..23 || parts[1] !in 0..59) return time
        val (h, m) = parts
        val part = when (h) {
            in 0..4 -> 0
            in 5..8 -> 1
            in 9..11 -> 2
            12 -> 3
            in 13..17 -> 4
            else -> 5
        }
        return string(R.string.assistant_proactive_clock, dayParts[part], if (h % 12 == 0) 12 else h % 12, m)
    }

    /** A time of day in the language's habit: "17:05" · "5:05 PM". */
    fun hm(at: Instant, zone: ZoneId): String = LocalizedFormats.format(at, LocalizedFormats.TIME, locale, zone)

    /** "今天" / "明天" / "昨天" / "周三" (within a week either way) / "11月12日" / "2025年12月1日". */
    fun day(at: Instant, now: Instant, zone: ZoneId): String {
        val date = at.atZone(zone).toLocalDate()
        val today = now.atZone(zone).toLocalDate()
        val days = ChronoUnit.DAYS.between(today, date)
        return when {
            days == 0L -> string(R.string.assistant_proactive_today)
            days == 1L -> string(R.string.assistant_proactive_tomorrow)
            days == -1L -> string(R.string.assistant_proactive_yesterday)
            abs(days) < 7 -> weekday(date.dayOfWeek.value)
            date.year == today.year -> LocalizedFormats.format(at, LocalizedFormats.MONTH_DAY, locale, zone)
            else -> LocalizedFormats.format(at, LocalizedFormats.DATE, locale, zone)
        }
    }

    /** "今天 15:30" / "明天 09:00" / "周三 09:00" / "11月2日 09:00". */
    fun moment(at: Instant, now: Instant, zone: ZoneId) = string(R.string.assistant_proactive_moment, day(at, now, zone), hm(at, zone))

    /** A future moment, roughly: "马上" / "25 分钟后" / "2 小时 30 分钟后" / "5 小时后" / "3 天后"; "已过时间" when late. */
    fun until(at: Instant, now: Instant, zone: ZoneId): String {
        val seconds = Duration.between(now, at).seconds
        return when {
            seconds < -60 -> string(R.string.assistant_proactive_overdue)
            seconds < 60 -> string(R.string.assistant_proactive_now)
            seconds < 3600 -> (seconds / 60).let { plural(R.plurals.assistant_proactive_in_minutes, it, it) }
            seconds < 86400 -> {
                val h = seconds / 3600
                val m = seconds % 3600 / 60
                if (h < 3 && m >= 10) string(R.string.assistant_proactive_in_hours_minutes, h, m)
                else plural(R.plurals.assistant_proactive_in_hours, h, h)
            }
            else -> ChronoUnit.DAYS.between(now.atZone(zone).toLocalDate(), at.atZone(zone).toLocalDate()).coerceAtLeast(1)
                .let { plural(R.plurals.assistant_proactive_in_days, it, it) }
        }
    }

    /** "准时" / "晚了 21 秒" / "晚了 3 分钟" / "晚了 2 小时"; null without both times. */
    fun lateness(due: Instant?, at: Instant?): String? {
        if (due == null || at == null) return null
        val late = Duration.between(due, at).seconds
        return when {
            late < 5 -> string(R.string.assistant_proactive_on_time)
            late < 120 -> plural(R.plurals.assistant_proactive_late_seconds, late, late)
            late < 7200 -> (late / 60).let { plural(R.plurals.assistant_proactive_late_minutes, it, it) }
            else -> (late / 3600).let { plural(R.plurals.assistant_proactive_late_hours, it, it) }
        }
    }

    /** A count with the plural the language needs. */
    fun count(@PluralsRes id: Int, count: Int, vararg args: Any): String = res.getQuantityString(id, count, *args)
}

/** The page's [ProactiveText], following the language it is shown in. */
@Composable
internal fun rememberProactiveText(): ProactiveText {
    val context = LocalContext.current
    val configuration = LocalConfiguration.current
    return remember(context, configuration) { ProactiveText(context.resources, configuration.locales[0]) }
}

internal enum class RunOutcome(@StringRes val word: Int) {
    Ran(R.string.assistant_proactive_outcome_ran),
    Failed(R.string.assistant_proactive_outcome_failed),
    Skipped(R.string.assistant_proactive_outcome_skipped),
    Unknown(R.string.assistant_proactive_outcome_unknown),
}

internal fun outcomeOf(status: String?) = when (status) {
    "ok" -> RunOutcome.Ran
    "failed" -> RunOutcome.Failed
    "skipped" -> RunOutcome.Skipped
    else -> RunOutcome.Unknown
}

internal val Run.outcome: RunOutcome get() = outcomeOf(status)

/** What happened, in a few words: "准时", "手动运行", or why it did not run. */
internal fun Run.detail(say: ProactiveText): String = when (outcome) {
    RunOutcome.Ran -> if (manual) say.string(R.string.assistant_proactive_run_manually)
        else say.lateness(parseTime(due), parseTime(at)) ?: say.string(outcome.word)
    RunOutcome.Failed -> (hubErrorText(say, error)?.let { say.string(R.string.assistant_proactive_failed_because, it) } ?: say.string(outcome.word))
        .let { if (manual) say.string(R.string.assistant_proactive_run_manually_suffix, it) else it }
    RunOutcome.Skipped -> hubErrorText(say, error)?.let { say.string(R.string.assistant_proactive_skipped_because, it) } ?: say.string(outcome.word)
    RunOutcome.Unknown -> say.string(outcome.word)
}

/** The conversation a run started, when it started one. */
internal fun Run.conversation(fallbackMachine: String?): SessionKey? {
    val session = sessionId?.takeIf { it.isNotBlank() } ?: return null
    val machine = machineId?.takeIf { it.isNotBlank() } ?: fallbackMachine?.takeIf { it.isNotBlank() } ?: return null
    return SessionKey(machine, session)
}

internal fun Run.displayTitle(say: ProactiveText): String =
    title.ifBlank { say.string(if (kind == "followup") R.string.assistant_proactive_followup else R.string.assistant_proactive_routine) }

/** The hub's errors are Go strings; the common ones are said in the page's language, the rest shown as they are. */
internal fun hubErrorText(say: ProactiveText, message: String?): String? {
    val text = message?.trim().orEmpty()
    return when {
        text.isEmpty() -> null
        text == "assistant machine is offline" -> say.string(R.string.assistant_proactive_error_assistant_offline)
        text == "machine is offline" -> say.string(R.string.assistant_proactive_error_machine_offline)
        text == "no such routine" || text == "no such task" -> say.string(R.string.assistant_proactive_error_no_routine)
        text == "no such follow-up" -> say.string(R.string.assistant_proactive_error_no_followup)
        else -> text
    }
}

/** Why a list could not be read, as a sentence for the page. */
internal fun loadFailure(say: ProactiveText, error: Throwable): String {
    val feedback = operationFeedback(error)
    return when {
        feedback.offline -> say.string(R.string.assistant_proactive_load_offline)
        feedback.action == FailureAction.PairAgain || feedback.action == FailureAction.UpdateHub -> feedback.message
        else -> say.string(R.string.assistant_proactive_load_failed,
            hubErrorText(say, error.message) ?: say.string(R.string.assistant_proactive_unknown_error))
    }
}

/** Why an action failed, short enough to follow "没保存上：". */
internal fun failureReason(say: ProactiveText, error: Throwable): String {
    if (error is TaskGoneException) return say.string(R.string.assistant_proactive_routine_gone)
    // The transport gives up on a request when the pairing or the route to the hub changed under it.
    if (error is kotlinx.coroutines.CancellationException) return say.string(R.string.assistant_proactive_connection_changed)
    val feedback = operationFeedback(error)
    return when {
        feedback.offline -> say.string(R.string.assistant_proactive_offline)
        feedback.action == FailureAction.PairAgain || feedback.action == FailureAction.UpdateHub -> feedback.message.trimEnd('。', '.')
        else -> hubErrorText(say, error.message) ?: say.string(R.string.assistant_proactive_no_answer)
    }
}

/** "上次 今天 09:00 已运行" / "上次 10月26日 17:00 没跑起来：助手所在的机器离线". */
internal fun LastRun.line(say: ProactiveText, now: Instant, zone: ZoneId): String {
    val word = when {
        outcome == RunOutcome.Ran && manual -> say.string(R.string.assistant_proactive_run_manually)
        outcome == RunOutcome.Unknown -> say.string(RunOutcome.Ran.word)
        else -> say.string(outcome.word)
    }
    val what = hubErrorText(say, error)?.takeIf { outcome != RunOutcome.Ran }
        ?.let { say.string(R.string.assistant_proactive_outcome_because, word, it) } ?: word
    return at?.let { say.string(R.string.assistant_proactive_last_run_at, say.moment(it, now, zone), what) }
        ?: say.string(R.string.assistant_proactive_last_run, what)
}

internal val LastRun.failed: Boolean get() = outcome == RunOutcome.Failed
internal val ScheduledTask.byAssistant: Boolean get() = createdBy == "assistant"

/** "下次 明天 09:00，15 小时后" / "已关闭"; null when the hub has not worked out the next run. */
internal fun ScheduledTask.nextLine(say: ProactiveText, now: Instant, zone: ZoneId): String? {
    if (!enabled) return say.string(R.string.assistant_proactive_off_line)
    val next = parseTime(nextRun) ?: return null
    return say.string(R.string.assistant_proactive_next_line, say.moment(next, now, zone), say.until(next, now, zone))
}

/** A picture for a task, from its title, else from how it repeats (iOS picks the same way). */
internal fun taskIcon(title: String, repeat: TaskRepeat = TaskRepeat.Daily): PhIcon = when {
    "早" in title -> PhIcons.Sun // i18n-ignore: matches words in the user's own title
    "晚" in title || "回顾" in title -> PhIcons.MoonStars // i18n-ignore: matches words in the user's own title
    "周报" in title || "报告" in title -> PhIcons.FileText // i18n-ignore: matches words in the user's own title
    "会" in title -> PhIcons.Chats // i18n-ignore: matches words in the user's own title
    "盘点" in title || "待办" in title -> PhIcons.ListChecks // i18n-ignore: matches words in the user's own title
    "巡检" in title || "检查" in title -> PhIcons.MagnifyingGlass // i18n-ignore: matches words in the user's own title
    repeat == TaskRepeat.Once -> PhIcons.ArrowBendUpLeft
    repeat == TaskRepeat.Interval -> PhIcons.ArrowsClockwise
    else -> PhIcons.Clock
}

/** Its title, or the first line of the prompt for a one-off without one. */
internal fun ScheduledTask.displayTitle(say: ProactiveText): String =
    title.takeIf { it.isNotBlank() } ?: prompt.lineSequence().firstOrNull { it.isNotBlank() }?.trim()
        ?: say.string(if (repeat == TaskRepeat.Once) R.string.assistant_proactive_followup else R.string.assistant_proactive_routine)

internal fun machineName(id: String?, machines: List<Machine>): String? =
    id?.takeIf { it.isNotBlank() }?.let { machine -> machines.firstOrNull { it.id == machine }?.displayName ?: machine }

/** "在 服务器 上接着原来的对话" / "在同一个对话里运行" / "开一段新对话". */
internal fun ScheduledTask.whereLine(say: ProactiveText, machines: List<Machine>): String {
    val origin = origin
    return when {
        origin != null -> machineName(origin.machineId, machines)?.let { say.string(R.string.assistant_proactive_continues_on, it) }
            ?: say.string(R.string.assistant_proactive_continues)
        ownConversation != null -> say.string(R.string.assistant_proactive_same_conversation)
        else -> say.string(R.string.assistant_proactive_starts_new)
    }
}

/** What runs with when it is not the default: "自动", "全部放行"; null for the default. */
internal fun ScheduledTask.runsWith(say: ProactiveText): String? =
    permissionMode?.takeIf { it.isNotBlank() && it != "default" }?.let { say.string(permissionModeTitle(it)) }

/** One thing Persona will do on its own: a task's next run. */
internal data class Upcoming(
    val id: String,
    val at: Instant,
    val title: String,
    val repeat: TaskRepeat,
    val detail: String,
)

/** Soonest first: every enabled task's next run (a one-off's is its time). */
internal fun upcoming(
    say: ProactiveText, tasks: List<ScheduledTask>, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault(),
    machines: List<Machine> = emptyList(),
): List<Upcoming> =
    tasks.filter { it.enabled }.mapNotNull { t ->
        (parseTime(t.nextRun) ?: parseTime(t.at).takeIf { t.repeat == TaskRepeat.Once })?.let {
            Upcoming(t.id, it, t.displayTitle(say), t.repeat,
                if (t.repeat == TaskRepeat.Once) t.whereLine(say, machines) else say.rule(t, now, zone))
        }
    }.sortedWith(compareBy<Upcoming> { it.at }.thenBy { it.id })

/** Persona's one line about the page; null until something is read. */
internal fun summaryLine(say: ProactiveText, tasks: List<ScheduledTask>?, now: Instant, zone: ZoneId): String? {
    if (tasks == null) return null
    val on = tasks.count { it.enabled && it.repeat != TaskRepeat.Once }
    val next = upcoming(say, tasks, now, zone).firstOrNull { !it.at.isBefore(now) }
    val nextText = next?.let { say.moment(it.at, now, zone) }
    return when {
        tasks.isEmpty() -> say.string(R.string.assistant_proactive_summary_nothing)
        on > 0 && nextText != null -> say.count(R.plurals.assistant_proactive_summary_on_next, on, on, nextText)
        on > 0 -> say.count(R.plurals.assistant_proactive_summary_on, on, on)
        nextText != null -> say.string(R.string.assistant_proactive_summary_off_next, nextText)
        else -> say.string(R.string.assistant_proactive_summary_off)
    }
}

/** A permission mode's name ("" is 默认). */
@StringRes internal fun permissionModeTitle(mode: String): Int = when (mode) {
    "acceptEdits" -> R.string.assistant_proactive_mode_accept_edits
    "plan" -> R.string.assistant_proactive_mode_plan
    "auto" -> R.string.assistant_proactive_mode_auto
    BypassMode -> R.string.assistant_proactive_mode_bypass
    else -> R.string.assistant_proactive_mode_default
}

@StringRes internal fun permissionModeDetail(mode: String): Int = when (mode) {
    "acceptEdits" -> R.string.assistant_proactive_mode_accept_edits_detail
    "plan" -> R.string.assistant_proactive_mode_plan_detail
    "auto" -> R.string.assistant_proactive_mode_auto_detail
    BypassMode -> R.string.assistant_proactive_mode_bypass_detail
    else -> R.string.assistant_proactive_mode_default_detail
}

/**
 * Starting points for a new routine (the same three iOS offers), plus a blank one. The name is said in the page's
 * language; the instructions are a prompt for the model and stay as written.
 */
internal data class RoutineTemplate(
    @StringRes val name: Int, val icon: PhIcon, @StringRes val prompt: Int, val time: String, val weekdays: List<Int>,
) {
    /** The prompt is the task's own text, which the user reads and edits, so it is in the user's language. */
    fun draft(say: ProactiveText) = TaskDraft(say.string(name), say.string(prompt), TaskRepeat.Daily, time = time, weekdays = weekdays)
}

internal val RoutineTemplates = listOf(
    RoutineTemplate(R.string.assistant_proactive_template_meeting_prep, PhIcons.Chats,
        R.string.assistant_proactive_template_meeting_prep_prompt, "08:45", Workdays),
    RoutineTemplate(R.string.assistant_proactive_template_weekly_report, PhIcons.FileText,
        R.string.assistant_proactive_template_weekly_report_prompt, "17:00", listOf(5)),
    RoutineTemplate(R.string.assistant_proactive_template_end_of_day, PhIcons.ListChecks,
        R.string.assistant_proactive_template_end_of_day_prompt, "18:30", Workdays),
)

/** A blank task: weekdays at 09:00, on. */
internal val BlankTask = TaskDraft()
