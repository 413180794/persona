package jarvis.feature.agents

import jarvis.core.contracts.Machine
import jarvis.core.contracts.api.ConnectionState
import jarvis.core.contracts.api.HubGroupInfo
import org.junit.Assert.*
import org.junit.Test

class AgentGroupsTest {
    private val home = HubGroupInfo("g11111111", "家里", null, true, ConnectionState.Online, emptyList())
    private val work = HubGroupInfo("g0a1b2c3d", "公司", "g0a1b2c3d", false, ConnectionState.Connecting, emptyList())
    private val machines = listOf(Machine("macbook", "MacBook"), Machine("macbook~g0a1b2c3d", "MacBook"))

    @Test fun oneGroupLabelsNothing() {
        assertNull(listOf(home).tagFor("macbook"))
        assertEquals(mapOf("macbook" to "MacBook", "macbook~g0a1b2c3d" to "MacBook"), listOf(home).machineNames(machines))
    }

    @Test fun moreGroupsNameTheGroupOfEveryComputer() {
        val groups = listOf(home, work)
        assertEquals("家里", groups.tagFor("macbook"))
        assertEquals("公司", groups.tagFor("macbook~g0a1b2c3d"))
        assertEquals(mapOf("macbook" to "MacBook（家里）", "macbook~g0a1b2c3d" to "MacBook（公司）"), groups.machineNames(machines))
    }

    @Test fun theFilterAdmitsOnlyItsGroup() {
        val nothing: GroupFilter? = null
        assertTrue(nothing.admits("macbook~g0a1b2c3d"))
        assertTrue(GroupFilter(null).admits("macbook"))
        assertFalse(GroupFilter(null).admits("macbook~g0a1b2c3d"))
        assertTrue(GroupFilter("g0a1b2c3d").admits("macbook~g0a1b2c3d"))
        assertFalse(GroupFilter("g0a1b2c3d").admits("macbook"))
    }
}
