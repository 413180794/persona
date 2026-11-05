package persona.feature.machines

import androidx.annotation.StringRes
import persona.core.contracts.api.DoctorCheck
import persona.core.contracts.api.DoctorReport

/*
 * 这台电脑能做什么 (docs/machines.md「自检」, the web's lib/node-health.ts): what `personad node doctor` finds on a computer,
 * read as features. Each feature stands on a set of the node's checks and takes the worst state among them; the raw
 * list stays one tap away.
 */

internal enum class FeatureState(@StringRes val label: Int) {
    Ready(R.string.machines_health_state_ready), Partial(R.string.machines_health_state_partial),
    Broken(R.string.machines_health_state_broken), Off(R.string.machines_health_state_off),
}

private class FeatureSpec(
    val id: String,
    @StringRes val name: Int,
    @StringRes val detail: Int,
    val checks: List<String>,
    /** Something a computer may well not use: a warning alone means 未启用. */
    val optional: Boolean = false,
    /** For an optional one: whether this computer needs it, which turns the warning into 有问题. */
    val needed: ((engine: String) -> Boolean)? = null,
)

/** The checks the node reports and no feature stands on: the user's own settings, shown as information. */
internal val InfoChecks = setOf("global-permission-hooks")

private val Features = listOf(
    FeatureSpec("approvals", R.string.machines_health_approvals, R.string.machines_health_approvals_detail,
        listOf("node.sock", "claude-settings.json", "claude-hooks", "assistant-settings.json", "assistant-hooks", "command-paths")),
    FeatureSpec("status", R.string.machines_health_status, R.string.machines_health_status_detail, listOf("node.sock", "command-paths")),
    FeatureSpec("terminal", R.string.machines_health_terminal, R.string.machines_health_terminal_detail, listOf("herdr"),
        optional = true, needed = { it in setOf("herdr", "herdr-both") }),
    FeatureSpec("mcp", R.string.machines_health_mcp, R.string.machines_health_mcp_detail, listOf("node.sock", "mcp.json", "command-paths")),
    FeatureSpec("skills", R.string.machines_health_skills, R.string.machines_health_skills_detail, listOf("agent-plugin", "claude-version")),
    FeatureSpec("runtimes", R.string.machines_health_runtimes, R.string.machines_health_runtimes_detail, listOf("runtime.sock"), optional = true),
)

/** Why a feature is not ready: a resource (a generated file 修复 rewrites) or the node's own words. */
internal sealed interface HealthNote {
    data class Res(@StringRes val id: Int) : HealthNote
    data class Plain(val text: String) : HealthNote
}

internal data class FeatureView(
    val id: String,
    @StringRes val name: Int,
    @StringRes val detail: Int,
    val state: FeatureState,
    val checks: List<DoctorCheck>,
    val note: HealthNote?,
)

private fun rank(status: String) = when (status) { "fail" -> 2; "warn" -> 1; else -> 0 }

private fun worst(checks: List<DoctorCheck>): DoctorCheck? =
    checks.filter { it.status != "ok" }.maxByOrNull { rank(it.status) }

/** A stale or missing generated file has a fix here (修复), so the node's hint, which names the command line, is not repeated. */
private fun noteOf(check: DoctorCheck?): HealthNote? = when {
    check == null -> null
    check.state == "stale" -> HealthNote.Res(R.string.machines_health_note_stale)
    check.state == "missing" -> HealthNote.Res(R.string.machines_health_note_missing)
    else -> (check.hint.ifBlank { check.detail }).takeIf { it.isNotBlank() }?.let(HealthNote::Plain)
}

/**
 * The report as features, in a fixed order. A feature none of whose checks the report has (an older node reports fewer)
 * is left out; a check no feature covers and that is not fine makes 其他检查, so nothing hides.
 */
internal fun deriveFeatures(report: DoctorReport, engine: String = "herdr"): List<FeatureView> {
    val byName = report.checks.associateBy { it.name }
    val out = Features.mapNotNull { spec ->
        val checks = spec.checks.mapNotNull(byName::get)
        if (checks.isEmpty()) return@mapNotNull null
        val w = worst(checks)
        val state = when (w?.status) {
            "fail" -> FeatureState.Broken
            "warn" -> if (spec.optional) (if (spec.needed?.invoke(engine) == true) FeatureState.Broken else FeatureState.Off) else FeatureState.Partial
            else -> FeatureState.Ready
        }
        FeatureView(spec.id, spec.name, spec.detail, state, checks, noteOf(w))
    }
    val covered = Features.flatMap { it.checks }.toSet() + InfoChecks
    val rest = report.checks.filter { it.name !in covered }
    val w = worst(rest) ?: return out
    return out + FeatureView("other", R.string.machines_health_other, R.string.machines_health_other_detail,
        if (w.status == "fail") FeatureState.Broken else FeatureState.Partial, rest, noteOf(w))
}

/** Features that are broken or partly working (一切正常 / N 项功能需要处理), and the ones not set up. */
internal fun attentionCount(features: List<FeatureView>) = features.count { it.state == FeatureState.Broken || it.state == FeatureState.Partial }

/** The checks 修复 would put right: Persona's own generated files that are stale or missing. */
internal fun fixableChecks(report: DoctorReport) = report.checks.filter { it.state == "stale" || it.state == "missing" }

/** The user's own settings the node only looks at: a row of information, not a feature. */
internal fun infoChecks(report: DoctorReport) = report.checks.filter { it.name in InfoChecks && it.status != "ok" }

/** Why a self-check could not be had, from the hub's answer (the web's `doctorUnavailable`). */
internal enum class DoctorUnavailable(@StringRes val text: Int) {
    OldNode(R.string.machines_health_old_node), OldHub(R.string.machines_health_old_hub),
    Offline(R.string.machines_health_went_offline), Forbidden(R.string.machines_health_forbidden);

    companion object {
        /** An old node answers "unknown method"; an old hub has no such route (a plain 404, not the JSON of "unknown machine"). */
        fun of(status: Int?, message: String?, json: Boolean): DoctorUnavailable? = when {
            message?.contains("unknown method", ignoreCase = true) == true -> OldNode
            status == 404 && !json -> OldHub
            status == 503 -> Offline
            status == 403 -> Forbidden
            else -> null
        }
    }
}
