package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GroupKeysTest {
    @Test fun thePrimaryGroupKeepsEveryIdAsItWas() {
        val key = SessionKey("macbook", "s1")
        assertEquals("macbook", (GroupKeys.machine("macbook", null)).zhAny())
        assertEquals(key, GroupKeys.key(key, null))
        assertEquals("macbook/w1", (GroupKeys.path("macbook/w1", null)).zhAny())
        assertNull(GroupKeys.namespaceOf("macbook"))
        assertEquals(key, GroupKeys.rawKey(key))
    }

    @Test fun otherGroupsCarryTheirSuffixAndLoseItOnTheWayToTheirHubs() {
        val key = GroupKeys.key(SessionKey("macbook", "s1"), "g0a1b2c3d")
        assertEquals(SessionKey("macbook~g0a1b2c3d", "s1"), key)
        assertEquals("g0a1b2c3d", (GroupKeys.namespaceOf(key.machineId)).zhAny())
        assertEquals(SessionKey("macbook", "s1"), GroupKeys.rawKey(key))
        assertEquals("macbook~g0a1b2c3d/w1", (GroupKeys.path("macbook/w1", "g0a1b2c3d")).zhAny())
        assertEquals("macbook/w1", (GroupKeys.rawPath("macbook~g0a1b2c3d/w1")).zhAny())
        assertEquals("g0a1b2c3d", (GroupKeys.pathNamespace("macbook~g0a1b2c3d/w1")).zhAny())
        // The mesh's own collision suffix (`@<hub>`) is part of the machine id and survives.
        assertEquals("mac@home", (GroupKeys.raw(GroupKeys.machine("mac@home", "g0a1b2c3d"))).zhAny())
    }

    @Test fun onlyWellFormedGroupIdsCountAsSuffixes() {
        assertTrue(GroupKeys.isGroupId("g0a1b2c3d"))
        assertFalse(GroupKeys.isGroupId("g0A1B2C3D"))
        assertNull(GroupKeys.namespaceOf("build~server"))
        assertNull(GroupKeys.namespaceOf("~g0a1b2c3d"))
        assertEquals("build~server", (GroupKeys.raw("build~server")).zhAny())
        // An empty machine id (the pending main conversation) never gets a suffix.
        assertEquals(SessionKey("", "main"), GroupKeys.key(SessionKey("", "main"), "g0a1b2c3d"))
    }

    @Test fun theHelloCarriesTheMesh() {
        val hello = ServerEvent.decode("""{"type":"hello","hub_id":"a","hub_name":"家里","mesh":{"hubs":[
            {"hub_id":"a","name":"家里","online":true,"self":true,"endpoints":["https://a.example.com:18662"],"tls_port":18662,"fp":"x"},
            {"hub_id":"b","name":"公司","online":false,"endpoints":[]}]}}""") as Hello
        assertEquals("家里", (hello.hubName).zhAny())
        assertEquals(listOf("a", "b"), (hello.mesh!!.hubs.map { it.hubId }).zhAny())
        assertTrue(hello.mesh!!.hubs[0].self)
        assertFalse(hello.mesh!!.hubs[1].online)
        // Hubs without the mesh keep decoding as before.
        assertNull((ServerEvent.decode("""{"type":"hello","hub_id":"a"}""") as Hello).mesh)
    }
}
