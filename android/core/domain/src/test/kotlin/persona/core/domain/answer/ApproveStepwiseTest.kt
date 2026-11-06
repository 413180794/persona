package persona.core.domain.answer

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.Interaction
import persona.core.contracts.Resolution

/** "Approve, confirm each step" on a conversation's plan: approve in the default mode, never with feedback or while rejecting. */
class ApproveStepwiseTest {
    private val plan = Interaction(id = "example_request", machineId = "example_machine", sessionId = "example_session", kind = "plan")

    private fun build(draft: AnswerDraft = AnswerDraft(), context: AnswerContext = AnswerContext(entry = AnswerEntry.Conversation)) =
        AnswerBuilder.build(plan, AnswerAction.ApproveStepwise, draft, context)

    @Test fun approvesInTheDefaultMode() {
        val result = build()
        assertEquals(Resolution(approve = true, mode = "default"), result.resolution)
    }

    @Test fun feedbackBlocksIt() {
        val result = build(draft = AnswerDraft(feedback = "先补测试"))
        assertFalse(result.allowed); assertNull(result.resolution)
    }

    @Test fun rejectingBlocksIt() {
        val result = build(context = AnswerContext(entry = AnswerEntry.Conversation, rejecting = true))
        assertFalse(result.allowed); assertNull(result.resolution)
    }

    @Test fun aLockedPhoneBlocksIt() {
        val result = build(context = AnswerContext(entry = AnswerEntry.Conversation, locked = true))
        assertFalse(result.allowed); assertNull(result.resolution)
    }
}
