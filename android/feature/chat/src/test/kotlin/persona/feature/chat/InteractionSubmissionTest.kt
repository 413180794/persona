package persona.feature.chat

import persona.core.contracts.Interaction
import persona.core.contracts.api.InteractionGoneException
import persona.core.contracts.api.InteractionPromptChangedException
import persona.core.contracts.Resolution
import persona.core.contracts.api.ResolveOutcome
import persona.core.domain.answer.AnswerAction
import persona.core.domain.answer.AnswerBuilder
import persona.core.domain.answer.AnswerDraft
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

class InteractionSubmissionTest {
    @Test fun terminalOutcomesWithdrawStaleCardsAndOnlyLostReceiptsStayUncertain() = runTest {
        assertEquals(InteractionSubmission.Answered, submitInteraction { ResolveOutcome() }.submission)
        val gone = submitInteraction { throw InteractionGoneException() }
        assertEquals(R.string.chat_interaction_resolved_elsewhere, gone.submission.notice)
        assertTrue(gone.withdraw)
        val expired = submitInteraction { throw InteractionGoneException("expired") }
        assertEquals(InteractionReply(InteractionSubmission.ResolvedElsewhere, endedStatus = "expired"), expired)
        val already = submitInteraction { ResolveOutcome(already = true, resolution = Resolution(allow = false)) }
        assertEquals(InteractionReply(InteractionSubmission.AlreadyAnswered, effective = Resolution(allow = false)), already)
        assertTrue(already.withdraw)
        val changed = submitInteraction { throw InteractionPromptChangedException() }
        assertEquals(InteractionSubmission.PromptChanged, changed.submission)
        assertTrue(changed.withdraw)
        assertFalse(submitInteraction { throw IllegalStateException() }.withdraw)
        try {
            submitInteraction { throw CancellationException() }
            fail("Cancellation must propagate")
        } catch (_: CancellationException) { }
    }

    // As iOS AppModel.resolve: answered succeeds, a changed dialog nudges, gone plays nothing, unconfirmed is an error.
    @Test fun eachOutcomePlaysWhatIosPlays() {
        assertEquals(mapOf(
            InteractionSubmission.Answered to persona.core.designsystem.Haptic.Success,
            // iOS plays success for a card the hub says was answered already, too.
            InteractionSubmission.AlreadyAnswered to persona.core.designsystem.Haptic.Success,
            InteractionSubmission.ResolvedElsewhere to null,
            InteractionSubmission.PromptChanged to persona.core.designsystem.Haptic.Attention,
            InteractionSubmission.Unknown to persona.core.designsystem.Haptic.Error,
        ), InteractionSubmission.entries.associateWith { it.haptic })
    }

    @Test fun passwordPreservesWhitespaceOnTheWireButNeverEntersSavedDrafts() {
        val request = Interaction("request", kind = "screen", secret = true)
        val draft = AnswerDraft(text = "  sample input  ", other = mapOf("question" to "sample input"))
        val result = AnswerBuilder.build(request, AnswerAction.TerminalText, draft)
        assertTrue(result.allowed)
        assertEquals(draft.text, result.resolution?.text)
        assertEquals(listOf("enter"), result.resolution?.keys)
        assertEquals(AnswerDraft(), result.draft.forStorage(request))
        assertEquals(draft, draft.forStorage(request.copy(secret = false)))
    }
}
