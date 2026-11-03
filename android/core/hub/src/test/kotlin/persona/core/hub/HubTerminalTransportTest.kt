package persona.core.hub

import persona.core.contracts.SessionKey
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test

class HubTerminalTransportTest {
    @Test fun keysGoToTheSessionsKeyEndpointTextFirst() {
        val (path, body) = keysRequest(SessionKey("machine/a b", "session#1"), listOf("ctrl+c", "enter"), "ls")
        assertEquals("/api/v1/sessions/machine%2Fa%20b/session%231/keys", path)
        assertEquals(Json.parseToJsonElement("""{"keys":["ctrl+c","enter"],"text":"ls"}"""), body)
        // Text alone still names an empty list of keys, as the old App and iOS send it.
        assertEquals(Json.parseToJsonElement("""{"keys":[],"text":"中文"}"""), keysRequest(SessionKey("m", "s"), emptyList(), "中文").second)
    }
}
