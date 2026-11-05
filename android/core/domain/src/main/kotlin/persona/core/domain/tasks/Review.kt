package persona.core.domain.tasks

import persona.core.contracts.Supervision
import persona.core.contracts.TaskReview
import persona.core.contracts.Todo

// A task the main conversation supervises (#3124, docs/unified-entry.md「督办」): where its review stands and how each
// criterion was found, for the task card, the todo page and the conversation's review lines.

/** Where a supervised task's review stands. */
enum class ReviewStage { Ongoing, Pending, Rework, NeedsUser, Accepted, Cancelled }

/** The stage that holds now (an accepted or cancelled task the user reopened goes on again); null when not supervised. */
val Todo.reviewStage: ReviewStage?
    get() {
        val sup = supervision ?: return null
        return when (sup.state) {
            "pending_review" -> ReviewStage.Pending
            "rework" -> ReviewStage.Rework
            "needs_user" -> ReviewStage.NeedsUser
            "accepted" -> if (isDone) ReviewStage.Accepted else ReviewStage.Ongoing
            "cancelled" -> if (isDone) ReviewStage.Cancelled else ReviewStage.Ongoing
            else -> ReviewStage.Ongoing
        }
    }

/** A review's verdict as a stage (a review line in the conversation); null for one it does not know. */
fun reviewVerdictStage(verdict: String): ReviewStage? = when (verdict) {
    "accepted" -> ReviewStage.Accepted
    "rework" -> ReviewStage.Rework
    "needs_user" -> ReviewStage.NeedsUser
    "cancelled" -> ReviewStage.Cancelled
    else -> null
}

enum class CheckMark { Met, Missing, Open }

data class CheckLine(val criterion: String, val mark: CheckMark, val evidence: String?)

/**
 * Each criterion as the latest review that judged it found it: met, missing (with its evidence or gap), or open (not
 * reviewed yet); criteria a review judged that are not in done_when (a task without any) follow.
 */
fun Supervision.checklist(): List<CheckLine> {
    val latest = LinkedHashMap<String, persona.core.contracts.ReviewCheck>()
    for (r in reviews) for (c in r.checks) {
        latest.remove(c.criterion)
        latest[c.criterion] = c
    }
    val out = doneWhen.map { criterion ->
        val c = latest[criterion]
        CheckLine(criterion, when { c == null -> CheckMark.Open; c.met -> CheckMark.Met; else -> CheckMark.Missing }, c?.evidence?.takeIf { it.isNotBlank() })
    }
    val more = latest.values.filter { it.criterion !in doneWhen }
        .map { CheckLine(it.criterion, if (it.met) CheckMark.Met else CheckMark.Missing, it.evidence?.takeIf { e -> e.isNotBlank() }) }
    return out + more
}

/** Criteria met and all of them ("2/3" on a card); null without criteria. */
fun Supervision.checkCount(): Pair<Int, Int>? {
    val list = checklist()
    if (list.isEmpty()) return null
    return list.count { it.mark == CheckMark.Met } to list.size
}

val Supervision.latestReview: TaskReview? get() = reviews.lastOrNull()
