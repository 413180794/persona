package jarvis.android

import jarvis.android.data.JsonCodec
import jarvis.android.data.Pins
import jarvis.android.data.Session
import jarvis.android.data.pinKey
import jarvis.android.data.spacePinKey
import jarvis.android.ui.tailOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The mute rule must match the hub's (server/internal/hub/pins.go `muted`) or the phone and the hub disagree. */
class PinsTest {
    private val agent = "server/s1"
    private val space = "server/app"

    @Test fun keysMatchTheHub() {
        val s = Session(machineId = "server", sessionId = "s1", spaceName = "app")
        assertEquals(agent, s.pinKey())
        assertEquals(space, s.spacePinKey())
        assertNull(Session(machineId = "server", sessionId = "s1").spacePinKey())
    }

    @Test fun muteRule() {
        assertFalse(Pins().mutes(agent, space))
        assertTrue(Pins(sessions = listOf(agent)).mutes(agent, null))
        assertTrue(Pins(spaces = listOf(space)).mutes(agent, space))
        // Turned back on inside a muted space.
        assertFalse(Pins(spaces = listOf(space), allowed = listOf(agent)).mutes(agent, space))
        // Muting the agent itself wins over "allowed".
        assertTrue(Pins(sessions = listOf(agent), allowed = listOf(agent)).mutes(agent, space))
    }

    @Test fun togglingMuteFlipsOnTopOfTheSpace() {
        val off = Pins().togglingMute(agent, space)
        assertEquals(listOf(agent), off.sessions)
        assertTrue(off.mutes(agent, space))
        assertFalse(off.togglingMute(agent, space).mutes(agent, space))

        // In a muted space, turning the agent on adds it to allowed; turning it off again removes it.
        val inMuted = Pins(spaces = listOf(space))
        val on = inMuted.togglingMute(agent, space)
        assertEquals(listOf(agent), on.allowed)
        assertFalse(on.mutes(agent, space))
        val back = on.togglingMute(agent, space)
        assertTrue(back.mutes(agent, space))
        assertEquals(emptyList<String>(), back.allowed)
    }

    @Test fun pinToggleAndWireShape() {
        val p = Pins().toggling(agent)
        assertEquals(listOf(agent), p.sessions)
        assertEquals(emptyList<String>(), p.toggling(agent).sessions)
        // Older hubs leave "allowed" out of /pins.
        val decoded = JsonCodec.decodeFromString<Pins>("""{"spaces":["server/app"],"sessions":[]}""")
        assertEquals(listOf(space), decoded.spaces)
        assertEquals(emptyList<String>(), decoded.allowed)
    }

    @Test fun pathTail() {
        assertEquals("/home/x/code", tailOf("/home/x/code"))
        assertEquals("…/code/foo/bar", tailOf("/home/x/code/foo/bar"))
    }
}
