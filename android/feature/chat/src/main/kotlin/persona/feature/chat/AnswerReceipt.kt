package persona.feature.chat

import android.content.res.Resources
import persona.core.contracts.Interaction
import persona.core.contracts.Resolution
import persona.core.designsystem.resolve
import persona.core.domain.answer.permissionSummary

/**
 * What the app says right after an answer reached the hub (#4740, web #4356): which command was allowed, which option
 * was picked, that the plan was approved, so a glance confirms it without opening the conversation.
 * [text] is null when there is nothing of its own to say (handed back, a form, a dialog answered only with keys);
 * the caller then keeps its plain 「已回传你的选择」.
 */
data class AnswerReceipt(val interaction: Interaction, val resolution: Resolution) {
    fun text(res: Resources): String? {
        if (resolution.dismiss == true) return null
        return when (interaction.kind) {
            "permission" -> {
                val about = oneLine(interaction.permissionSummary().text.resolve(res)) ?: return null
                when (resolution.allow) {
                    true -> res.getString(if (resolution.always == true) R.string.chat_receipt_always_allowed else R.string.chat_receipt_allowed, about)
                    false -> res.getString(R.string.chat_receipt_denied, about)
                    null -> null
                }
            }
            "plan" -> when {
                resolution.approve == true -> res.getString(R.string.chat_receipt_plan_approved)
                resolution.approve == false -> oneLine(resolution.feedback.orEmpty())
                    ?.let { res.getString(R.string.chat_receipt_plan_rejected_feedback, it) } ?: res.getString(R.string.chat_receipt_plan_rejected)
                else -> null
            }
            "question" -> {
                if (resolution.answers.orEmpty().values.none(String::isNotEmpty)) return null
                oneLine(AnsweredText.summary(res, interaction, resolution))?.let { res.getString(R.string.chat_receipt_answered, it) }
            }
            "screen" -> {
                // Only a picked option or typed text says something; keys alone (Esc) read as the old notice.
                val picked = !resolution.option.isNullOrEmpty() || !resolution.options.isNullOrEmpty() || !resolution.text.isNullOrEmpty()
                if (!picked) null else oneLine(AnsweredText.summary(res, interaction, resolution))?.let { res.getString(R.string.chat_receipt_answered, it) }
            }
            else -> null
        }
    }

    private fun oneLine(text: String): String? {
        val line = text.lineSequence().map(String::trim).firstOrNull(String::isNotEmpty) ?: return null
        return if (line.length > MAX) line.take(MAX) + "…" else line
    }

    private companion object { const val MAX = 120 }
}
