package persona.feature.chat

import android.content.res.Resources

/**
 * /loop typed in an agent's composer (#1586, web `lib/loop-command.ts`): Claude Code's own `/loop [interval] <task>`,
 * which re-runs the task on a schedule. The composer only helps write it: one line under the box says what it will do.
 */
internal data class LoopHint(val text: String, val missing: Boolean)

/** Claude Code's interval forms: a number and s / m / h / d (seconds round up to a minute). */
private val loopInterval = Regex("^(\\d+)([smhd])$", RegexOption.IGNORE_CASE)

/** The draft is a /loop command: the word alone or followed by a space (not "/looping"). */
internal fun isLoopDraft(draft: String): Boolean = Regex("^/loop(?:\\s|$)", RegexOption.IGNORE_CASE).containsMatchIn(draft)

/** What a /loop draft will do, in words; null for any other draft. */
internal fun loopHint(draft: String, resources: Resources): LoopHint? {
    if (!isLoopDraft(draft)) return null
    val words = draft.drop(SlashCommand.LOOP.length).trim().split(Regex("\\s+")).filter(String::isNotEmpty)
    val interval = words.firstOrNull()?.let(loopInterval::matchEntire)
    val task = (if (interval != null) words.drop(1) else words).joinToString(" ")
    val every = interval?.let { match ->
        val count = match.groupValues[1].toIntOrNull() ?: 0
        val plural = when (match.groupValues[2].lowercase()) {
            "s" -> R.plurals.chat_loop_seconds
            "m" -> R.plurals.chat_loop_minutes
            "h" -> R.plurals.chat_loop_hours
            else -> R.plurals.chat_loop_days
        }
        resources.getQuantityString(plural, count, count)
    }
    return when {
        task.isEmpty() && every != null -> LoopHint(resources.getString(R.string.chat_loop_every_no_task, every), missing = true)
        task.isEmpty() -> LoopHint(resources.getString(R.string.chat_loop_usage), missing = true)
        every != null -> LoopHint(resources.getString(R.string.chat_loop_every_task, every, task), missing = false)
        else -> LoopHint(resources.getString(R.string.chat_loop_self_paced, task), missing = false)
    }
}
