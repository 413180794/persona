package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The hub's `desktops` event (server/internal/hub/desktops.go): which desktop apps are connected (#3048). */
class DesktopsEventTest {
    @Test fun decodesTheList() {
        val event = ServerEvent.decode("""{"type":"desktops","desktops":[{"device":"d1","name":"MacBook","since":"2026-11-05T01:02:03Z"},{"device":"","name":"iMac","since":"2026-11-05T02:00:00Z"}]}""")
        assertTrue(event is DesktopsEvent)
        val list = (event as DesktopsEvent).desktops
        assertEquals(listOf("d1", ""), list.map { it.device })
        assertEquals("MacBook", list[0].name)
        assertEquals(emptyList<HubDesktop>(), (ServerEvent.decode("""{"type":"desktops","desktops":[]}""") as DesktopsEvent).desktops)
    }
}
