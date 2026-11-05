package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Session.workflows and the new background fields (#2935): optional, tolerant, never decoding a session badly. */
class WorkflowContractTest {
    private val running = """{"task_id":"t1","run_id":"wf_1","name":"parity-audit","status":"running","running":true,
        "phases":["Audit","Verify"],"phase":"Verify","phase_index":2,"agents_started":185,"agents_done":169,"agents_running":16,
        "started_at":"2026-11-05T10:00:00Z","updated_at":"0001-01-01T00:00:00Z",
        "agents":[{"id":"a1","session_id":"s1@a1","label":"audit web","phase":"Audit","running":false,"last_text":"ok"},
                  {"id":"a2","session_id":"s1@a2","running":true,"started_at":"2026-11-05T10:10:00Z"}]}"""

    private fun session(extra: String) = JsonCodec.decodeFromString<Session>(
        """{"machine_id":"m1","session_id":"s1","status":"idle","state":"working"$extra}""")

    @Test fun aRunningWorkflowDecodesWithItsAgents() {
        val s = session(""","background":[{"id":"t1","kind":"workflow","run_id":"wf_1","started_at":"2026-11-05T10:00:00Z"},
            {"id":"m1","kind":"monitor","description":"tail log","expires_at":"2026-11-05T10:30:00Z"}],"workflows":[$running]""")
        val w = s.workflows!!.single()
        assertEquals(WorkflowStatus.Running, w.state)
        assertTrue(w.isRunning)
        assertEquals("parity-audit", w.title)
        assertEquals(2, w.currentPhase)
        assertEquals(185, w.agentsStarted)
        assertNull(w.endedAt)
        assertNull(w.updatedAt) // Go's zero time is unset
        assertEquals("s1@a1", w.agents!!.first().sessionId)
        assertEquals("wf_1", s.background!!.first().runId)
        assertEquals("2026-11-05T10:30:00Z", s.background!![1].expiresAt)
        // the workflow is in background too: counted once
        assertEquals(2, s.backgroundCount)
        assertEquals(listOf("m1"), s.backgroundTasks.map { it.id })
        assertFalse(s.legacyWorkflowRunning)
    }

    @Test fun anOlderHubSendsNoWorkflows() {
        val s = session(""","background":[{"id":"t1","kind":"workflow"},{"id":"b1","kind":"shell"}]""")
        assertNull(s.workflows)
        assertTrue(s.runningWorkflows.isEmpty())
        assertEquals(2, s.backgroundCount)
        assertEquals(2, s.backgroundTasks.size)
        assertTrue(s.legacyWorkflowRunning)
        assertFalse(session("").legacyWorkflowRunning)
    }

    @Test fun anUnknownStatusReadsAsCompletedAndBadFieldsAreDropped() {
        val s = session(""","workflows":[{"task_id":"t2","status":"paused","running":false,"phase_index":5,"phases":["A"],"agents":"bad"},
            {"task_id":"t3","status":"killed","error":"Error: Workflow aborted","summary":"Check the parity","ended_at":"2026-11-05T11:00:00Z"}]""")
        val (paused, killed) = s.workflows!!
        assertEquals(WorkflowStatus.Completed, paused.state)
        assertNull(paused.currentPhase) // 5 is outside its phases
        assertNull(paused.agents)
        assertNull(paused.title)
        assertEquals(WorkflowStatus.Killed, killed.state)
        assertEquals("Check the parity", killed.title)
        assertEquals(0, s.backgroundCount)
        // a missing status is not running either
        assertEquals(WorkflowStatus.Completed, session(""","workflows":[{"task_id":"t4"}]""").workflows!!.single().state)
    }
}
