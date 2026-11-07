package persona.core.contracts

import persona.core.contracts.api.AssistantManagerChoice
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.ManagerRoster
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ManagerRosterTest {
    @After fun reset() = AssistantManagerChoice.reset()

    private val roster = ManagerRoster(listOf(
        HubManager("assistant", "助手", online = true, builtin = true),
        HubManager("m-2", "工作", online = true, isDefault = true),
        HubManager("m-3", "家里", online = false, lastSeen = "2026-11-05T08:00:00Z"),
        HubManager("m-4", "新的", installing = "2026-11-05T09:10:00Z"),
    ), defaultId = "m-2")

    @Test fun installedIsTheDefaultFirstThenTheRosterAndMultiCountsThem() {
        assertEquals(listOf("m-2", "assistant", "m-3"), roster.installed.map { it.id })
        assertTrue(roster.isMulti)
        // An installing manager is not one yet: one installed is not multi.
        val one = ManagerRoster(listOf(HubManager("assistant", online = true, isDefault = true), HubManager("m-4", installing = "x")), "assistant")
        assertFalse(one.isMulti)
        assertNull(one.groups(listOf("a")) { null })
    }

    @Test fun groupsFollowTheRosterLeaveEmptyOnesOutAndUnstampedAreTheDefaults() {
        val items = listOf("a" to "assistant", "b" to null, "c" to "m-3", "d" to "gone", "e" to "m-2")
        val groups = requireNotNull(roster.groups(items) { it.second })
        assertEquals(listOf("m-2", "assistant", "m-3", "gone"), groups.map { it.id })
        assertEquals(listOf("b", "e"), groups.first().items.map { it.first })
        assertNull(groups.last().manager)
        assertEquals(listOf("m-2", "m-3"), requireNotNull(roster.groups(listOf("x" to "m-3", "y" to null)) { it.second }).map { it.id })
    }

    @Test fun theChoiceNamesItsManagerAndOwnsItsItems() {
        AssistantManagerChoice.choose("hub", null, "assistant")
        assertTrue(AssistantManagerChoice.current.value.owns(null))
        assertFalse(AssistantManagerChoice.current.value.owns("m-2"))
        assertNull(AssistantManagerChoice.header("hub"))
        AssistantManagerChoice.choose("hub", "m-2", "assistant")
        assertTrue(AssistantManagerChoice.current.value.owns("m-2"))
        assertFalse(AssistantManagerChoice.current.value.owns(null))
        assertEquals("m-2", AssistantManagerChoice.header("hub"))
        assertNull(AssistantManagerChoice.header("another"))
        // A default that is not the hub's own assistant: picking it sends no header, and unstamped items are its.
        AssistantManagerChoice.choose("hub", "m-2", "m-2")
        assertNull(AssistantManagerChoice.header("hub"))
        assertTrue(AssistantManagerChoice.current.value.owns(null))
        assertTrue(AssistantManagerChoice.current.value.owns("m-2"))
    }

    @Test fun stampsAreRead() {
        val event = JsonCodec.decodeFromString(ServerEvent.serializer(),
            """{"type":"assistant_feed","conversation":"c1","manager":"m-2","item":{"id":"a1","conversation":"c1","at":"2026-11-05T08:00:00Z","kind":"plan","ref":"x"}}""")
        // another manager's live update (ws.broadcast stamps it): read as ever, the stamp left aside
        assertEquals("a1", (event as AssistantFeedEvent).item.id)
        assertEquals("m-2", JsonCodec.decodeFromString(Machine.serializer(), """{"id":"lab","manager":"m-2"}""").manager)
        assertNull(JsonCodec.decodeFromString(Machine.serializer(), """{"id":"lab"}""").manager)
        assertEquals("m-3", JsonCodec.decodeFromString(Session.serializer(), """{"session_id":"s","manager":"m-3"}""").manager)
    }
}
