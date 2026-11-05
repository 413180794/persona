package persona.feature.chat

import persona.core.contracts.Interaction
import persona.core.contracts.api.InteractionGoneException
import persona.core.contracts.api.InteractionPromptChangedException
import persona.core.domain.answer.AnswerAction
import persona.core.domain.answer.AnswerBuilder
import persona.core.domain.answer.AnswerDraft
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

class InteractionSubmissionTest {
    @Test fun terminalOutcomesWithdrawStaleCardsAndOnlyLostReceiptsStayUncertain() = runTest {
        assertEquals(InteractionSubmission.Answered, submitInteraction { })
        val gone = submitInteraction { throw InteractionGoneException() }
        assertEquals(R.string.chat_interaction_resolved_elsewhere, gone.notice)
        assertTrue(gone.withdraw)
        val changed = submitInteraction { throw InteractionPromptChangedException() }
        assertEquals(InteractionSubmission.PromptChanged, changed)
        assertTrue(changed.withdraw)
        assertFalse(submitInteraction { throw IllegalStateException() }.withdraw)
        try {
            submitInteraction { throw CancellationException() }
            fail("Cancellation must propagate")
        } catch (_: CancellationException) { }
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
