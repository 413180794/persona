package jarvis.feature.agents

import jarvis.core.contracts.*
import jarvis.core.domain.sort.*
import org.junit.Assert.*
import org.junit.Test

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
        val entries = agentTreeEntries(buildAgentList(rows, machines), "")
        assertEquals(listOf("machine-header:machine/mac", "group:mac/app", "session:mac/a", "idle:mac/app", "group:mac/notes",
            "machine-header:machine/box", "group:box/api", "session:box/e"), entries.map { it.key })
        val notes = entries.filterIsInstance<TreeGroupEntry>().single { it.group.title == "notes" }
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
        val searched = agentTreeEntries(buildAgentList(rows, machines, settings = AgentListSettings(query = "notes")), "")
        assertEquals(listOf("machine-header:machine/mac", "group:mac/notes", "session:mac/c", "session:mac/d"), searched.map { it.key })
        val rowsOfNotes = searched.filterIsInstance<TreeRowEntry>()
        assertEquals(listOf(true, false), rowsOfNotes.map { it.first })
        assertEquals(listOf(false, true), rowsOfNotes.map { it.last })
        val folded = agentTreeEntries(buildAgentList(rows, machines, settings = AgentListSettings(folded = setOf("machine/mac"))), "")
        assertEquals(listOf("machine-header:machine/mac", "machine-header:machine/box", "group:box/api", "session:box/e"), folded.map { it.key })
    }

    @Test fun oneComputerSelectedDropsItsHeadingAndLiftsItsSpaces() {
        val entries = agentTreeEntries(buildAgentList(rows, machines, settings = AgentListSettings(machineFilter = "mac")), "mac")
        assertTrue(entries.none { it is TreeMachineEntry })
        assertEquals(setOf(0), entries.filterIsInstance<TreeGroupEntry>().map { it.depth }.toSet())
        assertEquals(setOf(0), entries.filterIsInstance<TreeRowEntry>().map { it.depth }.toSet())
    }

    @Test fun sectionsOutsideTheTreeSayWhereTheirSessionsRun() {
        val pinned = agentTreeEntries(buildAgentList(rows, machines, pins = AgentPreferences(sessions = listOf("box/e"))), "")
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
        val shown = buildAgentList(rows, machines).groups.flatMap { it.rows }
        val chips = machineCounts(listOf(Machine("nas", "NAS")) + machines.filter { it.online }, shown)
        assertEquals(listOf("mac", "box", "nas"), chips.map { it.machine.id })
        assertEquals(listOf(4, 1, 0), chips.map { it.count })
        val searched = buildAgentList(rows, machines, settings = AgentListSettings(query = "api")).groups.flatMap { it.rows }
        assertEquals(listOf(0, 1, 0), machineCounts(machines, searched).map { it.count })
    }
}
