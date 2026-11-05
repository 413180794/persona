package persona.android.ui

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.SessionGitStatus

/** The chat's Git button status reads again once the hub is back (#4239). */
@OptIn(ExperimentalCoroutinesApi::class)
class ShellChatReconnectTest {
    private val repo = SessionGitStatus(isRepo = true)

    @Test fun aReadThatFailedOfflineIsReadAgainOnReconnect() = runTest(UnconfinedTestDispatcher()) {
        val states = MutableStateFlow(ConnectionState.Online)
        var online = false
        var reads = 0
        val shown = mutableListOf<SessionGitStatus?>()
        val follower = launch {
            followGitStatus(states, { reads++; if (online) repo else error("can't reach the hub") }) { shown += it }
        }
        assertEquals(listOf<SessionGitStatus?>(null), shown)
        // Going away and coming back while still unreachable reads nothing until Online.
        states.value = ConnectionState.Connecting
        assertEquals(1, reads)
        online = true
        states.value = ConnectionState.Online
        assertEquals(2, reads)
        assertEquals(listOf(null, repo), shown)
        // Staying Online reads nothing more.
        states.value = ConnectionState.Online
        assertEquals(2, reads)
        follower.cancel()
    }

    @Test fun aReadStillOnItsWayIsStartedAgainOnReconnect() = runTest(UnconfinedTestDispatcher()) {
        val states = MutableStateFlow(ConnectionState.Connecting)
        val stuck = CompletableDeferred<SessionGitStatus>()
        var reads = 0
        val shown = mutableListOf<SessionGitStatus?>()
        val follower = launch { followGitStatus(states, { reads++; if (reads == 1) stuck.await() else repo }) { shown += it } }
        assertEquals(emptyList<SessionGitStatus?>(), shown)
        states.value = ConnectionState.Online
        assertEquals(2, reads)
        assertEquals(listOf<SessionGitStatus?>(repo), shown)
        follower.cancel()
    }
}
