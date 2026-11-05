package persona.core.domain.sort

import persona.core.domain.zhAny

import persona.core.contracts.AgentState
import persona.core.contracts.Machine
import persona.core.contracts.Session
import org.junit.Assert.*
import org.junit.Test

/** Mirrors web/src/lib/agent-repos.test.ts, plus how 按仓库 folds on Android. */
class AgentReposTest {
    private fun session(id: String, cwd: String, state: String = "idle", machine: String = "machine-a", hour: Int = 1) =
        AgentSession(Session(machineId = machine, sessionId = id, title = id, cwd = cwd, state = state,
            updatedAt = "2026-01-01T0$hour:00:00Z"))

    private val machines = listOf(Machine("machine-b", "电脑 B", online = true), Machine("machine-a", "电脑 A", online = true))
    private val byRepo = AgentListSettings(grouping = AgentGrouping.Repo)

    @Test fun theMainCheckoutIsTheRepositoryItself() {
        assertEquals(RepoPlace("/code/app", "app", "/code/app", main = true), repoOf("/code/app"))
        assertEquals(RepoPlace("/code/app", "app", "/code/app", main = true), repoOf("/code/app/"))
    }

    @Test fun commonWorktreeLayoutsBelongToTheRepositoryRoot() {
        listOf("/code/app/.worktrees/fix-login", "/code/app/.claude/worktrees/fix-login", "/code/app/.herdr/worktrees/fix-login").forEach {
            assertEquals(RepoPlace("/code/app", "app", it, main = false), repoOf(it))
        }
    }

    @Test fun aHomeLevelWorktreeCollectionNamesTheRepositoryAfterTheMarker() {
        assertEquals(RepoPlace("/home/me/.worktrees/persona", "persona", "/home/me/.worktrees/persona/fix-login", main = false),
            repoOf("/home/me/.worktrees/persona/fix-login"))
    }

    @Test fun withoutAMarkerTheDirectoryIsItsOwnRepository() {
        assertEquals(RepoPlace("/code/app/server/internal", "internal", "/code/app/server/internal", main = true), repoOf("/code/app/server/internal"))
        assertEquals(RepoPlace("", "", "", main = true), repoOf(""))
    }

    @Test fun theMainCheckoutComesFirstThenTreesByName() {
        val repos = agentRepos(listOf(session("main", "/code/app"), session("wt-b", "/code/app/.worktrees/fix-b"),
            session("wt-a", "/code/app/.worktrees/fix-a"), session("wt-a2", "/code/app/.worktrees/fix-a")))
        assertEquals(listOf("app"), (repos.map { it.name }).zhAny())
        assertEquals(listOf("app*", "fix-a", "fix-b"), (repos.single().worktrees.map { it.name + if (it.main) "*" else "" }).zhAny())
        assertEquals(listOf("wt-a", "wt-a2"), (repos.single().worktrees[1].members.map { it.session.sessionId }).zhAny())
        assertEquals(4, repos.single().members.size)
        assertFalse(repos.single().flat)
    }

    @Test fun namesSortNaturallyAsOnIos() {
        assertEquals(listOf("app", "App2", "repo2", "repo10"), (agentRepos(listOf(session("a", "/c/repo10"), session("b", "/c/repo2"),
            session("c", "/c/App2"), session("d", "/c/app"))).map { it.name }).zhAny())
    }

    @Test fun aQuietRepositoryWithTreesFoldsIntoOneGroupUnderItsRepositoryId() {
        val list = buildAgentList(listOf(session("m", "/code/app"), session("w", "/code/app/.worktrees/wt")), machines, settings = byRepo)
        assertEquals(listOf("repo/machine-a//code/app"), (list.machines.single().groups).zhAny())
        assertEquals(2, list.groups.single().rows.size)
    }

    @Test fun repositoriesSortByNameAndSameNamesOnTwoComputersStayApart() {
        assertEquals(listOf("alpha", "zeta"), (agentRepos(listOf(session("z", "/code/zeta"), session("a", "/code/alpha"))).map { it.name }).zhAny())
        val two = agentRepos(listOf(session("a", "/code/app"), session("b", "/code/app", machine = "machine-b")))
        assertEquals(listOf("repo/machine-a//code/app", "repo/machine-b//code/app"), (two.map { it.id }).zhAny())
        assertEquals(UnnamedRepo, agentRepos(listOf(session("x", ""))).single().name)
    }

    @Test fun computersKeepHubOrderAndTheirRepositoriesSortByNameWithoutAssistants() {
        val list = buildAgentList(listOf(session("a-zeta", "/code/zeta", "working"), session("a-alpha", "/code/alpha", "working"),
            session("b-beta", "/code/beta", "working", machine = "machine-b"),
            AgentSession(Session(machineId = "machine-a", sessionId = "assistant", cwd = "/code/app", assistant = true))), machines, settings = byRepo)
        assertEquals(listOf("machine-b", "machine-a"), (list.machines.map { it.machine.id }).zhAny())
        assertEquals("one-checkout repositories keep the checkout's id", listOf("tree/machine-a//code/alpha", "tree/machine-a//code/zeta"), (list.machines[1].groups).zhAny())
        assertEquals(listOf(1, 2), list.machines.map { it.repoCount })
        assertTrue(list.groups.flatMap { it.rows }.none { it.session.assistant })
    }

    @Test fun agentsInATreeNeedYouFirstAndBusyTreesHideTheirIdleOnes() {
        val list = buildAgentList(listOf(session("idle", "/code/app/.worktrees/wt"), session("needs", "/code/app/.worktrees/wt", "needs_you"),
            session("main", "/code/app", "working")), machines, settings = byRepo)
        val tree = list.groups.single { it.id == "tree/machine-a//code/app/.worktrees/wt" }
        assertEquals(listOf("needs", "idle"), (tree.rows.map { it.session.sessionId }).zhAny())
        assertEquals(listOf("needs"), (tree.visibleRows.map { it.session.sessionId }).zhAny())
        assertEquals(RepoLevel.Worktree, tree.repo)
        val repo = list.groups.single { it.id == "repo/machine-a//code/app" }
        assertTrue("the trees hold the repository's rows", repo.rows.isEmpty())
        assertEquals(1, repo.stateCounts?.get(AgentState.NeedsYou))
        assertEquals(listOf(repo.id, repo.id), list.groups.filter { it.under != null }.map { it.under })
        assertEquals(3, list.groups.sumOf { it.rows.size })
    }

    @Test fun quietRepositoriesAndTreesFoldAfterTheBusyOnesAndSearchOpensThem() {
        val rows = listOf(session("a", "/code/alpha"), session("b", "/code/beta", "working"),
            session("c", "/code/beta/.worktrees/old"), session("d", "/code/beta/.worktrees/old"))
        val list = buildAgentList(rows, machines, settings = byRepo)
        assertEquals(listOf("repo/machine-a//code/beta", "tree/machine-a//code/beta", "tree/machine-a//code/beta/.worktrees/old",
            "tree/machine-a//code/alpha"), (list.machines.single().groups).zhAny())
        val quiet = list.groups.filter { it.parent == "quiet" }.map { it.id }
        assertEquals(listOf("tree/machine-a//code/beta/.worktrees/old", "tree/machine-a//code/alpha"), (quiet).zhAny())
        assertTrue(list.groups.filter { it.parent == "quiet" }.all { it.folded && it.visibleRows.isEmpty() })
        val searched = buildAgentList(rows, machines, settings = byRepo.copy(query = "code"))
        assertTrue(searched.groups.filter { it.parent == "quiet" }.none { it.folded })
        val opened = buildAgentList(rows, machines, settings = byRepo.copy(expandedQuiet = setOf("tree/machine-a//code/alpha")))
        assertEquals(listOf("a"), (opened.groups.single { it.id == "tree/machine-a//code/alpha" }.visibleRows.map { it.session.sessionId }).zhAny())
    }

    @Test fun pinnedSessionsLeadAndLeaveTheTree() {
        val list = buildAgentList(listOf(session("a", "/code/app", "working"), session("b", "/code/app", "working")), machines,
            pins = AgentPreferences(sessions = listOf("machine-a/b")), settings = byRepo)
        assertEquals("pinned", (list.groups.first().id).zhAny())
        assertEquals(listOf("a"), (list.groups.single { it.id == "tree/machine-a//code/app" }.rows.map { it.session.sessionId }).zhAny())
    }
}
