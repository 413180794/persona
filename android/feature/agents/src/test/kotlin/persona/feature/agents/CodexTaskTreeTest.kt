package persona.feature.agents

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Session
import persona.core.domain.sort.*

class CodexTaskTreeTest {
    private fun row(id: String, parent: String? = null, offline: Boolean = false) = AgentSession(Session(
        sessionId = id, machineId = "machine", title = id, sourceKind = "codex_history", sourceThreadId = id,
        parentThreadId = parent, cwd = "/code/app", repoRoot = "/code/app", state = if (offline) "offline" else "working",
        status = if (offline) "offline" else "working"))

    @Test fun childrenFoldByDefaultAndExpandRecursivelyWithoutChangingCounts() {
        val rows = listOf(row("root"), row("child", "root"), row("leaf", "child", true))
        val list = scopedAgentList(rows, emptyList(), AgentsPreferencesSnapshot(), AgentListSettings())
        assertEquals(listOf("root"), agentTreeEntries(list, "").filterIsInstance<TreeRowEntry>().map { it.row.session.sessionId })
        val open = agentTreeEntries(list, "", expandedTasks = setOf("machine/root", "machine/child"))
        assertEquals(listOf("root", "child", "leaf"), open.filterIsInstance<TreeRowEntry>().map { it.row.session.sessionId })
        assertEquals(listOf(1, 2, 3), open.filterIsInstance<TreeRowEntry>().map { it.depth })
        assertEquals(1, list.counts[AgentQuickFilter.Working])
        assertEquals(listOf("root"), agentTreeEntries(list, "").filterIsInstance<TreeRowEntry>().map { it.row.session.sessionId })
    }

    @Test fun allRangeUnifiesLiveAndHistoricalCodexProjectsButKeepsOrdinaryHistory() {
        val rows = listOf(row("root"), row("old", offline = true), row("child", "old", true),
            row("ordinary", offline = true).let { it.copy(session = it.session.copy(sourceKind = null)) })
        val list = scopedAgentList(rows, emptyList(), AgentsPreferencesSnapshot(range = AgentRange.All), AgentListSettings())
        assertEquals(listOf("root", "old"), list.groups.single { it.repo != null }.rows.map { it.session.sessionId })
        assertEquals("ordinary", list.groups.single { it.id.startsWith("history/") }.rows.single().session.sessionId)
        assertEquals(1, list.children.getValue("machine/old").size)
    }

    @Test fun orphanIsVisibleButMachineAndStatusChipsExcludeIt() {
        val orphan = row("orphan", "missing")
        val list = scopedAgentList(listOf(orphan), emptyList(), AgentsPreferencesSnapshot(), AgentListSettings())
        assertEquals("orphan", list.groups.flatMap { it.rows }.single().session.sessionId)
        assertEquals(0, list.counts[AgentQuickFilter.Working])
        assertEquals(0, list.machines.single().sessionCount)
        assertEquals(0, machineCounts(list.machines.map { it.machine }, listOf(orphan)).single().count)
    }
}
