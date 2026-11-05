package persona.feature.chat

import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.ListeningPort

class ChatPortChipsTest {
    @Test fun aPortsChipSaysItsNumberAndTitle() {
        assertEquals(":5173 天气 · 落地页", portChipLabel(ListeningPort(5173, title = " 天气 · 落地页 ")))
        assertEquals(":8000", portChipLabel(ListeningPort(8000, title = "")))
        assertEquals(":3000", portChipLabel(ListeningPort(3000)))
    }

    @Test fun onePerPortInTheHubsOrder() {
        val ports = listOf(ListeningPort(5173, addr = "127.0.0.1"), ListeningPort(3000), ListeningPort(5173, addr = "::1"))
        assertEquals(listOf(5173, 3000), chipPorts(ports).map { it.port })
        assertEquals(emptyList<Int>(), chipPorts(null).map { it.port })
    }
}
