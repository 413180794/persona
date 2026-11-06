package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*

class SessionDetailTest {
    @Test fun summaryDoesNotEraseOpenChatDetail() {
        val held = Session("demo", "task", launcher = "example", permissionMode = "plan", effort = "high",
            model = "example-model", queued = listOf("next"), efforts = listOf("low", "high"))
        val summary = Session("demo", "task", title = "Updated", state = "working", waiting = false)
        val merged = sessionWithDetail(summary, held, null)
        assertEquals("Updated", merged.title)
        assertEquals("working", merged.state)
        assertEquals(held.launcher, merged.launcher)
        assertEquals(held.permissionMode, merged.permissionMode)
        assertEquals(held.queued, merged.queued)
        assertEquals(held.efforts, merged.efforts)
    }
    @Test fun infoHydratesOneSummaryAndDoesNotCrossSessionKeys() {
        val summary = Session("demo", "task", waiting = false)
        val old = Session("demo", "different", queued = listOf("unrelated"))
        val merged = sessionWithDetail(summary, old, SessionInfo(launcher = "example", permissionMode = "plan", model = "new-model"))
        assertEquals("example", merged.launcher)
        assertEquals("plan", merged.permissionMode)
        assertEquals("new-model", merged.model)
        assertNull(merged.queued)
    }
    @Test fun legacyCompleteRowCanClearOldDetail() {
        val old = Session("demo", "task", queued = listOf("old"))
        val complete = Session("demo", "task", queued = emptyList())
        assertEquals(complete, sessionWithDetail(complete, old, null))
    }
}
