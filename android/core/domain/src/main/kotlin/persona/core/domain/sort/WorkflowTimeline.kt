package persona.core.domain.sort

import persona.core.contracts.CoreText
import persona.core.contracts.CoreText.Key
import persona.core.contracts.Workflow
import persona.core.contracts.WorkflowAgent
import persona.core.contracts.WorkflowPhaseStats
import persona.core.contracts.WorkflowStatus
import persona.core.contracts.parseTime
import java.time.Duration
import java.time.Instant

/**
 * A Workflow run as its timeline card draws it (#3435; the web's `lib/workflows.ts` from #3434 has the same rules): the
 * counts in its head ("169/185+"), where it is now, the progress bar's parts, and one block per phase with its agents
 * in the order they started, at most [PHASE_ROWS] of them with the quiet rest folded into "n more". Fields an older hub
 * does not send (phase_stats, agents_failed, an agent's status) are read when they are there; without them it says less.
 */
object WorkflowTimeline {
    /** At most this many agents are rows in a block; the quiet ones after them fold into "n more". */
    const val PHASE_ROWS = 12

    /** "169/185+": done of those started so far, "+" while it runs (the script starts more as it goes). */
    fun counts(workflow: Workflow): String = "${workflow.agentsDone}/${workflow.agentsStarted}" + if (workflow.isRunning) "+" else ""

    /** What one of its agents is doing: running, back with its result, failed, or stopped when the run ended without it. */
    enum class AgentState { Running, Done, Failed, Stopped }

    fun agentState(agent: WorkflowAgent): AgentState = when {
        agent.running -> AgentState.Running
        agent.status == "failed" -> AgentState.Failed
        agent.status == "stopped" -> AgentState.Stopped
        else -> AgentState.Done
    }

    /** How long an agent has run (or ran); null when the hub does not say when it started or ended. */
    fun agentSeconds(agent: WorkflowAgent, now: Instant): Long? {
        val start = parseTime(agent.startedAt) ?: return null
        val end = if (agent.running) now else parseTime(agent.endedAt) ?: parseTime(agent.updatedAt) ?: return null
        return Duration.between(start, end).seconds.coerceAtLeast(0)
    }

    /** What its latest tool call does ("Reading example.go"), else the tool's name; null when the hub does not say. */
    fun agentTool(agent: WorkflowAgent): String? = agent.latestTool?.let { tool ->
        tool.summary?.trim()?.takeIf(String::isNotEmpty) ?: tool.name.trim().takeIf(String::isNotEmpty)
    }

    /** A duration as the card says it: "15 s", "14 min", "2 h", "2 h 5 min". */
    fun took(seconds: Long): CoreText = when {
        seconds < 60 -> CoreText.of(Key.DurationSeconds, seconds)
        seconds < 3600 -> CoreText.of(Key.DurationMinutes, seconds / 60)
        seconds % 3600 / 60 == 0L -> CoreText.of(Key.DurationHours, seconds / 3600)
        else -> CoreText.of(Key.DurationHoursMinutes, seconds / 3600, seconds % 3600 / 60)
    }

    /** Where a block stands: all its agents back, some running (or the run is in it), none started yet, or the run ended with some not back. */
    enum class PhaseState { Done, Current, Next, Stopped }

    /** One phase of a run as its card draws it. */
    data class PhaseBlock(
        /** The phase as the script names it ("": the agents whose phase is not known). */
        val title: String,
        val started: Int,
        val done: Int,
        val running: Int,
        val failed: Int,
        /** Its agents the hub listed (the run's newest 60), in the order they started. */
        val agents: List<WorkflowAgent>,
        val state: PhaseState,
        /** The run is in this phase now: its block is lit. */
        val current: Boolean,
        /** More may still start in it: its count reads "5/8+". */
        val growing: Boolean,
    ) {
        /** "5/8+": done of started, "+" while more may start in it. */
        val count: String get() = "$done/$started" + if (growing) "+" else ""

        /**
         * Open unless the reader chose: while the run is in it or any of its agents runs (so it does not fold between one
         * agent and the next), folded once it is behind the run with all back.
         */
        val opens: Boolean get() = running > 0 || current
    }

    /**
     * Its phases, one block each, in the script's order, then undeclared ones, then agents without a phase. Counts come
     * from phase_stats (all agents, before the hub's 60-agent cap); an older hub's are counted from the agents it lists.
     */
    fun blocks(workflow: Workflow): List<PhaseBlock> {
        val agents = workflow.agents.orEmpty()
        val stats = workflow.phaseStats?.takeIf { it.isNotEmpty() } ?: statsFromAgents(workflow, agents)
        // A run that names no phases and has no agents: nothing to draw.
        if (stats.size == 1 && stats[0].title.isEmpty() && stats[0].started == 0) return emptyList()
        val running = workflow.isRunning
        val phase = workflow.phase.orEmpty()
        // Where the run is now: the phase of its newest running agent, else the first block with an agent running.
        val currentAt = when {
            phase.isNotEmpty() && stats.any { it.title == phase } -> stats.indexOfFirst { it.title == phase }
            running -> stats.indexOfFirst { it.running > 0 }
            else -> -1
        }
        return stats.mapIndexed { i, stat ->
            val current = running && i == currentAt
            val state = when {
                stat.running > 0 || current -> PhaseState.Current
                // None started, as far as the hub says: passed when the run went beyond it; the phase an ended run
                // stopped in (a hub that lists neither its agents nor phase_stats) did run, done or not.
                stat.started == 0 -> when {
                    currentAt >= 0 && i < currentAt -> PhaseState.Done
                    i == currentAt && !running -> if (workflow.state == WorkflowStatus.Completed) PhaseState.Done else PhaseState.Stopped
                    else -> PhaseState.Next
                }
                stat.done + stat.failed >= stat.started -> PhaseState.Done
                running -> PhaseState.Done
                else -> PhaseState.Stopped
            }
            PhaseBlock(
                title = stat.title, started = stat.started, done = stat.done, running = if (running) stat.running else 0,
                failed = stat.failed,
                agents = agents.filter { a ->
                    a.phase.orEmpty() == stat.title || (stat.title.isEmpty() && stats.none { it.title == a.phase })
                },
                state = state, current = current, growing = running && (stat.running > 0 || current),
            )
        }
    }

    /**
     * Counts from the agents listed, for a hub without phase_stats: declared phases first, then any other an agent
     * names, then the agents without one. They undercount once a run has more agents than the hub lists.
     */
    private fun statsFromAgents(workflow: Workflow, agents: List<WorkflowAgent>): List<WorkflowPhaseStats> {
        val titles = workflow.phases.orEmpty().toMutableList()
        for (agent in agents) if (agent.phase.orEmpty() !in titles) titles += agent.phase.orEmpty()
        return titles.map { title ->
            val states = agents.filter { it.phase.orEmpty() == title }.map(::agentState)
            WorkflowPhaseStats(title, started = states.size, done = states.count { it == AgentState.Done },
                failed = states.count { it == AgentState.Failed }, running = states.count { it == AgentState.Running })
        }
    }

    /**
     * How a block's rows stand: [rows] in start order; [folded] the listed agents not shown ("n more" opens them all),
     * the first of them where [breakAt] rows have been shown; [unlisted] the block's earliest agents, which the hub no
     * longer lists (it keeps the run's newest 60), said above its first row.
     */
    data class PhaseRows(val rows: List<WorkflowAgent>, val folded: Int, val breakAt: Int, val unlisted: Int)

    /**
     * The rows a block shows, in the order its agents started (never reordered, so nothing jumps as they finish), running
     * ones always. Normally its first [limit]: the quiet tail folds. Once the hub's cap has cut into the block (it drops
     * the run's oldest agent each time one starts anywhere in the run), its last [limit] instead: anchored to the end,
     * the rows stay put as the cap moves on, and all that is hidden comes before them. [all]: "n more" was opened.
     */
    fun rows(block: PhaseBlock, all: Boolean = false, limit: Int = PHASE_ROWS): PhaseRows {
        val agents = block.agents
        val unlisted = (block.started - agents.size).coerceAtLeast(0)
        if (all) return PhaseRows(agents, 0, agents.size, unlisted)
        val from = if (unlisted > 0) (agents.size - limit).coerceAtLeast(0) else 0
        fun shown(index: Int) = agents[index].running || index in from until from + limit
        val rows = agents.filterIndexed { index, _ -> shown(index) }
        val firstHidden = agents.indices.firstOrNull { !shown(it) }
        val breakAt = if (firstHidden == null) rows.size else (0 until firstHidden).count(::shown)
        return PhaseRows(rows, agents.size - rows.size, breakAt, unlisted)
    }

    /** Where a running run is now: its phase ("" when not known) and its newest agent running there, else its newest running one. */
    data class Now(val phase: String, val agent: WorkflowAgent?)

    /** Null once it has ended, or when the hub says neither its phase nor a running agent. */
    fun now(workflow: Workflow): Now? {
        if (!workflow.isRunning) return null
        val newestFirst = workflow.agents.orEmpty().asReversed()
        val phase = workflow.phase.orEmpty()
        val lead = newestFirst.firstOrNull { it.running && (phase.isEmpty() || it.phase == phase) } ?: newestFirst.firstOrNull { it.running }
        return if (phase.isNotEmpty() || lead != null) Now(phase, lead) else null
    }

    /** The progress bar's parts, in order. */
    enum class Part { Done, Running, Failed, Stopped }

    /** Its agents by how they stand: back, still running, failed, and stopped by its end (only once it ended). */
    data class Tally(val started: Int, val done: Int, val running: Int, val failed: Int, val stopped: Int) {
        fun of(part: Part): Int = when (part) {
            Part.Done -> done
            Part.Running -> running
            Part.Failed -> failed
            Part.Stopped -> stopped
        }
    }

    fun tally(workflow: Workflow): Tally {
        val started = workflow.agentsStarted.coerceAtLeast(0)
        val done = workflow.agentsDone.coerceIn(0, started)
        val running = if (workflow.isRunning) workflow.agentsRunning.coerceIn(0, started - done) else 0
        val failed = workflow.agentsFailed.coerceIn(0, started - done - running)
        // The ones not back when it ended; while it runs the rest is not known yet (a hub before #5511 counts failures as done).
        val stopped = if (workflow.isRunning) 0 else started - done - running - failed
        return Tally(started, done, running, failed, stopped)
    }

    /** A part of the bar: where it starts and how much of the bar it is (0…1). */
    data class Segment(val part: Part, val from: Float, val size: Float)

    fun segments(tally: Tally): List<Segment> {
        var at = 0f
        return Part.entries.map { part ->
            val size = if (tally.started > 0) tally.of(part).toFloat() / tally.started else 0f
            Segment(part, at, size).also { at += size }
        }
    }
}
