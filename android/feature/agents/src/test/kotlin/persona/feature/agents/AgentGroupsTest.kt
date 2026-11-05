package persona.feature.agents

import persona.core.contracts.Machine
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubGroupInfo
import org.junit.Assert.*
import org.junit.Test

class AgentGroupsTest {
    private val home = HubGroupInfo("g11111111", "家里", null, true, ConnectionState.Online, emptyList())
    private val work = HubGroupInfo("g0a1b2c3d", "公司", "g0a1b2c3d", false, ConnectionState.Connecting, emptyList())
    private val machines = listOf(Machine("macbook", "MacBook"), Machine("macbook~g0a1b2c3d", "MacBook"))

    @Test fun oneGroupLabelsNothing() {
        assertNull(listOf(home).tagFor("macbook"))
        assertEquals(mapOf("macbook" to "MacBook", "macbook~g0a1b2c3d" to "MacBook"), listOf(home).machineNames(machines, "%1\$s（%2\$s）"))
    }

    @Test fun moreGroupsNameTheGroupOfEveryComputer() {
        val groups = listOf(home, work)
        assertEquals("家里", groups.tagFor("macbook"))
        assertEquals("公司", groups.tagFor("macbook~g0a1b2c3d"))
        assertEquals(mapOf("macbook" to "MacBook（家里）", "macbook~g0a1b2c3d" to "MacBook（公司）"), groups.machineNames(machines, "%1\$s（%2\$s）"))
    }

    @Test fun theFilterAdmitsOnlyItsGroup() {
        val nothing: GroupFilter? = null
        assertTrue(nothing.admits("macbook~g0a1b2c3d"))
        assertTrue(GroupFilter(null).admits("macbook"))
        assertFalse(GroupFilter(null).admits("macbook~g0a1b2c3d"))
        assertTrue(GroupFilter("g0a1b2c3d").admits("macbook~g0a1b2c3d"))
        assertFalse(GroupFilter("g0a1b2c3d").admits("macbook"))
    }

    @Test fun aRequestGoesThroughItsOwnGroupsConnection() {
        val groups = listOf(home, work)
        // The shell's connection is the selected group's (home); work's own state decides for its computers.
        assertEquals(ConnectionState.Connecting, groups.stateFor("macbook~g0a1b2c3d", ConnectionState.Online))
        assertEquals(ConnectionState.Online, groups.stateFor("macbook", ConnectionState.Connecting))
        val workOnline = listOf(home.copy(state = ConnectionState.WaitingForNetwork), work.copy(state = ConnectionState.Online))
        assertEquals(ConnectionState.Online, workOnline.stateFor("macbook~g0a1b2c3d", ConnectionState.WaitingForNetwork))
    }

    @Test fun oneGroupOrAnUnknownOneKeepsTheShellsConnection() {
        assertEquals(ConnectionState.Connecting, listOf(home).stateFor("macbook", ConnectionState.Connecting))
        assertEquals(ConnectionState.Online, emptyList<HubGroupInfo>().stateFor("macbook", ConnectionState.Online))
        assertEquals(ConnectionState.Connecting, listOf(home, work).stateFor("macbook~g99999999", ConnectionState.Connecting))
    }
}
