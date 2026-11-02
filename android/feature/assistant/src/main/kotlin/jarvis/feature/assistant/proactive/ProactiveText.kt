package jarvis.feature.assistant.proactive

import jarvis.core.contracts.Machine
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.parseTime
import jarvis.core.contracts.api.Followup
import jarvis.core.contracts.api.Routine
import jarvis.core.contracts.api.Run
import jarvis.core.designsystem.icons.PhIcon
import jarvis.core.designsystem.icons.PhIcons
import jarvis.core.designsystem.operationFeedback
import jarvis.core.domain.FailureAction
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit
import java.util.Locale
import kotlin.math.abs

/** How the schedule is said on screen. Times are always 24-hour digits; rules read like Reminders. */
internal object ScheduleText {
    val dayNames = listOf("一", "二", "三", "四", "五", "六", "日")
    private val hm = DateTimeFormatter.ofPattern("HH:mm", Locale.ROOT)

    /** "每天" / "工作日" / "周末" / "每周一、三、五". */
    fun repeats(days: List<Int>): String = when (val d = normalizeDays(days)) {
        emptyList<Int>() -> "每天"
        Workdays -> "工作日"
        listOf(6, 7) -> "周末"
        else -> "每周" + d.joinToString("、") { dayNames[it - 1] }
    }

    /** The rule in words: "每个工作日 上午 9:00", "每天 晚上 9:30", "每周五 下午 5:00". */
    fun sentence(time: String, days: List<Int>): String {
        val when_ = when (val d = normalizeDays(days)) {
            emptyList<Int>() -> "每天"
            Workdays -> "每个工作日"
            listOf(6, 7) -> "每个周末"
            else -> "每周" + d.joinToString("、") { dayNames[it - 1] }
        }
        return "$when_ ${clock(time)}"
    }

    /** "09:00" → "上午 9:00", "21:30" → "晚上 9:30", "00:15" → "凌晨 12:15". */
    fun clock(time: String): String {
        val parts = time.split(":").mapNotNull { it.toIntOrNull() }
        if (parts.size != 2 || parts[0] !in 0..23 || parts[1] !in 0..59) return time
        val (h, m) = parts
        val part = when (h) {
            in 0..4 -> "凌晨"
            in 5..8 -> "早上"
            in 9..11 -> "上午"
            12 -> "中午"
            in 13..17 -> "下午"
            else -> "晚上"
        }
        return String.format(Locale.ROOT, "%s %d:%02d", part, if (h % 12 == 0) 12 else h % 12, m)
    }

    /** "17:05", always 24-hour. */
    fun hm(at: Instant, zone: ZoneId): String = hm.format(at.atZone(zone))

    /** "今天" / "明天" / "昨天" / "周三" (within a week either way) / "11月12日" / "2025年12月1日". */
    fun day(at: Instant, now: Instant, zone: ZoneId): String {
        val date = at.atZone(zone).toLocalDate()
        val today = now.atZone(zone).toLocalDate()
        val days = ChronoUnit.DAYS.between(today, date)
        return when {
            days == 0L -> "今天"
            days == 1L -> "明天"
            days == -1L -> "昨天"
            abs(days) < 7 -> "周" + dayNames[date.dayOfWeek.value - 1]
            date.year == today.year -> "${date.monthValue}月${date.dayOfMonth}日"
            else -> "${date.year}年${date.monthValue}月${date.dayOfMonth}日"
        }
    }

    /** "今天 15:30" / "明天 09:00" / "周三 09:00" / "11月2日 09:00". */
    fun moment(at: Instant, now: Instant, zone: ZoneId) = "${day(at, now, zone)} ${hm(at, zone)}"

    /** A future moment, roughly: "马上" / "25 分钟后" / "2 小时 30 分钟后" / "5 小时后" / "3 天后"; "已过时间" when late. */
    fun until(at: Instant, now: Instant, zone: ZoneId): String {
        val seconds = Duration.between(now, at).seconds
        return when {
            seconds < -60 -> "已过时间"
            seconds < 60 -> "马上"
            seconds < 3600 -> "${seconds / 60} 分钟后"
            seconds < 86400 -> {
                val h = seconds / 3600
                val m = seconds % 3600 / 60
                if (h < 3 && m >= 10) "$h 小时 $m 分钟后" else "$h 小时后"
            }
            else -> "${ChronoUnit.DAYS.between(now.atZone(zone).toLocalDate(), at.atZone(zone).toLocalDate()).coerceAtLeast(1)} 天后"
        }
    }

    /** "准时" / "晚了 21 秒" / "晚了 3 分钟" / "晚了 2 小时"; null without both times. */
    fun lateness(due: Instant?, at: Instant?): String? {
        if (due == null || at == null) return null
        val late = Duration.between(due, at).seconds
        return when {
            late < 5 -> "准时"
            late < 120 -> "晚了 $late 秒"
            late < 7200 -> "晚了 ${late / 60} 分钟"
            else -> "晚了 ${late / 3600} 小时"
        }
    }
}

internal enum class RunOutcome(val word: String) { Ran("已运行"), Failed("没跑起来"), Skipped("已跳过"), Unknown("没有结果记录") }

internal fun outcomeOf(status: String?) = when (status) {
    "ok" -> RunOutcome.Ran
    "failed" -> RunOutcome.Failed
    "skipped" -> RunOutcome.Skipped
    else -> RunOutcome.Unknown
}

internal val Run.outcome: RunOutcome get() = outcomeOf(status)

/** What happened, in a few words: "准时", "手动运行", or why it did not run. */
internal fun Run.detail(): String = when (outcome) {
    RunOutcome.Ran -> if (manual) "手动运行" else ScheduleText.lateness(parseTime(due), parseTime(at)) ?: outcome.word
    RunOutcome.Failed -> (hubErrorText(error)?.let { "没跑起来：$it" } ?: outcome.word) + if (manual) "（手动运行）" else ""
    RunOutcome.Skipped -> hubErrorText(error)?.let { "跳过了：$it" } ?: outcome.word
    RunOutcome.Unknown -> outcome.word
}

/** The conversation a run started, when it started one. */
internal fun Run.conversation(fallbackMachine: String?): SessionKey? {
    val session = sessionId?.takeIf { it.isNotBlank() } ?: return null
    val machine = machineId?.takeIf { it.isNotBlank() } ?: fallbackMachine?.takeIf { it.isNotBlank() } ?: return null
    return SessionKey(machine, session)
}

internal fun Run.displayTitle(): String = title.ifBlank { if (kind == "followup") "跟进" else "定时任务" }

/** The hub's errors are Go strings; the common ones are said in Chinese, the rest shown as they are. */
internal fun hubErrorText(message: String?): String? {
    val text = message?.trim().orEmpty()
    return when {
        text.isEmpty() -> null
        text == "assistant machine is offline" -> "助手所在的机器离线"
        text == "machine is offline" -> "机器离线"
        text == "no such routine" -> "这个定时任务已经不在了"
        text == "no such follow-up" -> "这个跟进已经不在了"
        else -> text
    }
}

/** Why a list could not be read, as a sentence for the page. */
internal fun loadFailure(error: Throwable): String {
    val feedback = operationFeedback(error)
    return when {
        feedback.offline -> "连不上 hub，连上后下拉刷新。"
        feedback.action == FailureAction.PairAgain || feedback.action == FailureAction.UpdateHub -> feedback.message
        else -> "hub 没有给出结果：${hubErrorText(error.message) ?: "未知错误"}"
    }
}

/** Why an action failed, short enough to follow "没保存上：". */
internal fun failureReason(error: Throwable): String {
    if (error is RoutineGoneException) return error.message.orEmpty()
    // The transport gives up on a request when the pairing or the route to the hub changed under it.
    if (error is kotlinx.coroutines.CancellationException) return "连接刚换过，请再试一次"
    val feedback = operationFeedback(error)
    return when {
        feedback.offline -> "连不上 hub"
        feedback.action == FailureAction.PairAgain || feedback.action == FailureAction.UpdateHub -> feedback.message.trimEnd('。')
        else -> hubErrorText(error.message) ?: "hub 没有回应"
    }
}

/** "上次 今天 09:00 已运行" / "上次 10月26日 17:00 没跑起来：助手所在的机器离线". */
internal fun LastRun.line(now: Instant, zone: ZoneId): String {
    val word = when {
        outcome == RunOutcome.Ran && manual -> "手动运行"
        outcome == RunOutcome.Unknown -> RunOutcome.Ran.word
        else -> outcome.word
    }
    val why = hubErrorText(error)?.takeIf { outcome != RunOutcome.Ran }?.let { "：$it" }.orEmpty()
    return "上次${at?.let { " " + ScheduleText.moment(it, now, zone) }.orEmpty()} $word$why"
}

internal val LastRun.failed: Boolean get() = outcome == RunOutcome.Failed
internal val Routine.byAssistant: Boolean get() = createdBy == "assistant"

/** "下次 明天 09:00，15 小时后" / "已关闭"; null when the hub has not worked out the next run. */
internal fun Routine.nextLine(now: Instant, zone: ZoneId): String? {
    if (!enabled) return "已关闭，不会自动运行"
    val next = parseTime(nextRun) ?: return null
    return "下次 ${ScheduleText.moment(next, now, zone)}，${ScheduleText.until(next, now, zone)}"
}

/** A picture for a routine, from its title (iOS picks the same way). */
internal fun routineIcon(title: String): PhIcon = when {
    "早" in title -> PhIcons.Sun
    "晚" in title || "回顾" in title -> PhIcons.MoonStars
    "周报" in title || "报告" in title -> PhIcons.FileText
    "会" in title -> PhIcons.Chats
    "盘点" in title || "待办" in title -> PhIcons.ListChecks
    "巡检" in title || "检查" in title -> PhIcons.MagnifyingGlass
    else -> PhIcons.Clock
}

internal fun Followup.displayTitle(): String =
    title?.takeIf { it.isNotBlank() } ?: prompt.lineSequence().firstOrNull { it.isNotBlank() }?.trim() ?: "跟进"

/** The conversation a follow-up continues; null when it will start a new one. */
internal fun Followup.conversation(): SessionKey? {
    val session = sessionId?.takeIf { it.isNotBlank() } ?: return null
    val machine = machineId?.takeIf { it.isNotBlank() } ?: return null
    return SessionKey(machine, session)
}

internal fun machineName(id: String?, machines: List<Machine>): String? =
    id?.takeIf { it.isNotBlank() }?.let { machine -> machines.firstOrNull { it.id == machine }?.displayName ?: machine }

/** "在 服务器 上接着原来的对话" / "在 服务器 上开一段新对话". */
internal fun Followup.whereLine(machines: List<Machine>): String {
    val machine = machineName(machineId, machines)
    val what = if (conversation() != null) "接着原来的对话" else "开一段新对话"
    return if (machine == null) what else "在 $machine 上$what"
}

/** One thing Jarvis will do on its own: a routine's next run or a follow-up. */
internal data class Upcoming(
    val id: String,
    val at: Instant,
    val title: String,
    val routine: Boolean,
    val detail: String,
    val conversation: SessionKey?,
)

/** Soonest first: enabled routines' next runs and every follow-up. */
internal fun upcoming(routines: List<Routine>, followups: List<Followup>): List<Upcoming> {
    val next = routines.filter { it.enabled }.mapNotNull { r ->
        parseTime(r.nextRun)?.let { Upcoming("r-${r.id}", it, r.title, true, ScheduleText.sentence(r.time, r.weekdays), null) }
    }
    val later = followups.mapNotNull { f ->
        parseTime(f.at)?.let {
            Upcoming("f-${f.id}", it, f.displayTitle(), false,
                if (f.conversation() != null) "跟进，接着原来的对话" else "跟进，开一段新对话", f.conversation())
        }
    }
    return (next + later).sortedWith(compareBy<Upcoming> { it.at }.thenBy { it.id })
}

/** Jarvis's one line about the page; null until something is read. */
internal fun summaryLine(routines: List<Routine>?, followups: List<Followup>?, now: Instant, zone: ZoneId): String? {
    if (routines == null && followups == null) return null
    val on = routines.orEmpty().count { it.enabled }
    val next = upcoming(routines.orEmpty(), followups.orEmpty()).firstOrNull { !it.at.isBefore(now) }
    val nextText = next?.let { "下一件在${ScheduleText.moment(it.at, now, zone)}" }
    return when {
        routines.orEmpty().isEmpty() && followups.orEmpty().isEmpty() -> "还没有安排，Jarvis 只在你开口时做事。"
        on > 0 && nextText != null -> "$on 个定时任务开着，$nextText。"
        on > 0 -> "$on 个定时任务开着。"
        nextText != null -> "定时任务都关着，$nextText。"
        else -> "定时任务都关着。"
    }
}

/** Starting points for a new routine (the same three iOS offers), plus a blank one. */
internal data class RoutineTemplate(val name: String, val icon: PhIcon, val draft: RoutineDraft)

internal val RoutineTemplates = listOf(
    RoutineTemplate("会前准备", PhIcons.Chats, RoutineDraft("会前准备",
        "看看今天飞书日历和手机日历里的会。对每个需要准备的会，用 schedule_followup 在会前 10 分钟安排一次会前准备：" +
            "相关文档、上次的会议纪要、还没定的事、我该说的要点。今天没有要准备的会就简单说一声。", "08:45", Workdays)),
    RoutineTemplate("周报", PhIcons.FileText, RoutineDraft("周报",
        "帮我起草这周的周报：从飞书日历、会议纪要、文档和消息里整理这周做了什么、进展和结果、遇到的问题、下周计划；" +
            "各台电脑上的 Agent 这周完成的主要改动也算进去。先给我草稿，问我要不要改，不要直接发出去。", "17:00", listOf(5))),
    RoutineTemplate("下班前盘点", PhIcons.ListChecks, RoutineDraft("下班前盘点",
        "下班前盘点：今天还有哪些消息没回、答应别人的事还没做、各台电脑上还在跑或在等我的 Agent，明天上午有哪些会要准备。" +
            "按紧急程度列出来，最后问我哪些要设提醒。", "18:30", Workdays)),
)

/** A blank routine: weekdays at 09:00, on. */
internal val BlankRoutine = RoutineDraft()
