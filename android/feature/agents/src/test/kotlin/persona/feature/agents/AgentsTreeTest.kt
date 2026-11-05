package persona.feature.agents

import persona.core.contracts.*
import persona.core.domain.sort.*
import org.junit.Assert.*
import org.junit.Test

/** The 按电脑和空间 tree these tests were written for (the default grouping is now 按仓库). */
private val MachineSpaceTree = AgentListSettings(grouping = AgentGrouping.MachineSpace)

class AgentsTreeTest {
    private fun session(machine: String, id: String, state: String, space: String) =
        AgentSession(Session(machineId = machine, sessionId = id, state = state, spaceId = space, spaceName = space,
            updatedAt = "2026-01-01T00:00:00Z"))

    private val machines = listOf(Machine("mac", "Mac", online = true), Machine("box", "Box", online = true), Machine("nas", "NAS"))
    private val rows = listOf(
        session("mac", "a", "working", "app"), session("mac", "b", "idle", "app"),
        session("mac", "c", "idle", "notes"), session("mac", "d", "idle", "notes"),
        session("box", "e", "needs_you", "api"),
    )

    @Test fun quietSpacesStayUnderTheirComputerAfterTheActiveOnesWithoutATitleOfTheirOwn() {
        val entries = agentTreeEntries(buildAgentList(rows, machines, settings = MachineSpaceTree), "")
        assertEquals(listOf("machine-header:machine/mac", "group:mac/app", "session:mac/a", "idle:mac/app", "group:mac/notes",
            "machine-header:machine/box", "group:box/api", "session:box/e"), entries.map { it.key })
        val notes = entries.filterIsInstance<TreeGroupEntry>().single { it.group.title.shown == "notes" }
        assertTrue(notes.quiet)
        assertTrue("quiet spaces start folded", notes.group.folded)
        assertEquals(2, notes.group.rows.size)
        assertEquals(1, notes.depth)
        assertTrue(entries.filterIsInstance<TreeGroupEntry>().all { it.space })
        assertTrue(entries.filterIsInstance<TreeRowEntry>().none { it.located })
        assertTrue((entries.first() as TreeMachineEntry).first)
        assertFalse(entries.filterIsInstance<TreeMachineEntry>().last().first)
    }

    @Test fun searchingOpensQuietSpacesAndFoldingAComputerLeavesOnlyItsHeading() {
        val searched = agentTreeEntries(buildAgentList(rows, machines, settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, query = "notes")), "")
        assertEquals(listOf("machine-header:machine/mac", "group:mac/notes", "session:mac/c", "session:mac/d"), searched.map { it.key })
        val rowsOfNotes = searched.filterIsInstance<TreeRowEntry>()
        assertEquals(listOf(true, false), rowsOfNotes.map { it.first })
        assertEquals(listOf(false, true), rowsOfNotes.map { it.last })
        val folded = agentTreeEntries(buildAgentList(rows, machines, settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, folded = setOf("machine/mac"))), "")
        assertEquals(listOf("machine-header:machine/mac", "machine-header:machine/box", "group:box/api", "session:box/e"), folded.map { it.key })
    }

    @Test fun oneComputerSelectedDropsItsHeadingAndLiftsItsSpaces() {
        val entries = agentTreeEntries(buildAgentList(rows, machines, settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, machineFilter = "mac")), "mac")
        assertTrue(entries.none { it is TreeMachineEntry })
        assertEquals(setOf(0), entries.filterIsInstance<TreeGroupEntry>().map { it.depth }.toSet())
        assertEquals(setOf(0), entries.filterIsInstance<TreeRowEntry>().map { it.depth }.toSet())
    }

    @Test fun sectionsOutsideTheTreeSayWhereTheirSessionsRun() {
        val pinned = agentTreeEntries(buildAgentList(rows, machines, pins = AgentPreferences(sessions = listOf("box/e")), settings = MachineSpaceTree), "")
        val first = pinned.first() as TreeGroupEntry
        assertEquals("pinned", first.group.id)
        assertFalse(first.space)
        assertTrue((pinned[1] as TreeRowEntry).located)
        assertEquals(1, pinned.count { it.key == "session:box/e" })
        assertTrue("a computer with only pinned sessions has no empty heading", pinned.none { it.key == "machine-header:machine/box" })
        val byState = agentTreeEntries(buildAgentList(rows, machines, settings = AgentListSettings(grouping = AgentGrouping.State)), "")
        assertTrue(byState.none { it is TreeMachineEntry })
        assertTrue(byState.filterIsInstance<TreeGroupEntry>().none { it.space })
        assertTrue(byState.filterIsInstance<TreeRowEntry>().all { it.located && it.depth == 0 })
    }

    @Test fun everyComputerGetsAChipOnlineFirstWithWhatTheListWouldShow() {
        val shown = buildAgentList(rows, machines, settings = MachineSpaceTree).groups.flatMap { it.rows }
        val chips = machineCounts(listOf(Machine("nas", "NAS")) + machines.filter { it.online }, shown)
        assertEquals(listOf("mac", "box", "nas"), chips.map { it.machine.id })
        assertEquals(listOf(4, 1, 0), chips.map { it.count })
        val searched = buildAgentList(rows, machines, settings = AgentListSettings(grouping = AgentGrouping.MachineSpace, query = "api")).groups.flatMap { it.rows }
        assertEquals(listOf(0, 1, 0), machineCounts(machines, searched).map { it.count })
    }

    @Test fun byTodoGroupingTreeFlattensIntoSectionsWithDetailAction() {
        val todo = Todo(id = "todo_1", title = "待办一", agents = listOf(TodoAgent("mac", "a")))
        val list = buildAgentList(
            rows, machines,
            settings = AgentListSettings(grouping = AgentGrouping.Todo),
            todos = listOf(todo)
        )
        val entries = agentTreeEntries(list, "")
        assertTrue(entries.none { it is TreeMachineEntry })
        val groups = entries.filterIsInstance<TreeGroupEntry>()
        assertEquals(listOf("todo/todo_1", "todo/none"), groups.map { it.group.id })
        assertEquals("待办一", groups[0].group.title.shown)
        assertEquals("没有待办", groups[1].group.title.shown)
        val rowsInTodo1 = entries.filterIsInstance<TreeRowEntry>().filter { it.row.session.sessionId == "a" }
        assertEquals(1, rowsInTodo1.size)
    }

    @Test fun byRepositoryTreesSitAStepInsideTheirRepositoryAndFoldWithIt() {
        fun at(id: String, cwd: String, state: String) = AgentSession(Session(machineId = "mac", sessionId = id, state = state, cwd = cwd,
            updatedAt = "2026-01-01T00:00:00Z"))
        val repoRows = listOf(at("main", "/code/app", "working"), at("fix", "/code/app/.worktrees/fix", "needs_you"),
            at("notes", "/code/notes", "idle"), session("box", "e", "needs_you", "api"))
        val byRepo = AgentListSettings(grouping = AgentGrouping.Repo)
        val entries = agentTreeEntries(buildAgentList(repoRows, machines, settings = byRepo), "")
        assertEquals(listOf("machine-header:machine/mac", "group:repo/mac//code/app", "group:tree/mac//code/app", "session:mac/main",
            "group:tree/mac//code/app/.worktrees/fix", "session:mac/fix", "group:tree/mac//code/notes",
            "machine-header:machine/box", "group:tree/box/", "session:box/e"), entries.map { it.key })
        val depths = entries.filterIsInstance<TreeGroupEntry>().associate { it.group.id to it.depth }
        assertEquals(1, depths["repo/mac//code/app"])
        assertEquals(2, depths["tree/mac//code/app/.worktrees/fix"])
        assertEquals(2, entries.filterIsInstance<TreeRowEntry>().single { it.row.session.sessionId == "fix" }.depth)
        assertTrue(entries.filterIsInstance<TreeGroupEntry>().single { it.group.id == "tree/mac//code/notes" }.quiet)
        assertEquals(1, entries.filterIsInstance<TreeGroupEntry>().single { it.group.id == "repo/mac//code/app" }.group.headingCounts
            .single { it.first == persona.core.designsystem.Status.NeedsYou }.second)
        val folded = agentTreeEntries(buildAgentList(repoRows, machines, settings = byRepo.copy(machineFilter = "mac", folded = setOf("repo/mac//code/app"))), "mac")
        assertEquals(listOf("group:repo/mac//code/app", "group:tree/mac//code/notes"), folded.take(2).map { it.key })
    }
}
