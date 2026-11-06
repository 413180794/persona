package persona.core.domain.sort

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Session

class CodexTaskForestTest {
    private fun row(id: String, parent: String? = null, machine: String = "a", state: String = "working", source: String = "codex_history") =
        AgentSession(Session(sessionId = id, machineId = machine, sourceKind = source, sourceThreadId = id,
            parentThreadId = parent, state = state, cwd = "/code/app", title = id))

    @Test fun hierarchyUsesRealRelationshipsAndDoesNotCrossMachines() {
        val rows = listOf(row("root"), row("child", "root"), row("leaf", "child"), row("orphan", "missing"), row("remote", "root", "b"))
        val tree = codexTaskForest(rows)
        assertEquals(listOf("root", "orphan", "remote"), tree.roots.map { it.session.sessionId })
        assertEquals("leaf", tree.children.getValue("a/child").single().session.sessionId)
        assertEquals(1, buildAgentList(rows).counts[AgentQuickFilter.Working])
    }

    @Test fun filtersUseParentStateAndSearchKeepsMatchedDescendantsReachable() {
        val rows = listOf(row("root", state = "idle"), row("needle", "root"))
        val working = buildAgentList(rows, settings = AgentListSettings(quick = AgentQuickFilter.Working))
        assertTrue(working.groups.isEmpty())
        assertEquals(0, working.counts[AgentQuickFilter.Working])
        val search = buildAgentList(rows, settings = AgentListSettings(query = "needle"))
        assertEquals("root", search.groups.flatMap { it.visibleRows }.single().session.sessionId)
        assertEquals("needle", search.children.getValue("a/root").single().session.sessionId)
    }

    @Test fun explicitSessionIdsAndRootFallbackResolveMissingIntermediateParent() {
        val parent = row("root")
        val child = row("child", "gone").let { it.copy(session = it.session.copy(rootSessionId = "root", isSubagent = true)) }
        assertEquals(listOf(parent), codexTaskForest(listOf(parent, child)).roots)
        val explicit = child.copy(session = child.session.copy(parentSessionId = "root", rootSessionId = null))
        assertEquals(listOf(explicit), codexTaskForest(listOf(parent, explicit)).children["a/root"])
    }

    @Test fun publicRelationshipsStayWithinNamespacedHubMachines() {
        val root = row("root")
        val child = row("child", machine = "a~g12345678").let { it.copy(session = it.session.copy(parentSessionId = "root")) }
        assertEquals(2, codexTaskForest(listOf(root, child)).roots.size)
        val localRoot = row("root", machine = "a~g12345678")
        assertEquals(listOf(child), codexTaskForest(listOf(root, localRoot, child)).children[localRoot.id])
    }

    @Test fun terminalCodexParentOwnsItsHistoricalChild() {
        val parent = row("root").let { it.copy(session = it.session.copy(sourceKind = null, agent = "codex")) }
        val child = row("child", "root").let { it.copy(session = it.session.copy(parentSessionId = "root")) }
        val tree = codexTaskForest(listOf(parent, child))
        assertEquals(listOf(parent), tree.roots)
        assertEquals(listOf(child), tree.children[parent.id])
    }

    @Test fun unresolvedPublicParentDoesNotFallBackToAnotherHomesRawThread() {
        val root = row("root")
        val child = row("child", "root").let { it.copy(session = it.session.copy(parentSessionId = "other-home-root")) }
        assertEquals(2, codexTaskForest(listOf(root, child)).roots.size)
        val otherHome = row("child", "root").let { it.copy(session = it.session.copy(sourceInstanceId = "other-home")) }
        assertEquals(2, codexTaskForest(listOf(root, otherHome)).roots.size)
    }

    @Test fun ordinaryTasksRemainIndependentAndCyclesRemainReachable() {
        val regular = listOf(row("root", source = "runtime"), row("child", "root", source = "runtime"))
        assertEquals(regular, codexTaskForest(regular).roots)
        assertEquals(2, buildAgentList(regular).counts[AgentQuickFilter.Working])
        val cycle = codexTaskForest(listOf(row("a", "b"), row("b", "a")))
        assertEquals(1, cycle.roots.size)
        assertEquals(1, cycle.children.values.flatten().size)
    }

    @Test fun repositoriesUseFullPathAndMachineAndKeepNestedWorktrees() {
        val rows = listOf("/code/app", "/code/app/.claude/worktrees/fix-a/src", "/elsewhere/fix-b").mapIndexed { i, path ->
            row("$i").let { it.copy(session = it.session.copy(cwd = path, repoRoot = "/code/app")) }
        } + row("other").let { it.copy(session = it.session.copy(cwd = "/other/app", repoRoot = "/other/app")) } + row("remote", machine = "b")
        val repos = agentRepos(rows)
        assertEquals(3, repos.size)
        assertEquals(3, repos.single { it.machineId == "a" && it.path == "/code/app" }.worktrees.size)
        assertEquals("/code/app/.claude/worktrees/fix-a", repoOf(rows[1].session.cwd, "/code/app").worktree)
    }
}
