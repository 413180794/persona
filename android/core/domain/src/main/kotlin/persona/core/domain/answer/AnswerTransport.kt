package persona.core.domain.answer

import persona.core.contracts.Resolution

enum class ResolveOutcome { Success, Gone, PromptChanged, Error }

data class ResolveAttempt(val allowed: Boolean, val resolution: Resolution?, val cardPresent: Boolean)

/** Pure projection of the existing optimistic card behavior; this does not send or retry requests. */
object AnswerTransport {
    fun begin(resolution: Resolution, apiAvailable: Boolean, networkUp: Boolean, state: String = "pending"): ResolveAttempt =
        if (apiAvailable && networkUp && state == "pending") ResolveAttempt(true, resolution, false)
        else ResolveAttempt(false, null, true)

    fun complete(attempt: ResolveAttempt, outcome: ResolveOutcome): ResolveAttempt =
        if (!attempt.allowed) attempt else attempt.copy(cardPresent = outcome == ResolveOutcome.Error)
}
