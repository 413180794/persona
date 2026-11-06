package persona.feature.chat

import android.content.res.Resources
import persona.core.contracts.Interaction
import persona.core.contracts.Resolution

/**
 * How a request was answered, in words: each question and its pick, or how a plan, permission or terminal dialog was
 * answered. [resolution] is the answer that took effect when the hub says the card was answered already (POST
 * …/resolve → `already`), as its history keeps it (secrets redacted). Mirrors web `lib/answered.ts` and iOS `Answered`.
 */
internal object AnsweredText {
    data class Line(val label: String, val answer: String)

    fun lines(res: Resources, interaction: Interaction, resolution: Resolution): List<Line> {
        fun one(id: Int, vararg args: Any) = listOf(Line("", res.getString(id, *args)))
        if (resolution.cancel == true) return one(R.string.chat_question_closed)
        if (resolution.dismiss == true) return one(R.string.chat_answered_dismissed)
        if (resolution.answeredBy == "assistant" && interaction.kind != "question") return one(R.string.chat_answered_by_assistant)
        return when (interaction.kind) {
            "question" -> {
                val answers = resolution.answers.orEmpty()
                interaction.questions.orEmpty().map { question ->
                    Line(question.question, answers[question.question]?.takeIf { it.isNotEmpty() } ?: res.getString(R.string.chat_answered_unanswered))
                }
            }
            "plan" -> when {
                resolution.approve == true -> modeLabel(resolution.mode)
                    ?.let { one(R.string.chat_answered_approved_mode, res.getString(it)) } ?: one(R.string.chat_answered_approved)
                !resolution.feedback.isNullOrEmpty() -> one(R.string.chat_answered_rejected_feedback, resolution.feedback!!)
                else -> one(R.string.chat_answered_rejected)
            }
            "permission" -> when (resolution.allow) {
                true -> one(if (resolution.always == true) R.string.chat_answered_always_allowed else R.string.chat_answered_allowed)
                false -> one(R.string.chat_answered_denied)
                null -> one(R.string.chat_answered_handled)
            }
            else -> {
                val options = interaction.dialog?.options.orEmpty()
                fun labelOf(key: String) = options.firstOrNull { it.key == key }?.label?.takeIf { it.isNotEmpty() } ?: key
                val typed = resolution.text?.takeIf { it.isNotEmpty() }?.let {
                    if (interaction.secret == true || it == REDACTED) res.getString(R.string.chat_answered_entered) else it
                }
                val picked = resolution.options?.map(::labelOf) ?: listOfNotNull(resolution.option?.takeIf { it.isNotEmpty() }?.let(::labelOf))
                val answer = (picked + listOfNotNull(typed)).filter { it.isNotEmpty() }
                    .joinToString(res.getString(R.string.chat_answered_separator))
                if (answer.isEmpty()) one(R.string.chat_answered_handled) else listOf(Line("", answer))
            }
        }
    }

    /** One line: the answer alone; several: each with what was asked. */
    fun summary(res: Resources, interaction: Interaction, resolution: Resolution): String {
        val lines = lines(res, interaction, resolution)
        if (lines.size == 1) return lines.single().answer
        return lines.joinToString(res.getString(R.string.chat_answered_separator)) { line ->
            if (line.label.isEmpty()) line.answer else res.getString(R.string.chat_answered_labeled, line.label, line.answer)
        }
    }

    /** The approval mode a plan was approved with, in words; null for none or one this app has no words for. */
    fun modeLabel(mode: String?) = when (mode) {
        "acceptEdits" -> R.string.chat_answered_mode_accept_edits
        "bypassPermissions" -> R.string.chat_answered_mode_bypass
        "default" -> R.string.chat_answered_mode_default
        else -> null
    }

    /** What the hub's history keeps in place of typed text it must not hand back. */
    private const val REDACTED = "<redacted>"
}
