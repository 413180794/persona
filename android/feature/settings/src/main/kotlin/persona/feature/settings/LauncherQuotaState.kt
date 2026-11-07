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
import android.content.res.Resources
import persona.core.designsystem.LocalizedFormats
import persona.core.designsystem.Tone
import persona.core.designsystem.failureReasonText
import java.time.Instant
import java.time.ZoneId
import java.util.Locale
import kotlin.math.abs
import kotlin.math.roundToInt

/** "Quota" on a launcher's page: the hub's last answer, a read in flight, or why it could not be read. */
internal data class LauncherQuotaState(
    val answer: LauncherQuotaAnswer? = null,
    val loading: Boolean = false,
    val failed: ResText? = null,
)

/**
 * Reads a launcher's quota at its gateway (docs/integrations/launchers.md "Quota") on the machine whose page it is. Read once when the
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
                catch (error: Exception) { mutable.update { it.copy(failed = ResText.of(R.string.settings_src_quota_read_failed, failureReasonText(error))) } }
                fresh = true
            } while (again)
        } finally { mutable.update { it.copy(loading = false) } }
    }
}

/** How quota numbers read, the same as on iOS (QuotaText) and the web (lib/quota.ts), in the language of [Resources]. */
internal object QuotaText {
    /** "Quota · <the gateway's name>"; the hub's name for it, else its id. */
    fun title(res: Resources, q: LauncherQuota?): String = q?.let {
        res.getString(R.string.settings_src_quota_title_named,
            it.gatewayName?.trim()?.takeIf(String::isNotEmpty) ?: it.gateway.ifBlank { res.getString(R.string.settings_src_quota_gateway) })
    } ?: res.getString(R.string.settings_src_quota_title)

    /** Something wrong with a pool (cooling down, used up, unreadable); "no quota interface" is just how it is. */
    fun trouble(p: QuotaPool): Boolean = p.state != null && p.state !in setOf("available", "ok", "unsupported")

    /** Approaching the limit is caution; only an exhausted pool is a failure. */
    fun tone(ratio: Double, exhausted: Boolean): Tone = when {
        exhausted -> Tone.Danger
        ratio >= 0.7 -> Tone.Caution
        else -> Tone.Neutral
    }

    /** Units of 10^4 and 10^8 in Chinese and Japanese (their numbers group by 10⁴), 12K and 340M otherwise. */
    fun count(res: Resources, v: Double): String {
        fun short(x: Double) = String.format(Locale.ROOT, "%.1f", x).removeSuffix(".0")
        val language = res.configuration.locales[0].language
        return if (language == "zh" || language == "ja") when {
            abs(v) >= 1e8 -> res.getString(R.string.settings_src_quota_count_1e8, short(v / 1e8))
            abs(v) >= 1e4 -> res.getString(R.string.settings_src_quota_count_1e4, short(v / 1e4))
            else -> String.format(Locale.ROOT, "%.0f", v)
        } else when {
            abs(v) >= 1e9 -> res.getString(R.string.settings_src_quota_count_1e9, short(v / 1e9))
            abs(v) >= 1e6 -> res.getString(R.string.settings_src_quota_count_1e6, short(v / 1e6))
            abs(v) >= 1e4 -> res.getString(R.string.settings_src_quota_count_1e3, short(v / 1e3))
            else -> String.format(Locale.ROOT, "%.0f", v)
        }
    }

    fun money(v: Double): String = if (v >= 100) String.format(Locale.ROOT, "%.0f", v) else String.format(Locale.ROOT, "%.2f", v)

    /** "64% used", "6401 / 10K", "$496 / $800", "Used up". */
    fun amount(res: Resources, w: QuotaWindow): String {
        if (w.exhausted && (w.usedRatio ?: 1.0) >= 1) return res.getString(R.string.settings_src_quota_used_up)
        val used = w.used
        val limit = w.limit
        if (used != null && limit != null) when (w.unit) {
            "usd" -> return "$${money(used)} / $${money(limit)}"
            "tokens" -> return res.getString(R.string.settings_src_quota_tokens, count(res, used), count(res, limit))
            "requests" -> return res.getString(R.string.settings_src_quota_requests, count(res, used), count(res, limit))
            "credits" -> return "${count(res, used)} / ${count(res, limit)}"
        }
        return w.usedRatio?.let { res.getString(R.string.settings_src_quota_used_percent, percent(it)) } ?: "—"
    }

    /** The bar's share, 0…100. */
    fun percent(ratio: Double): Int = (ratio.coerceIn(0.0, 1.0) * 100).roundToInt()

    fun usage(res: Resources, u: QuotaUsage): String =
        res.getQuantityString(R.plurals.settings_src_quota_usage, u.requests.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(),
            count(res, u.requests.toDouble()), count(res, u.tokens.toDouble())) + (u.costUsd?.let { " · $${money(it)}" } ?: "")

    /** "Resets Nov 6, 15:00", or "Reset" once it is past; empty when the time does not read. */
    fun reset(res: Resources, iso: String, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String {
        val at = parseTime(iso) ?: return ""
        if (!at.isAfter(now)) return res.getString(R.string.settings_src_quota_reset_done)
        return res.getString(R.string.settings_src_quota_resets,
            LocalizedFormats.format(at, LocalizedFormats.MONTH_DAY_TIME, res.configuration.locales[0], zone))
    }

    /** Nothing to show beyond the key's use: the key has no ceiling. */
    fun unlimited(q: LauncherQuota): Boolean = q.pools.isEmpty() && q.budget.isEmpty() && q.note.isNullOrBlank()

    /** [footer] has something to say. */
    fun hasFooter(a: LauncherQuotaAnswer): Boolean =
        !a.quota?.keyName.isNullOrBlank() || !a.machine.isNullOrBlank() || a.checkedAt?.let(::parseTime) != null

    /** The line under the section: the key's name at the gateway, which machine read it, when. */
    fun footer(res: Resources, a: LauncherQuotaAnswer, machineName: (String) -> String, now: Instant = Instant.now()): String {
        val parts = listOfNotNull(
            a.quota?.keyName?.takeIf { it.isNotBlank() }?.let { res.getString(R.string.settings_src_quota_key_name, it) },
            a.machine?.takeIf { it.isNotBlank() }?.let { res.getString(R.string.settings_src_quota_read_by, machineName(it)) },
            a.checkedAt?.let(::parseTime)?.let { res.getString(R.string.settings_src_quota_read_at, agoText(res, it, now)) },
        )
        return if (parts.isEmpty()) "" else res.getString(R.string.settings_src_quota_footer, parts.joinToString(res.getString(R.string.settings_src_quota_footer_separator)))
    }
}
