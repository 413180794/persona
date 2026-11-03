package persona.core.contracts

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

    @Test fun sourceFieldsDecodeAndRoundTrip() {
        val history = JsonCodec.decodeFromString<Session>(
            """{"machine_id":"m","session_id":"opaque","source_kind":"codex_history","source_thread_id":"thread",""" +
                """"source_instance_id":"instance","parent_thread_id":"parent","read_only_reason":"外部进程正在使用此会话",""" +
                """"capabilities":{"read":true,"future":true},"attached":true,"agent_label":"DSH"}""",
        )
        assertEquals("codex_history", history.sourceKind)
        assertEquals("thread", history.sourceThreadId)
        assertEquals("instance", history.sourceInstanceId)
        assertEquals("parent", history.parentThreadId)
        assertEquals("外部进程正在使用此会话", history.readOnlyReason)
        assertEquals(SessionCapabilities(read = true), history.capabilities)
        assertTrue(history.attached)
        assertEquals(history, JsonCodec.decodeFromString<Session>(JsonCodec.encodeToString(history)))
        // A malformed capability drops only that field (D5), never the session.
        val malformed = JsonCodec.decodeFromString<Session>("""{"session_id":"s","source_kind":"runtime","capabilities":{"send":"yes","read":true}}""")
        assertEquals(SessionCapabilities(read = true), malformed.capabilities)
        val old = JsonCodec.decodeFromString<Session>("""{"session_id":"s"}""")
        assertNull(old.sourceKind); assertNull(old.capabilities); assertEquals(false, old.attached)
    }

    @Test fun agentLabelNamesEveryAgent() {
        fun label(json: String) = JsonCodec.decodeFromString<Session>("""{"session_id":"s",$json}""").agentLabel
        assertEquals("Claude", label(""""status":"idle""""))
        assertEquals("Claude", label(""""agent":"claude""""))
        assertEquals("Codex", label(""""agent":"codex""""))
        assertEquals("Codex", label(""""source_kind":"codex_app_server""""))
        assertEquals("TRAEX", label(""""agent":"traex""""))
        assertEquals("DSH", label(""""agent":"dsh","agent_label":"DSH""""))
        assertEquals("pi", label(""""agent":"pi""""))
        val dsh = JsonCodec.decodeFromString<Session>("""{"session_id":"s","agent":"dsh"}""")
        assertTrue(!dsh.isClaude && !dsh.isCodex)
        assertTrue(JsonCodec.decodeFromString<Session>("""{"session_id":"s","agent":""}""").isClaude)
    }

    @Test fun interactionNamesItsAgentAndWhatAlwaysAllows() {
        val codex = JsonCodec.decodeFromString<Interaction>(
            """{"id":"cx","kind":"permission","agent":"codex","reason":"访问网络","always_label":"以后不再问这个命令"}""",
        )
        assertEquals("Codex", codex.agentDisplayName)
        assertEquals("以后不再问这个命令", codex.alwaysLabel)
        assertEquals("访问网络", codex.reason)
        val dsh = JsonCodec.decodeFromString<Interaction>("""{"id":"d","kind":"question","agent":"dsh","agent_label":"DSH"}""")
        assertEquals("DSH", dsh.agentDisplayName)
        assertEquals("DSH 有问题问你", dsh.headline)
        assertEquals("DSH 请求使用 read_file：读取报表", dsh.copy(kind = "permission", toolName = "read_file", summary = "读取报表").headline)
        assertNull(JsonCodec.decodeFromString<Interaction>("""{"id":"c","kind":"question","agent":"claude"}""").agentDisplayName)
        assertEquals("Claude 有问题问你", JsonCodec.decodeFromString<Interaction>("""{"id":"c","kind":"question"}""").headline)
    }

    @Test fun sessionInfoDecodes() {
        val info = JsonCodec.decodeFromString<SessionInfo>(
            """{"session_id":"s1","cwd":"/x","model":"claude-opus","context_tokens":51234,"output_tokens":9,"turns":3,""" +
                """"compactions":1,"git_branch":"main","started_at":"2026-11-01T10:00:00+08:00","last_activity":"0001-01-01T00:00:00Z","new":1}""",
        )
        assertEquals(51234L, info.contextTokens)
        assertEquals("main", info.gitBranch)
        assertEquals(3, info.turns)
        assertEquals(1, info.compactions)
        assertEquals(java.time.Instant.parse("2026-11-01T02:00:00Z"), parseTime(info.startedAt))
        assertNull(info.lastActivity)
        assertEquals(SessionInfo(), JsonCodec.decodeFromString<SessionInfo>("{}"))
    }

    @Test fun interactionHeadline() {
        val ix = JsonCodec.decodeFromString<Interaction>(
            """{"id":"ix_1","kind":"permission","tool_name":"Bash","summary":"rm -rf build"}""",
        )
        assertEquals("请求使用 Bash：rm -rf build", ix.headline)
    }
}
