package jarvis.android.settings

import jarvis.android.data.Machine
import jarvis.android.data.MachineSystem
import jarvis.android.data.Session
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import java.util.Locale
import kotlin.math.abs

// Wire models and pure logic of 设置 and 机器 (iOS APIClient.ClaudeSetting / NotifyPrefs, Shared/MachineMetrics.swift).
// Decoded with JsonCodec: unknown fields ignored, missing ones default.

// --- Claude Code settings (GET/PUT /api/v1/machines/{mid}/claude-settings, server/internal/node/claudeconfig.go) ------

/** One whitelisted `~/.claude/settings.json` option. [value] null = unset (Claude Code uses [default]). */
@Serializable
data class ClaudeSetting(
    val key: String,
    val title: String = "",
    val detail: String = "",
    /** bool | enum | string. */
    val type: String = "string",
    val options: List<String>? = null,
    val default: JsonElement? = null,
    val value: JsonElement? = null,
) {
    /** What is in effect: the set value, else the default. */
    val effective: JsonElement? get() = value?.takeUnless { it is JsonNull } ?: default
    val isDefault: Boolean get() = value == null || value is JsonNull
    val boolValue: Boolean get() = (effective as? JsonPrimitive)?.booleanOrNull ?: false
    val stringValue: String get() = (effective as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull.orEmpty()

    /** The text field's placeholder: the default when it has one. */
    val placeholder: String
        get() = (default as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull?.takeIf { it.isNotEmpty() } ?: "未设置"
}

@Serializable
data class ClaudeSettingsBody(val settings: List<ClaudeSetting> = emptyList())

// --- Notification prefs (GET/PUT /api/v1/devices/{id}/prefs, server/internal/hub/notify.go) ---------------------------

/** 「Agent 做完时」: never / only long turns / every time (hub `done`). */
enum class DoneMode(val wire: String, val title: String) {
    OFF("off", "不推送"),
    LONG("long", "只推长任务"),
    ALWAYS("always", "每次都推");

    companion object {
        /** The hub normalizes anything else to "long"; so do we. */
        fun of(wire: String?): DoneMode = entries.firstOrNull { it.wire == wire } ?: LONG
    }
}

/** The part of a device's prefs this app reads; the rest (live activity, island) stays on the hub untouched. */
@Serializable
data class NotifyPrefs(
    val input: Boolean = true,
    val done: String = "long",
    @SerialName("long_turn_seconds") val longTurnSeconds: Int? = null,
    @SerialName("done_delay_seconds") val doneDelaySeconds: Int? = null,
) {
    val doneMode: DoneMode get() = DoneMode.of(done)

    companion object {
        /** "1 分钟" / "90 秒" (iOS `NotifyPrefs.duration`). */
        fun duration(seconds: Int?): String {
            val s = seconds ?: 60
            return if (s >= 60 && s % 60 == 0) "${s / 60} 分钟" else "$s 秒"
        }
    }
}

/** Only `input` / `done` go up: the hub decodes the body over what it has, so the iPhone-only fields stay. */
@Serializable
data class NotifyPrefsUpdate(val input: Boolean, val done: String)

// --- Devices (GET /api/v1/auth/tokens, DELETE /api/v1/auth/tokens/{id}) ------------------------------------------------

@Serializable
data class DeviceToken(
    val id: String = "",
    val name: String = "",
    /** phone | watch | other. */
    val kind: String = "other",
    /** full | answer (watches: look and answer only). */
    val role: String = "full",
    @SerialName("created_at") val createdAt: String? = null,
    @SerialName("last_seen") val lastSeen: String? = null,
    @SerialName("revoked_at") val revokedAt: String? = null,
) {
    val revoked: Boolean get() = jarvis.android.data.parseTime(revokedAt) != null
    val title: String get() = name.ifBlank { id }
}

/** The list and which one this phone is calling with ([current] empty: the pairing token). */
@Serializable
data class DeviceTokens(val tokens: List<DeviceToken> = emptyList(), val current: String = "")

/** "claude-opus-4" / "anthropic/claude-opus-4" → the last path part (iOS `Fmt.model`). */
fun modelLabel(model: String?): String? = model?.takeIf { it.isNotEmpty() }?.substringAfterLast('/')

// --- Machines --------------------------------------------------------------------------------------------------------

/** A machine's agents for its card / page: live ones, not the Jarvis tab's assistant conversations (iOS `MachineDetailView.agents`). */
fun machineAgents(sessions: List<Session>?): List<Session> = sessions.orEmpty().filter { it.isLive && !it.assistant }

/** The hub's own machine (kind "local"), else the first one. */
fun hubMachineId(machines: List<Machine>): String? = (machines.firstOrNull { it.kind == "local" } ?: machines.firstOrNull())?.id

/** 「Ubuntu 24.04」, else the OS's family name. */
fun osName(m: Machine?, system: MachineSystem? = m?.system): String? {
    system?.name?.takeIf { it.isNotBlank() }?.let { return it }
    return when (val os = m?.os ?: system?.os) {
        "darwin" -> "macOS"
        "linux" -> "Linux"
        "windows" -> "Windows"
        null, "" -> null
        else -> os
    }
}

/** The mark to draw for an OS (iOS `OSFamily`). */
enum class OSFamily {
    MACOS, UBUNTU, DEBIAN, FEDORA, REDHAT, CENTOS, ROCKY, ALMA, ARCH, ALPINE, OPENSUSE, NIXOS, LINUX, OTHER;

    /** The letter the badge shows for distributions without a drawn mark. */
    val letter: String?
        get() = when (this) {
            DEBIAN -> "d"
            FEDORA -> "f"
            REDHAT, ROCKY -> "R"
            CENTOS -> "C"
            ALMA -> "A"
            OPENSUSE -> "S"
            else -> null
        }

    companion object {
        fun of(system: MachineSystem?, os: String? = null): OSFamily {
            val ids = listOf(system?.distro.orEmpty().lowercase()) +
                system?.like.orEmpty().lowercase().split(' ').filter { it.isNotEmpty() }
            for (id in ids) {
                when (id) {
                    "macos" -> return MACOS
                    "ubuntu", "pop", "linuxmint", "elementary", "zorin" -> return UBUNTU
                    "debian", "raspbian", "kali" -> return DEBIAN
                    "fedora" -> return FEDORA
                    "rhel", "redhat" -> return REDHAT
                    "centos" -> return CENTOS
                    "rocky" -> return ROCKY
                    "almalinux" -> return ALMA
                    "arch", "manjaro", "endeavouros" -> return ARCH
                    "alpine" -> return ALPINE
                    "opensuse", "opensuse-leap", "opensuse-tumbleweed", "suse", "sles" -> return OPENSUSE
                    "nixos" -> return NIXOS
                }
            }
            return when (system?.os ?: os) {
                "darwin" -> MACOS
                "linux" -> LINUX
                else -> OTHER
            }
        }
    }
}

// --- Monitoring (GET /api/v1/machines/{mid}/metrics?range=, docs/machines.md) ----------------------------------------

/** One reading (every 10 s). Null: not measured. */
@Serializable
data class MetricSample(
    val t: Long = 0,
    val cpu: Double? = null,
    val cores: List<Double>? = null,
    val load: Double? = null,
    @SerialName("mem_used") val memUsed: Long = 0,
    @SerialName("mem_total") val memTotal: Long = 0,
    @SerialName("swap_used") val swapUsed: Long = 0,
    @SerialName("swap_total") val swapTotal: Long = 0,
    @SerialName("net_rx") val netRx: Double? = null,
    @SerialName("net_tx") val netTx: Double? = null,
    @SerialName("disk_used") val diskUsed: Long = 0,
    @SerialName("disk_total") val diskTotal: Long = 0,
) {
    val memPercent: Double? get() = if (memTotal > 0) 100.0 * memUsed / memTotal else null
    val diskPercent: Double? get() = if (diskTotal > 0) 100.0 * diskUsed / diskTotal else null
    val swapPercent: Double? get() = if (swapTotal > 0) 100.0 * swapUsed / swapTotal else null
}

/** One step of history: averages, or a break where the machine sent nothing. Memory / swap / disk in bytes. */
@Serializable
data class MetricPoint(
    val t: Long = 0,
    val gap: Boolean? = null,
    val cpu: Double? = null,
    val load: Double? = null,
    val mem: Double? = null,
    val swap: Double? = null,
    val rx: Double? = null,
    val tx: Double? = null,
    val disk: Double? = null,
) {
    val isGap: Boolean get() = gap == true
}

@Serializable
data class MachineMetrics(
    @SerialName("machine_id") val machineId: String = "",
    val online: Boolean = false,
    val system: MachineSystem? = null,
    val latest: MetricSample? = null,
    val points: List<MetricPoint> = emptyList(),
)

@Serializable
data class MachineMetricsBody(val metrics: MachineMetrics = MachineMetrics())

enum class MetricRange(val wire: String, val title: String, val seconds: Long) {
    HOUR("1h", "1 小时", 3600),
    SIX_HOURS("6h", "6 小时", 6 * 3600),
    DAY("24h", "24 小时", 24 * 3600);

    /** The hub's step for this range (seconds): a scrub finds the nearest point within two of them. */
    val step: Long get() = when (this) { HOUR -> 10; SIX_HOURS -> 60; DAY -> 240 }

    /** How often the page refetches while on screen. */
    val pollMillis: Long get() = if (this == HOUR) 10_000 else 60_000
}

/** How worrying a value is (iOS `MetricLevel`). */
enum class MetricLevel {
    NORMAL, WARNING, CRITICAL;

    companion object {
        fun of(v: Double?, warn: Double, crit: Double): MetricLevel = when {
            v == null -> NORMAL
            v >= crit -> CRITICAL
            v >= warn -> WARNING
            else -> NORMAL
        }
    }
}

/** One run of a series between gaps: what a chart draws as one line (iOS series "指标#段号"). */
class ChartSegment(val times: LongArray, val values: DoubleArray)

/**
 * [points]' values split at every gap marker, so no line crosses a time the machine sent nothing. A point without
 * the value (not measured) is skipped but does not break the line.
 */
fun segments(points: List<MetricPoint>, value: (MetricPoint) -> Double?): List<ChartSegment> {
    val out = mutableListOf<ChartSegment>()
    val ts = ArrayList<Long>()
    val vs = ArrayList<Double>()
    fun flush() {
        if (ts.isNotEmpty()) out += ChartSegment(ts.toLongArray(), vs.toDoubleArray())
        ts.clear(); vs.clear()
    }
    for (p in points) {
        if (p.isGap) { flush(); continue }
        val v = value(p) ?: continue
        ts += p.t; vs += v
    }
    flush()
    return out
}

/** The point nearest to [t] (Unix seconds), within two steps; a scrub over a gap finds nothing. */
fun nearestPoint(points: List<MetricPoint>, t: Long, step: Long): MetricPoint? {
    val p = points.filter { !it.isGap }.minByOrNull { abs(it.t - t) } ?: return null
    return if (abs(p.t - t) <= step * 2) p else null
}

object MetricFormat {
    /** "41.2 GB", "512 MB" (binary units, like Activity Monitor and free -h). */
    fun bytes(v: Double): String {
        val units = listOf("B", "KB", "MB", "GB", "TB")
        var x = v
        var i = 0
        while (x >= 1024 && i < units.size - 1) { x /= 1024; i++ }
        if (i == 0) return "${x.toInt()} B"
        val fmt = if (x >= 100) "%.0f %s" else if (x >= 10) "%.1f %s" else "%.2f %s"
        return String.format(Locale.US, fmt, x, units[i])
    }

    fun bytes(v: Long): String = bytes(v.toDouble())

    fun rate(v: Double?): String = v?.let { bytes(it) + "/s" } ?: "—"

    /** An axis label: "2 MB/s", "1.5 KB/s", "0" (no trailing zeros). */
    fun axisRate(v: Double): String {
        if (v <= 0) return "0"
        val units = listOf("B", "KB", "MB", "GB", "TB")
        var x = v
        var i = 0
        while (x >= 1024 && i < units.size - 1) { x /= 1024; i++ }
        val n = String.format(Locale.US, "%.1f", x).removeSuffix(".0")
        return "$n ${units[i]}/s"
    }

    /** "37%", "4.5%" under 10. */
    fun percent(v: Double?): String {
        v ?: return "—"
        return if (v < 10 && v > 0) String.format(Locale.US, "%.1f%%", v) else String.format(Locale.US, "%.0f%%", v)
    }

    fun load(v: Double?): String = v?.let { String.format(Locale.US, if (it >= 10) "%.1f" else "%.2f", it) } ?: "—"

    /** "3 天 4 小时", "5 小时 12 分钟", "8 分钟". */
    fun uptime(bootSeconds: Long, nowMillis: Long = System.currentTimeMillis()): String {
        val s = maxOf(0L, nowMillis / 1000 - bootSeconds)
        val d = s / 86400
        val h = s % 86400 / 3600
        val m = s % 3600 / 60
        if (d > 0) return if (h > 0) "$d 天 $h 小时" else "$d 天"
        if (h > 0) return "$h 小时 $m 分钟"
        return "$m 分钟"
    }
}

/** "刚刚", "5 分钟前", "3 小时前", "2 天前". */
fun relativeTime(t: java.time.Instant?, nowMillis: Long = System.currentTimeMillis()): String {
    t ?: return ""
    val d = nowMillis - t.toEpochMilli()
    return when {
        d < 60_000 -> "刚刚"
        d < 3_600_000 -> "${d / 60_000} 分钟前"
        d < 86_400_000 -> "${d / 3_600_000} 小时前"
        else -> "${d / 86_400_000} 天前"
    }
}
