package persona.core.domain.answer

import persona.core.contracts.Interaction
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** "Always allow" is Claude's; another agent's card offers it only with always_label (iOS AgentPermissionForm). */
class AlwaysOfferTest {
    private val card = Interaction("ix", machineId = "m", sessionId = "s", kind = "permission", toolName = "Bash")

    @Test fun claudesCardsOfferItUnderTheirOwnName() {
        assertTrue(card.offersAlways)
        assertTrue(card.copy(agent = "claude").offersAlways)
        assertEquals("本会话总是允许", card.alwaysActionLabel)
    }

    @Test fun otherAgentsNeedTheirTuiToNameWhatItAllows() {
        assertFalse(card.copy(agent = "codex").offersAlways)
        assertFalse(card.copy(agent = "traex", alwaysLabel = " ").offersAlways)
        assertFalse(card.copy(agent = "dsh", agentLabelHint = "DSH").offersAlways)
        val labelled = card.copy(agent = "codex", alwaysLabel = "以后不再问以 curl -s 开头的命令")
        assertTrue(labelled.offersAlways)
        assertEquals("总是同意", labelled.alwaysActionLabel)
        val always = ApprovalPolicy.actions(AnswerEntry.Conversation, "permission", false).single { it.id == ApprovalAction.Always }
        assertTrue(AnswerRequirement.AlwaysOffered in always.requirements)
        assertFalse(AnswerBuilder.build(card.copy(agent = "codex"), AnswerAction.Always).allowed)
        assertTrue(AnswerBuilder.build(labelled, AnswerAction.Always).allowed)
    }
}
