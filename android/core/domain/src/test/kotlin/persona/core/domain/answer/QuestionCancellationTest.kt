package persona.core.domain.answer

import kotlinx.serialization.encodeToString
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*

class QuestionCancellationTest {
    private val question = Interaction("q", kind = "question", canCancel = true,
        questions = listOf(Question("Choose", options = listOf(QuestionOption("First")))))

    @Test fun cancellationDropsPicksAndTextWithoutAnAnswerOnTheWire() {
        val result = AnswerBuilder.build(question, AnswerAction.CancelQuestion,
            AnswerDraft(picks = mapOf("Choose" to setOf("First")), other = mapOf("Choose" to "Draft")))
        assertTrue(result.allowed)
        assertEquals(Resolution(cancel = true), result.resolution)
        assertEquals("""{"cancel":true}""", JsonCodec.encodeToString(result.resolution!!))
    }

    @Test fun cancellationRequiresQuestionCapabilityAndUnlockedPendingConversation() {
        for (kind in listOf("permission", "plan", "screen", "elicitation", "unknown")) {
            assertFalse(AnswerBuilder.build(question.copy(kind = kind), AnswerAction.CancelQuestion).allowed)
        }
        for (capability in listOf(null, false)) {
            assertFalse(AnswerBuilder.build(question.copy(canCancel = capability), AnswerAction.CancelQuestion).allowed)
        }
        for (context in listOf(AnswerContext(sending = true), AnswerContext(locked = true), AnswerContext(state = "unknown"),
            AnswerContext(state = "cancelled")) + AnswerEntry.entries.filter { it != AnswerEntry.Conversation }.map { AnswerContext(entry = it) }) {
            assertFalse(AnswerBuilder.build(question, AnswerAction.CancelQuestion, context = context).allowed)
        }
    }

    @Test fun answeringAndHandingBackRetainTheirExistingSemantics() {
        assertEquals(mapOf("Choose" to "First"), AnswerBuilder.build(question, AnswerAction.PickOption).resolution?.answers)
        assertEquals(Resolution(dismiss = true), AnswerBuilder.build(question, AnswerAction.Dismiss).resolution)
        assertEquals(Resolution(allow = true), AnswerBuilder.build(question.copy(kind = "permission"), AnswerAction.Allow).resolution)
    }
}
