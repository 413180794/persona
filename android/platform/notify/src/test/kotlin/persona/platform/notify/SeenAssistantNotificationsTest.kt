package persona.platform.notify

import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.Session
import java.time.Instant

class SeenAssistantNotificationsTest {
    private fun at(ms: Long) = Instant.ofEpochMilli(ms).toString()
    private fun session(id: String, seenAt: Long?, assistant: Boolean = true) =
        Session(machineId = "m", sessionId = id, state = "idle", assistant = assistant, seenAt = seenAt?.let(::at))

    private val base = 1_800_000_000_000L

    // Seen on the web while idle all along (no unread → idle): the notification posted before goes, a later one stays.
    @Test fun seenAtMovingRetractsOlderNotifications() {
        val seen = SeenAssistantNotifications()
        seen.update(listOf(session("a", base)), now = base + 10)
        val moved = seen.update(listOf(session("a", base + 5_000)), now = base + 6_000)
        assertEquals(mapOf("assistant:m/a" to base + 5_000), moved)
        assertEquals(mapOf("assistant:m/a" to base + 5_000),
            SeenAssistantNotifications.stale(mapOf("assistant:m/a" to base + 1_000), moved))
        assertEquals(emptyMap<String, Long>(), SeenAssistantNotifications.stale(mapOf("assistant:m/a" to base + 5_500), moved))
    }

    // After a restart the first list is checked too: an old notification of a conversation seen since goes.
    @Test fun firstListReconcilesOldNotifications() {
        val moved = SeenAssistantNotifications().update(listOf(session("a", base), session("b", null)), now = base + 60_000)
        assertEquals(mapOf("assistant:m/a" to base), moved)
        val showing = mapOf("assistant:m/a" to base - 1_000, "assistant:m/b" to base - 1_000)
        assertEquals(setOf("assistant:m/a"), SeenAssistantNotifications.stale(showing, moved).keys)
    }

    // Unchanged seen_at reports nothing again; agents' sessions are SeenSessions' (done:), not these.
    @Test fun onlyChangesOfAssistantConversations() {
        val seen = SeenAssistantNotifications()
        assertEquals(setOf("assistant:m/a"), seen.update(listOf(session("a", base), session("x", base, assistant = false)), base).keys)
        assertEquals(emptyMap<String, Long>(), seen.update(listOf(session("a", base)), base + 1_000))
    }

    // A hub clock ahead of the phone's must not take a notification posted after the list arrived for seen.
    @Test fun cutOffIsCappedAtArrival() {
        val moved = SeenAssistantNotifications().update(listOf(session("a", base + 60_000)), now = base)
        assertEquals(mapOf("assistant:m/a" to base), moved)
        assertEquals(emptyMap<String, Long>(), SeenAssistantNotifications.stale(mapOf("assistant:m/a" to base + 1), moved))
    }
}
