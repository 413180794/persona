package jarvis.feature.chat

import jarvis.core.contracts.Interaction
import jarvis.core.contracts.api.InteractionGoneException
import jarvis.core.contracts.api.InteractionPromptChangedException
import jarvis.core.domain.answer.AnswerDraft
import kotlinx.coroutines.CancellationException

/** Sensitive forms must never enter saved request drafts, even after submission. */
internal fun AnswerDraft.forStorage(interaction: Interaction): AnswerDraft =
    if (interaction.secret == true) AnswerDraft() else copy(openedElicitationUrl = null, fields = kotlinx.serialization.json.JsonObject(fields.filterKeys { key ->
        jarvis.core.domain.answer.ElicitationForm.fields(interaction.elicitation?.requestedSchema).none { it.key == key && it.secret }
    }))

internal enum class InteractionSubmission(val notice: String, val withdraw: Boolean) {
    Answered("已回传你的选择", true),
    ResolvedElsewhere("已在电脑上处理", true),
    PromptChanged("屏幕内容已变化，本次未提交。请查看新问题后重新选择", true),
    Unknown("回传结果待确认，请先核对请求", false),
}

internal suspend fun submitInteraction(submit: suspend () -> Unit): InteractionSubmission = try {
    submit()
    InteractionSubmission.Answered
} catch (error: CancellationException) {
    throw error
} catch (_: InteractionGoneException) {
    InteractionSubmission.ResolvedElsewhere
} catch (_: InteractionPromptChangedException) {
    InteractionSubmission.PromptChanged
} catch (_: Exception) {
    InteractionSubmission.Unknown
}
