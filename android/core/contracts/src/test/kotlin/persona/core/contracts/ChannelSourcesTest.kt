package persona.core.contracts

import persona.core.contracts.api.ChannelSources
import persona.core.contracts.api.HubChannel
import kotlinx.serialization.decodeFromString
import org.junit.Assert.*
import org.junit.Test

class ChannelSourcesTest {
    private val channels = listOf(
        HubChannel(id = "app", name = "Persona", kind = "app"),
        HubChannel(id = "wechat", name = "微信", kind = "im"),
        HubChannel(id = "feishu", name = "飞书机器人", kind = "im"),
        HubChannel(id = "lark_agent", name = "飞书 /agent", kind = "mention"),
    )

    @Test fun aMessageFromAChannelIsKnownByItsPrefix() {
        assertEquals("微信", (ChannelSources.of("wechat-msg-17", channels)?.name).zhAny())
        assertEquals("飞书机器人", (ChannelSources.of("feishu-om_1", channels)?.name).zhAny())
        assertEquals("lark_agent", (ChannelSources.of("lark_agent-x", channels)?.id).zhAny())
    }

    @Test fun theAppsOwnMessagesAndUnknownOnesAreNotMarked() {
        // The app's own client message ids are UUIDs: eight hex digits before the first dash.
        assertNull(ChannelSources.of("3f2a9c1d-0b4e-4c55-9a1e-6f0d2c7b8a90", channels))
        assertNull(ChannelSources.of("app-1", channels))
        assertNull(ChannelSources.of("telegram-1", channels))
        assertNull(ChannelSources.of("wechat", channels))
        assertNull(ChannelSources.of("-wechat", channels))
        assertNull(ChannelSources.of(null, channels))
        assertNull(ChannelSources.of("wechat-1", emptyList()))
    }

    @Test fun aConversationShowsItsLatestChannel() {
        assertEquals("feishu", (ChannelSources.latest(listOf("feishu", "wechat"), channels)?.id).zhAny())
        assertNull(ChannelSources.latest(emptyList(), channels))
        assertNull(ChannelSources.latest(listOf("app"), channels))
        // Not in the list (yet, or any more): no mark rather than a guess.
        assertNull(ChannelSources.latest(listOf("slack"), channels))
    }

    @Test fun severalHubsListsMergeByIdTheFirstWinning() {
        val merged = ChannelSources.merge(listOf(channels, listOf(HubChannel(id = "wechat", name = "别的"), HubChannel(id = "voice", name = "语音"))))
        assertEquals(listOf("app", "wechat", "feishu", "lark_agent", "voice"), (merged.map { it.id }).zhAny())
        assertEquals("微信", (merged[1].name).zhAny())
    }

    @Test fun sessionsCarryTheirChannelsAndOlderHubsSendNone() {
        val session = JsonCodec.decodeFromString<Session>("""{"session_id":"s1","channels":["wechat","feishu"]}""")
        assertEquals(listOf("wechat", "feishu"), (session.channels).zhAny())
        assertTrue(JsonCodec.decodeFromString<Session>("""{"session_id":"s1"}""").channels.isEmpty())
        // A wrong shape is dropped like any other optional field, not the whole session.
        assertTrue(JsonCodec.decodeFromString<Session>("""{"session_id":"s1","channels":"wechat"}""").channels.isEmpty())
    }
}
