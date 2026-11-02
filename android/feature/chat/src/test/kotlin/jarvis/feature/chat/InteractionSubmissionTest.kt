package jarvis.feature.chat

import jarvis.core.contracts.Interaction
import jarvis.core.contracts.api.InteractionGoneException
import jarvis.core.contracts.api.InteractionPromptChangedException
import jarvis.core.domain.answer.AnswerAction
import jarvis.core.domain.answer.AnswerBuilder
import jarvis.core.domain.answer.AnswerDraft
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

class InteractionSubmissionTest {
    @Test fun terminalOutcomesWithdrawStaleCardsAndOnlyLostReceiptsStayUncertain() = runTest {
        assertEquals(InteractionSubmission.Answered, submitInteraction { })
        val gone = submitInteraction { throw InteractionGoneException() }
        assertEquals("已在电脑上处理", gone.notice)
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
