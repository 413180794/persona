package persona.core.domain.sort

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.AgentState
import persona.core.contracts.Session
import persona.core.contracts.WorkState
import persona.core.contracts.parseTime
import java.time.Duration
import java.time.Instant

// A session's work state as the lists show it (docs/agent-work.md; the web's lib/work.ts): the hub triages each stopped
// turn into done or blocked on something named, with one line for the row.

/** The hub waits it out: it wakes it (wake_at), or it wakes itself (its background tasks, a /loop). */
val WorkState.hubWaits: Boolean
    get() = phase == "blocked" && on != "user" && (wakeAt != null || on == "external" && wait.isNullOrBlank() && agent == null)

/**
 * The row's line for a stopped session the hub triaged: 「✓ 结论」 when done, what it waits for when blocked (with when the
 * hub acts next); null while it works or before the hub triaged its turn.
 */
fun Session.workLine(now: Instant): CoreText? {
    val w = work ?: return null
    val line = w.line?.takeIf(String::isNotBlank) ?: return null
    if (w.phase == "working" || agentState == AgentState.Working) return null
    if (w.phase == "done") return CoreText.of(Key.WorkLineDone, line)
    val wake = parseTime(w.wakeAt) ?: return CoreText.plain(line)
    val retry = w.on == "transient"
    val left = Duration.between(now, wake)
    val minutes = Math.round(left.seconds / 60.0)
    val whenText = when {
        left.seconds < 60 -> CoreText.of(if (retry) Key.RetryNow else Key.WakeNow)
        minutes < 60 -> CoreText.of(if (retry) Key.RetryInMinutes else Key.WakeInMinutes, minutes.toInt())
        minutes % 60 == 0L -> CoreText.of(if (retry) Key.RetryInHours else Key.WakeInHours, (minutes / 60).toInt())
        else -> CoreText.of(if (retry) Key.RetryInHoursMinutes else Key.WakeInHoursMinutes, minutes / 60, minutes % 60)
    }
    return CoreText.of(Key.Parenthesized, line, whenText)
}
