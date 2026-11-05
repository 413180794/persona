package persona.feature.chat

import persona.core.contracts.Interaction
import persona.core.contracts.api.InteractionGoneException
import persona.core.contracts.api.InteractionPromptChangedException
import persona.core.domain.answer.AnswerDraft
import kotlinx.coroutines.CancellationException

/** Sensitive forms must never enter saved request drafts, even after submission. */
internal fun AnswerDraft.forStorage(interaction: Interaction): AnswerDraft =
    if (interaction.secret == true) AnswerDraft() else copy(openedElicitationUrl = null, fields = kotlinx.serialization.json.JsonObject(fields.filterKeys { key ->
        persona.core.domain.answer.ElicitationForm.fields(interaction.elicitation?.requestedSchema).none { it.key == key && it.secret }
    }))

internal enum class InteractionSubmission(@androidx.annotation.StringRes val notice: Int, val withdraw: Boolean) {
    Answered(R.string.chat_interaction_answered, true),
    ResolvedElsewhere(R.string.chat_interaction_resolved_elsewhere, true),
    PromptChanged(R.string.chat_interaction_prompt_changed, true),
    Unknown(R.string.chat_interaction_unknown, false),
    ;

    /** As iOS AppModel.resolve: answered buzzes success, a changed dialog a nudge, an unconfirmed answer an error. */
    val haptic: persona.core.designsystem.Haptic? get() = when (this) {
        Answered -> persona.core.designsystem.Haptic.Success
        PromptChanged -> persona.core.designsystem.Haptic.Attention
        Unknown -> persona.core.designsystem.Haptic.Error
        ResolvedElsewhere -> null
    }
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
