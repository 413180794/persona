package persona.platform.notify

import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SeenSessionsTest {
    private fun session(id: String, state: String) = Session(machineId = "m", sessionId = id, state = state)

    @Test fun unreadTurningIdleIsSeen() {
        val seen = SeenSessions()
        assertEquals(emptyList<SessionKey>(), seen.update(listOf(session("a", "unread"), session("b", "unread"))))
        assertEquals(listOf(SessionKey("m", "a")), seen.update(listOf(session("a", "idle"), session("b", "unread"))))
        // Already reported: idle again is not seen twice.
        assertEquals(emptyList<SessionKey>(), seen.update(listOf(session("a", "idle"), session("b", "unread"))))
    }

    @Test fun otherChangesAreNotSeen() {
        val seen = SeenSessions()
        seen.update(listOf(session("a", "unread"), session("b", "idle"), session("c", "unread")))
        // Working again, idle all along, or missing for a moment while reconnecting: the notification stays.
        assertEquals(emptyList<SessionKey>(), seen.update(listOf(session("a", "working"), session("b", "idle"))))
        assertEquals(emptyList<SessionKey>(), seen.update(listOf(session("a", "idle"), session("b", "idle"), session("c", "unread"))))
    }

    // A reconnect or a regroup can drop a session for a moment; seen meanwhile, it comes back idle.
    @Test fun missingForAMomentIsStillTracked() {
        val seen = SeenSessions()
        seen.update(listOf(session("a", "unread")))
        assertEquals(emptyList<SessionKey>(), seen.update(emptyList()))
        assertEquals(listOf(SessionKey("m", "a")), seen.update(listOf(session("a", "idle"))))
    }

    @Test fun firstListOnlyRecords() {
        assertEquals(emptyList<SessionKey>(), SeenSessions().update(listOf(session("a", "idle"))))
    }

    @Test fun seenRetractsResultsButNotCards() {
        val key = SessionKey("m", "a")
        val tags = NotificationPolicy.resultTags(key)
        assertEquals(listOf("done:${key.path}", "assistant:${key.path}", "reply:${key.path}"), tags)
        listOf("done", "assistant", "reply").forEach { kind ->
            val event = persona.core.contracts.NotifyEvent(kind = kind, machineId = "m", sessionId = "a")
            assertTrue(NotificationPolicy.tag(event) in tags)
        }
        assertTrue(tags.none { it.startsWith("interaction:") })
    }
}
