package jarvis.core.contracts

import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ModelsTest {
    @Test fun sessionDecodesWithMissingFieldsAndUnknownState() {
        val s = JsonCodec.decodeFromString<Session>(
            """{"machine_id":"server","session_id":"u1","cwd":"/home/x/code/foo","status":"done","state":"later","extra":1}""",
        )
        assertEquals("foo", s.displayTitle)
        // D5 preserves unknown states instead of inferring a known state from status.
        assertEquals(AgentState.Unknown, s.agentState)
        assertEquals(0, s.pendingInteractions)
    }

    @Test fun zeroTimeIsUnset() {
        assertNull(parseTime("0001-01-01T00:00:00Z"))
        assertTrue(parseTime("2026-10-29T15:05:32.42Z") != null)
    }

    @Test fun offsetTimesParse() {
        // A hub in a local zone sends +08:00; the instant must match the same moment written with Z.
        assertEquals(java.time.Instant.parse("2026-11-02T00:30:00Z"), parseTime("2026-11-02T08:30:00+08:00"))
        assertEquals(java.time.Instant.parse("2026-11-02T00:30:00.5Z"), parseTime("2026-11-02T08:30:00.5+08:00"))
        assertNull(parseTime("0001-01-01T08:05:43+08:05"))
    }

    @Test fun resolutionOmitsUnsetFields() {
        assertEquals("""{"allow":true}""", JsonCodec.encodeToString(Resolution(allow = true)))
        assertEquals("""{"approve":false,"feedback":"改"}""", JsonCodec.encodeToString(Resolution(approve = false, feedback = "改")))
        assertEquals("""{"option":"2"}""", JsonCodec.encodeToString(Resolution(option = "2")))
    }

    @Test fun interactionHeadline() {
        val ix = JsonCodec.decodeFromString<Interaction>(
            """{"id":"ix_1","kind":"permission","tool_name":"Bash","summary":"rm -rf build"}""",
        )
        assertEquals("请求使用 Bash：rm -rf build", ix.headline)
    }
}
