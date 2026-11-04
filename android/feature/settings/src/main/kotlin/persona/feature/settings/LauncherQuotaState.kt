package persona.feature.settings

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import persona.core.contracts.api.LauncherQuota
import persona.core.contracts.parseTime
import persona.core.contracts.api.LauncherQuotaAnswer
import persona.core.contracts.api.MachineManagement
import persona.core.contracts.api.QuotaPool
import persona.core.contracts.api.QuotaUsage
import persona.core.contracts.api.QuotaWindow
import persona.core.designsystem.Tone
import persona.core.designsystem.failureReason
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlin.math.abs
import kotlin.math.roundToInt

/** 额度 on a launcher's page: the hub's last answer, a read in flight, or why it could not be read. */
internal data class LauncherQuotaState(
    val answer: LauncherQuotaAnswer? = null,
    val loading: Boolean = false,
    val failed: String? = null,
)

/**
 * Reads a launcher's quota at its gateway (docs/launchers.md「额度」) on the machine whose page it is. Read once when the
 * page opens (the hub keeps an answer a minute); [load] with refresh reads again. A failed read keeps the last answer.
 */
internal class LauncherQuotaModel(private val api: MachineManagement, private val machineId: String, private val name: String) {
    private val mutable = MutableStateFlow(LauncherQuotaState())
    val state = mutable.asStateFlow()

    private var again = false

    suspend fun load(refresh: Boolean = false) {
        // A ↻ while a read is out (the first one can wait 45 s on a machine) reads again once it is back, not never.
        if (state.value.loading) { again = again || refresh; return }
        mutable.update { it.copy(loading = true) }
        var fresh = refresh
        try {
            do {
                again = false
                try {
                    val answer = api.launcherQuota(machineId, name, fresh)
                    mutable.update { it.copy(answer = answer, failed = null) }
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (error: Exception) { mutable.update { it.copy(failed = "读不到额度：${failureReason(error)}") } }
                fresh = true
            } while (again)
        } finally { mutable.update { it.copy(loading = false) } }
    }
}

/** How quota numbers read, the same as on iOS (QuotaText) and the web (lib/quota.ts). */
internal object QuotaText {
    /** 额度 · <the gateway's name>; the hub's name for it, else its id. */
    fun title(q: LauncherQuota?): String = q?.let { "额度 · " + (it.gatewayName?.trim()?.takeIf(String::isNotEmpty) ?: it.gateway.ifBlank { "网关" }) } ?: "额度"

    /** Something wrong with a pool (cooling down, used up, unreadable); "no quota interface" is just how it is. */
    fun trouble(p: QuotaPool): Boolean = p.state != null && p.state !in setOf("available", "ok", "unsupported")

    /** ≥ 70 % caution, ≥ 90 % or used up danger. */
    fun tone(ratio: Double, exhausted: Boolean): Tone = when {
        exhausted || ratio >= 0.9 -> Tone.Danger
        ratio >= 0.7 -> Tone.Caution
        else -> Tone.Neutral
    }

    /** 1.2 万, 3.4 亿. */
    fun count(v: Double): String {
        fun short(x: Double) = String.format(Locale.ROOT, "%.1f", x).removeSuffix(".0")
        return when {
            abs(v) >= 1e8 -> short(v / 1e8) + " 亿"
            abs(v) >= 1e4 -> short(v / 1e4) + " 万"
            else -> String.format(Locale.ROOT, "%.0f", v)
        }
    }

    fun money(v: Double): String = if (v >= 100) String.format(Locale.ROOT, "%.0f", v) else String.format(Locale.ROOT, "%.2f", v)

    /** "已用 64%", "6401 / 1 万", "$496 / $800", "已用完". */
    fun amount(w: QuotaWindow): String {
        if (w.exhausted && (w.usedRatio ?: 1.0) >= 1) return "已用完"
        val used = w.used
        val limit = w.limit
        if (used != null && limit != null) when (w.unit) {
            "usd" -> return "$${money(used)} / $${money(limit)}"
            "tokens" -> return "${count(used)} / ${count(limit)} token"
            "requests" -> return "${count(used)} / ${count(limit)} 次"
            "credits" -> return "${count(used)} / ${count(limit)}"
        }
        return w.usedRatio?.let { "已用 ${percent(it)}%" } ?: "—"
    }

    /** The bar's share, 0…100. */
    fun percent(ratio: Double): Int = (ratio.coerceIn(0.0, 1.0) * 100).roundToInt()

    fun usage(u: QuotaUsage): String =
        "${count(u.requests.toDouble())} 次 · ${count(u.tokens.toDouble())} token" + (u.costUsd?.let { " · $${money(it)}" } ?: "")

    /** "11月6日 15:00 重置", or "已重置" once it is past; empty when the time does not read. */
    fun reset(iso: String, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String {
        val at = parseTime(iso) ?: return ""
        if (!at.isAfter(now)) return "已重置"
        return DateTimeFormatter.ofPattern("M月d日 HH:mm").withZone(zone).format(at) + " 重置"
    }

    /** Nothing to show beyond the key's use: the key has no ceiling. */
    fun unlimited(q: LauncherQuota): Boolean = q.pools.isEmpty() && q.budget.isEmpty() && q.note.isNullOrBlank()

    /** The line under the section: the key's name at the gateway, which machine read it, when. */
    fun footer(a: LauncherQuotaAnswer, machineName: (String) -> String, now: Instant = Instant.now()): String {
        val parts = listOfNotNull(
            a.quota?.keyName?.takeIf { it.isNotBlank() }?.let { "网关上的 Key 名：$it" },
            a.machine?.takeIf { it.isNotBlank() }?.let { "由 ${machineName(it)} 读取，Key 不离开那台电脑" },
            a.checkedAt?.let(::parseTime)?.let { "${ChannelText.ago(it, now)}读的，一分钟内再打开用这次的" },
        )
        return if (parts.isEmpty()) "" else parts.joinToString("；") + "。"
    }
}
