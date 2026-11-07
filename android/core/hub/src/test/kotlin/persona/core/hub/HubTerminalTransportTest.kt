package persona.core.hub

import persona.core.contracts.SessionKey
import persona.core.contracts.api.TerminalAddress
import persona.core.contracts.api.TerminalCellPosition
import persona.core.contracts.api.TerminalSize
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test

class HubTerminalTransportTest {
    @Test fun keysGoToTheSessionsKeyEndpointTextFirst() {
        val (path, body) = keysRequest(SessionKey("machine/a b", "session#1"), listOf("ctrl+c", "enter"), "ls")
        assertEquals("/api/v1/sessions/machine%2Fa%20b/session%231/keys", (path).zhAny())
        assertEquals(Json.parseToJsonElement("""{"keys":["ctrl+c","enter"],"text":"ls"}"""), body)
        // Text alone still names an empty list of keys, as the old App and iOS send it.
        assertEquals(Json.parseToJsonElement("""{"keys":[],"text":"中文"}"""), keysRequest(SessionKey("m", "s"), emptyList(), "中文").second)
    }

    @Test fun aSessionsStreamMessagesNameItsSession() {
        val key = SessionKey("desk", "s-1")
        assertEquals(Json.parseToJsonElement("""{"type":"terminal_subscribe","machine_id":"desk","session_id":"s-1","epoch":"e1","after_seq":7,"control":{"cols":80,"rows":24}}"""),
            subscribeMessage(key, "e1", 7, TerminalSize(80, 24), false))
        assertEquals(Json.parseToJsonElement("""{"type":"terminal_unsubscribe","machine_id":"desk","session_id":"s-1"}"""),
            terminalMessage("terminal_unsubscribe", key))
    }

    // docs/protocol/protocol.md "Terminal stream": a pane with no session goes by `pane:<pane id>` where the session id goes.
    @Test fun aPaneWithoutASessionIsAddressedByPane() {
        val key = TerminalAddress.key("desk", "pane:w1:p2")
        assertEquals(TerminalAddress.key("desk", "w1:p2"), key)
        assertEquals(Json.parseToJsonElement("""{"type":"terminal_subscribe","machine_id":"desk","session_id":"pane:w1:p2","control":{"cols":40,"rows":12},"takeover":true}"""),
            subscribeMessage(key, null, 0, TerminalSize(40, 12), true))
        assertEquals(Json.parseToJsonElement("""{"type":"terminal_input","machine_id":"desk","session_id":"pane:w1:p2","text":"ls\r","scroll":-3,"at":[2,5]}"""),
            inputMessage(key, "ls\r", null, -3, TerminalCellPosition(2, 5), null))
        assertEquals(Json.parseToJsonElement("""{"type":"terminal_unsubscribe","machine_id":"desk","session_id":"pane:w1:p2"}"""),
            terminalMessage("terminal_unsubscribe", key))
    }
}
