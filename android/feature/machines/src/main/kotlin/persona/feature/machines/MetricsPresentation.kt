package persona.feature.machines

import persona.core.contracts.*
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlin.math.abs

internal fun percent(used: Number?, total: Number?): Double? =
    if (used != null && total != null && total.toDouble() > 0) used.toDouble() * 100 / total.toDouble() else null

internal fun MachineMetrics.current(now: Long): MetricSample? =
    latest?.takeIf { online && now - it.t in 0..30 }

internal data class MetricAlert(val label: String, val critical: Boolean)
internal fun alerts(sample: MetricSample?, cores: Int?): List<MetricAlert> {
    if (sample == null) return emptyList()
    val result = mutableListOf<MetricAlert>()
    fun add(label: String, value: Double?, warning: Double, critical: Double) {
        if (value != null && value.isFinite() && value >= warning) result += MetricAlert(label, value >= critical)
    }
    add("CPU 高", sample.cpu, 75.0, 90.0)
    add("内存紧张", percent(sample.memUsed, sample.memTotal), 80.0, 90.0)
    add("磁盘快满", percent(sample.diskUsed, sample.diskTotal), 80.0, 90.0)
    add("负载高", cores?.takeIf { it > 0 }?.let { sample.load?.div(it) }, 1.0, 2.0)
    return result.sortedByDescending { it.critical }
}

internal fun machineSessions(sessions: List<Session>, machineId: String): List<Session> = sessions
    .filter { it.machineId == machineId && !it.assistant && it.isLive }
    .sortedWith(compareBy<Session> { it.agentState.priority }.thenByDescending { it.updated })

internal data class PlotPoint(val t: Long, val value: Double)

/** A separate path per run also breaks missing values for an individual measure. */
internal fun segments(points: List<MetricPoint>, step: Int, value: (MetricPoint) -> Double?): List<List<PlotPoint>> {
    val result = mutableListOf<List<PlotPoint>>()
    var run = mutableListOf<PlotPoint>()
    fun flush() { if (run.isNotEmpty()) result += run; run = mutableListOf() }
    for (point in points.sortedBy { it.t }) {
        val v = value(point)?.takeIf { it.isFinite() && it >= 0 }
        if (point.gap || v == null) { flush(); continue }
        if (run.lastOrNull()?.let { point.t - it.t > 3L * step } == true) flush()
        run += PlotPoint(point.t, v)
    }
    flush()
    return result
}

internal fun selectedPoint(points: List<MetricPoint>, time: Long, step: Int): MetricPoint? {
    val ordered = points.sortedBy { it.t }
    // Do not borrow an edge value from either side of an explicitly marked outage.
    val before = ordered.lastOrNull { it.t <= time }
    val after = ordered.firstOrNull { it.t >= time }
    if (before?.gap == true || after?.gap == true) return null
    if (before != null && after != null && after.t - before.t > 3L * step) return null
    return ordered.filterNot { it.gap }.minByOrNull { abs(it.t - time) }
        ?.takeIf { abs(it.t - time) <= 2L * step }
}

internal object MetricFormat {
    fun number(value: Double?): String = value?.takeIf(Double::isFinite)?.let { String.format(Locale.ROOT, "%.1f", it) } ?: "—"
    fun percent(value: Double?): String = value?.takeIf(Double::isFinite)?.let { "${number(it)}%" } ?: "—"
    fun bytes(value: Number?): String {
        var v = value?.toDouble()?.takeIf { it.isFinite() && it >= 0 } ?: return "—"
        val units = listOf("B", "KB", "MB", "GB", "TB")
        var unit = 0
        while (v >= 1024 && unit < units.lastIndex) { v /= 1024; unit++ }
        return "${number(v)} ${units[unit]}"
    }
    fun rate(value: Double?): String = if (value == null) "—" else "${bytes(value)}/s"
    fun time(seconds: Long): String = DateTimeFormatter.ofPattern("HH:mm").withZone(ZoneId.systemDefault()).format(Instant.ofEpochSecond(seconds))
    fun lastSeen(value: String?): String = parseTime(value)?.let {
        DateTimeFormatter.ofPattern("MM-dd HH:mm").withZone(ZoneId.systemDefault()).format(it)
    } ?: "未知"
    fun uptime(boot: Double?, now: Long): String? = boot?.takeIf { it.isFinite() && it > 0 && it <= now }?.let {
        val minutes = (now - it.toLong()) / 60
        when {
            minutes >= 1440 -> "${minutes / 1440} 天 ${minutes % 1440 / 60} 小时"
            minutes >= 60 -> "${minutes / 60} 小时 ${minutes % 60} 分钟"
            else -> "$minutes 分钟"
        }
    }
}
