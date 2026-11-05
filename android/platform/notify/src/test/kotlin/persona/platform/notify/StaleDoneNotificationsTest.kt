package persona.platform.notify

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Session

@OptIn(ExperimentalCoroutinesApi::class)
class StaleDoneNotificationsTest {
    private fun session(id: String, state: String) = Session(machineId = "m", sessionId = id, state = state)
    private val settle = InteractionNotificationReconciler.SETTLE_MILLIS
    private val since = 1_000L

    private class Shade(vararg posted: Pair<String, Long>) {
        val showing = mutableMapOf(*posted)
        fun retract(tag: String, before: Long) { if (showing[tag]?.let { it < before } == true) showing -= tag }
    }

    private fun TestScope.step() { advanceTimeBy(settle + 1); runCurrent() }

    // After a restart: `a` was seen on a computer meanwhile, `b` is still unread, `c` is not listed, `new` was posted by
    // this process, `edge` exactly at its start. Nothing goes offline or before the lists are verified; verification
    // lands later without the sessions list changing, and then only `a` goes.
    @Test fun oldNotificationsOfSeenSessionsGoOnceVerified() = runTest {
        val shade = Shade("done:m/a" to 10L, "done:m/b" to 20L, "done:m/c" to 30L, "done:m/new" to 2_000L, "done:m/edge" to since)
        val online = MutableStateFlow(false)
        var verified = emptyList<Session>()
        val job = launch { StaleDoneNotifications({ shade.showing.toMap() }, { verified }, shade::retract, since).run(online) }
        step()
        assertEquals(5, shade.showing.size)
        online.value = true
        step()
        assertEquals(5, shade.showing.size)
        verified = listOf(session("a", "idle"), session("b", "unread"), session("new", "idle"), session("edge", "idle"))
        step()
        assertEquals(setOf("done:m/b", "done:m/c", "done:m/new", "done:m/edge"), shade.showing.keys)
        job.cancel()
    }

    // Offline in between pauses the window; it stops for good once spent, so a notification left unread is not polled forever.
    @Test fun theWindowIsBounded() = runTest {
        var asked = 0
        val online = MutableStateFlow(true)
        val job = launch {
            StaleDoneNotifications({ asked++; mapOf("done:m/b" to 10L) }, { listOf(session("b", "unread")) }, { _, _ -> }, since,
                window = settle * 3).run(online)
        }
        step()
        online.value = false
        step(); step()
        assertEquals(1, asked)
        online.value = true
        repeat(5) { step() }
        assertEquals(3, asked)
        job.cancel()
    }

    @Test fun nothingOldStopsLooking() = runTest {
        var asked = 0
        val job = launch {
            StaleDoneNotifications({ asked++; mapOf("done:m/a" to 5_000L) }, { emptyList() }, { tag, _ -> error("retracted $tag") }, since)
                .run(MutableStateFlow(true))
        }
        repeat(5) { step() }
        assertEquals(1, asked)
        assertTrue(job.isActive)
        job.cancel()
    }

    @Test fun onlyIdleSessionsAreStale() {
        val old = setOf("done:m/a", "done:m/b", "done:m/c", "done:m/d")
        val verified = listOf(session("a", "idle"), session("b", "unread"), session("c", "working"), session("d", "needs_you"))
        assertEquals(setOf("done:m/a"), StaleDoneNotifications.stale(old, verified))
    }
}
