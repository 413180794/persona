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

    /** #3731 / #5511: the timeline card's fields (phase counts, the launching call, failed agents, each agent's details). */
    @Test fun theCardsFieldsDecodeAndOlderHubsLeaveThemOut() {
        val s = session(""","workflows":[{"task_id":"t1","status":"failed","running":false,"phase_count":2,"tool_use_id":"toolu_1",
            "phase_stats":[{"title":"Snapshot","started":6,"done":4,"failed":2,"running":0},{"title":"Compare","started":0,"done":0,"running":0}],
            "agents_started":6,"agents_done":4,"agents_failed":2,"agents_running":0,"tokens":81000,"calls":27,
            "agents":[{"id":"f4","session_id":"s1@f4","label":"snapshot:dark-large","phase":"Snapshot","running":false,"status":"failed",
                "started_at":"2026-11-05T10:00:00Z","ended_at":"2026-11-05T10:07:00Z","model":"claude-example",
                "latest_tool":{"id":"toolu_9","name":"Bash","summary":"Running the snapshots"},"tokens":12000,"calls":3},
                {"id":"f5","session_id":"s1@f5","running":false,"status":"paused"}]}]""")
        val w = s.workflows!!.single()
        assertEquals("toolu_1", w.toolUseId)
        assertEquals(2, w.phaseCount)
        assertEquals(listOf(WorkflowPhaseStats("Snapshot", 6, 4, 2, 0), WorkflowPhaseStats("Compare")), w.phaseStats)
        assertEquals(2, w.agentsFailed)
        assertEquals(81000L, w.tokens)
        assertEquals(27, w.calls)
        val (failed, unknown) = w.agents!!
        assertEquals("failed", failed.status)
        assertEquals("2026-11-05T10:07:00Z", failed.endedAt)
        assertEquals("claude-example", failed.model)
        assertEquals(WorkflowTool("toolu_9", "Bash", "Running the snapshots"), failed.latestTool)
        assertEquals(12000L, failed.tokens)
        assertEquals(3, failed.calls)
        assertEquals("unknown", unknown.status) // a status this app does not know
        // an older hub: none of them, nothing else lost
        val old = JsonCodec.decodeFromString<Workflow>(running)
        assertNull(old.toolUseId)
        assertNull(old.phaseStats)
        assertEquals(0, old.agentsFailed)
        assertNull(old.agents!!.first().status)
        assertNull(old.agents.first().latestTool)
        // a malformed phase_stats drops that field only
        val bad = JsonCodec.decodeFromString<Workflow>("""{"task_id":"t2","status":"running","running":true,"phase_stats":"bad","agents_started":3}""")
        assertNull(bad.phaseStats)
        assertEquals(3, bad.agentsStarted)
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
