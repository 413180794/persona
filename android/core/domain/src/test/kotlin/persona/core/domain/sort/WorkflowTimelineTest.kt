package persona.core.domain.sort

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Workflow
import persona.core.contracts.WorkflowAgent
import persona.core.contracts.WorkflowPhaseStats
import persona.core.contracts.WorkflowTool
import persona.core.domain.sort.WorkflowTimeline.AgentState
import persona.core.domain.sort.WorkflowTimeline.Part
import persona.core.domain.sort.WorkflowTimeline.PhaseState
import persona.core.domain.sort.WorkflowTimeline.Tally
import persona.core.domain.zh
import java.time.Instant

/**
 * The run's timeline card (#3435): counts with "+", one block per phase, rows in start order folded past 12, the bar's
 * parts. The same cases as the web's `workflows.test.ts` (#3434), so the clients draw a run alike.
 */
class WorkflowTimelineTest {
    private val start = Instant.parse("2026-11-05T13:46:57Z")
    private val run = Workflow(taskId = "wdp5zxh37", runId = "wf_c1d9daf8-cd2", name = "parity-audit", status = "running", running = true,
        phases = listOf("Audit", "Verify"), phase = "Verify", phaseIndex = 2, agentsStarted = 185, agentsDone = 169, agentsRunning = 16,
        startedAt = start.toString(), agents = listOf(
            WorkflowAgent("a1", "s1@a1", "audit:chat", "Audit", running = false, lastText = "found 2 gaps"),
            WorkflowAgent("a2", "s1@a2", "verify:chat#0", "Verify", running = true),
            WorkflowAgent("a3", "s1@a3", "verify:chat#1", "Verify", running = false),
        ))

    private fun agent(i: Int, phase: String, running: Boolean = false, status: String = if (running) "running" else "done") =
        WorkflowAgent("a$i", "s1@a$i", "${phase.lowercase()}:$i", phase, running = running, status = status,
            startedAt = start.plusSeconds(i.toLong()).toString(), endedAt = if (running) null else start.plusSeconds(i + 90L).toString())

    private val stats = run.copy(phases = listOf("Audit", "Verify", "Report"), phaseStats = listOf(
        WorkflowPhaseStats("Audit", started = 180, done = 169, running = 11),
        WorkflowPhaseStats("Verify", started = 5, done = 0, running = 5),
        WorkflowPhaseStats("Report"),
    ), agents = listOf(agent(1, "Audit"), agent(2, "Audit", running = true), agent(3, "Verify", running = true)))

    private fun ids(agents: List<WorkflowAgent>) = agents.map { it.id }

    @Test fun countsDoneOfStartedWithAPlusOnlyWhileItRuns() {
        assertEquals("169/185+", WorkflowTimeline.counts(run))
        assertEquals("169/185", WorkflowTimeline.counts(run.copy(status = "completed", running = false)))
        assertEquals("0/0+", WorkflowTimeline.counts(Workflow(taskId = "x", status = "running", running = true)))
    }

    @Test fun drawsOneBlockPerPhaseFromPhaseStatsInTheirOrderTheRunsPhaseLit() {
        val blocks = WorkflowTimeline.blocks(stats)
        assertEquals(listOf(
            listOf("Audit", PhaseState.Current, false, "169/180+"),
            listOf("Verify", PhaseState.Current, true, "0/5+"),
            listOf("Report", PhaseState.Next, false, "0/0"),
        ), blocks.map { listOf(it.title, it.state, it.current, it.count) })
        assertEquals(listOf("a1", "a2"), ids(blocks[0].agents))
        assertEquals(listOf("a3"), ids(blocks[1].agents))
        // open while the run is in it or any of its agents runs, folded otherwise
        assertEquals(listOf(true, true, false), blocks.map { it.opens })
    }

    @Test fun dropsThePlusOnceTheRunEndsAndMarksPhasesThatDidNotFinish() {
        val ended = stats.copy(status = "killed", running = false, phaseStats = listOf(
            WorkflowPhaseStats("Audit", started = 4, done = 4), WorkflowPhaseStats("Verify", started = 3, done = 1)))
        assertEquals(listOf(listOf(PhaseState.Done, "4/4", false), listOf(PhaseState.Stopped, "1/3", false)),
            WorkflowTimeline.blocks(ended).map { listOf(it.state, it.count, it.current) })
        assertEquals(listOf(false, false), WorkflowTimeline.blocks(ended).map { it.opens })
    }

    @Test fun countsFailuresApartWhenTheHubSendsThemAndReadsWithoutThem() {
        val failed = stats.copy(phase = "Audit", phaseStats = listOf(WorkflowPhaseStats("Audit", started = 6, done = 4, failed = 2)))
        val block = WorkflowTimeline.blocks(failed).single()
        assertEquals(2, block.failed)
        assertEquals(PhaseState.Current, block.state) // the run is in it
        assertEquals(PhaseState.Done, WorkflowTimeline.blocks(failed.copy(phase = null)).single().state) // all back, two failed
    }

    @Test fun derivesBlocksFromTheAgentsOnAHubWithoutPhaseStats() {
        assertEquals(listOf(listOf("Audit", 1, 1, 0, PhaseState.Done), listOf("Verify", 2, 1, 1, PhaseState.Current)),
            WorkflowTimeline.blocks(run).map { listOf(it.title, it.started, it.done, it.running, it.state) })
        // no phases at all: one block of its agents under no name; no agents either: nothing
        val plain = Workflow(taskId = "p", status = "running", running = true, agents = listOf(agent(1, ""), agent(2, "", running = true)))
        val only = WorkflowTimeline.blocks(plain).single()
        assertEquals(listOf("", 2, 2), listOf(only.title, only.started, only.agents.size))
        assertEquals(emptyList<WorkflowTimeline.PhaseBlock>(), WorkflowTimeline.blocks(Workflow(taskId = "e", status = "running", running = true)))
        // an ended run the hub lists no agents of: the phases before where it stopped passed, that one stopped
        val killed = run.copy(status = "killed", running = false, phases = listOf("Scan", "Fix", "Check"), phase = "Fix", agents = emptyList())
        assertEquals(listOf(PhaseState.Done, PhaseState.Stopped, PhaseState.Next), WorkflowTimeline.blocks(killed).map { it.state })
        // ...or the phase a completed one ended in: done
        assertEquals(PhaseState.Done, WorkflowTimeline.blocks(killed.copy(status = "completed"))[1].state)
    }

    @Test fun keepsRowsInStartOrderAtMostTwelveTheQuietTailFoldedRunningOnesNever() {
        val agents = (1..20).map { agent(it, "Audit", running = it == 16) }
        val block = WorkflowTimeline.blocks(run.copy(phase = "Audit", agents = agents,
            phaseStats = listOf(WorkflowPhaseStats("Audit", started = 20, done = 19, running = 1)))).single()
        val rows = WorkflowTimeline.rows(block)
        assertEquals((1..12).map { "a$it" } + "a16", ids(rows.rows))
        assertEquals(7, rows.folded) // a13–a15, a17–a20: what "n more" opens
        assertEquals(12, rows.breakAt) // the first of them comes after a12
        assertEquals(0, rows.unlisted)
        val open = WorkflowTimeline.rows(block, all = true)
        assertEquals(20, open.rows.size)
        assertEquals(0, open.folded)
        // a block of 12 or fewer shows them all
        val small = WorkflowTimeline.rows(block.copy(agents = agents.take(12)))
        assertEquals(listOf(12, 0, 12), listOf(small.rows.size, small.folded, small.breakAt))
    }

    @Test fun anchorsABlockTheHubsCapCutIntoToItsEndSoTheCapMovingOnShiftsNothing() {
        // Audit started 180, the hub lists #126–180 (its newest 60 are these and 5 more elsewhere); #170 on still run
        fun listed(from: Int) = (from..180).map { agent(it, "Audit", running = it >= 170) }
        fun block(from: Int) = WorkflowTimeline.blocks(run.copy(phase = "Verify", agents = listed(from),
            phaseStats = listOf(WorkflowPhaseStats("Audit", started = 180, done = 169, running = 11)))).first()
        val before = WorkflowTimeline.rows(block(126))
        assertEquals((169..180).map { "a$it" }, ids(before.rows))
        assertEquals(listOf(43, 0, 125), listOf(before.folded, before.breakAt, before.unlisted)) // all that is hidden comes first
        // an agent starts elsewhere in the run: the hub drops #126; the rows are the same
        val after = WorkflowTimeline.rows(block(127))
        assertEquals(ids(before.rows), ids(after.rows))
        assertEquals(listOf(42, 126), listOf(after.folded, after.unlisted))
        // a running one before the window stays, the break after it
        val early = WorkflowTimeline.blocks(run.copy(agents = (11..30).map { agent(it, "Audit", running = it == 11 || it == 30) },
            phaseStats = listOf(WorkflowPhaseStats("Audit", started = 30, done = 28, running = 2)))).first()
        val e = WorkflowTimeline.rows(early)
        assertEquals(listOf("a11") + (19..30).map { "a$it" }, ids(e.rows))
        assertEquals(listOf(7, 1, 10), listOf(e.folded, e.breakAt, e.unlisted))
    }

    @Test fun splitsTheBarIntoDoneRunningFailedAndStoppedStoppedOnlyOnceItEnded() {
        assertEquals(Tally(185, 169, 16, 0, 0), WorkflowTimeline.tally(run))
        val killed = run.copy(status = "killed", running = false, agentsRunning = 3, agentsFailed = 2)
        assertEquals(Tally(185, 169, 0, 2, 14), WorkflowTimeline.tally(killed))
        val parts = WorkflowTimeline.segments(WorkflowTimeline.tally(killed))
        assertEquals(listOf(Part.Done, Part.Running, Part.Failed, Part.Stopped), parts.map { it.part })
        assertEquals(169f / 185, parts[2].from, 1e-5f)
        assertEquals(1f, parts.sumOf { it.size.toDouble() }.toFloat(), 1e-5f)
        assertTrue(WorkflowTimeline.segments(WorkflowTimeline.tally(Workflow(taskId = "z", status = "running", running = true))).all { it.size == 0f })
        // counts the hub sends out of range are held to what started
        assertEquals(Tally(3, 3, 0, 0, 0), WorkflowTimeline.tally(Workflow(taskId = "o", status = "running", running = true,
            agentsStarted = 3, agentsDone = 5, agentsRunning = 2, agentsFailed = 1)))
    }

    @Test fun saysWhereItIsNowThePhaseAndItsNewestRunningAgent() {
        assertEquals(WorkflowTimeline.Now("Verify", run.agents!![1]), WorkflowTimeline.now(run))
        assertNull(WorkflowTimeline.now(run.copy(status = "completed", running = false)))
        assertNull(WorkflowTimeline.now(run.copy(phase = null, agents = emptyList())))
        // no phase named: the newest running agent alone
        assertEquals(WorkflowTimeline.Now("", run.agents!![1]), WorkflowTimeline.now(run.copy(phase = null)))
    }

    @Test fun readsAnAgentsStateTimeAndTool() {
        val a = agent(1, "Audit").copy(model = "claude-example", latestTool = WorkflowTool(name = "Read", summary = "Reading example.go"))
        assertEquals(AgentState.Done, WorkflowTimeline.agentState(a))
        assertEquals(AgentState.Stopped, WorkflowTimeline.agentState(a.copy(status = "stopped")))
        assertEquals(AgentState.Failed, WorkflowTimeline.agentState(a.copy(status = "failed")))
        assertEquals(AgentState.Running, WorkflowTimeline.agentState(a.copy(running = true)))
        assertEquals(AgentState.Done, WorkflowTimeline.agentState(a.copy(status = null))) // an older hub
        assertEquals("1 分", WorkflowTimeline.agentSeconds(a, start)?.let(WorkflowTimeline::took)?.zh())
        assertEquals("30 秒", WorkflowTimeline.agentSeconds(a.copy(running = true), start.plusSeconds(31))?.let(WorkflowTimeline::took)?.zh())
        assertNull(WorkflowTimeline.agentSeconds(a.copy(startedAt = null), start))
        assertNull(WorkflowTimeline.agentSeconds(a.copy(endedAt = null, updatedAt = null), start)) // ended, but not saying when
        assertEquals("Reading example.go", WorkflowTimeline.agentTool(a))
        assertEquals("Grep", WorkflowTimeline.agentTool(a.copy(latestTool = WorkflowTool(name = "Grep"))))
        assertNull(WorkflowTimeline.agentTool(a.copy(latestTool = null)))
    }

    @Test fun saysHowLongInTheLargestUnitThatFits() {
        assertEquals(listOf("59 秒", "1 分", "59 分", "2 小时", "2 小时 5 分"),
            listOf(59L, 60L, 3599L, 7200L, 7500L).map { WorkflowTimeline.took(it).zh() })
    }
}
