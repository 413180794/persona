package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test

class ChatCompletionFeedbackTest {
    @Test fun feedbackRequiresVisibleCompletionAndCannotReplayOnResumeOrRefresh() {
        val gate = CompletionFeedbackGate()
        assertFalse(gate.update("cached", "done", true, null))
        assertFalse(gate.update("first", "working", true, null))
        assertTrue(gate.update("first", "done", true, null))
        assertFalse(gate.update("first", "idle", true, "first"))
        assertFalse(gate.update("first", "working", true, "first"))
        assertFalse(gate.update("first", "done", true, "first"))
        assertFalse(gate.update("background", "working", true, "first"))
        assertFalse(gate.update("background", "working", false, "first"))
        assertFalse(gate.update("background", "done", false, "first"))
        assertFalse(gate.update("background", "done", true, "first"))
        assertFalse(gate.update("covered", "working", true, "first"))
        assertFalse(gate.update("covered", "done", false, "first"))
        assertFalse(gate.update("covered", "done", true, "first"))
        assertFalse(gate.update("next", "working", true, "first"))
        assertFalse(gate.update("next", "blocked", true, "first"))
        assertFalse(gate.update("next", "done", true, "first"))
        assertFalse(gate.update("last", "working", true, "first"))
        assertTrue(gate.update("last", "idle", true, "first"))
    }
}
