package persona.platform.notify

import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.ServerEvent

class ResidentConnectionTest {
    private class CountingConnection(state: ConnectionState) : HubConnection {
        override val state = MutableStateFlow(state)
        override val events = MutableSharedFlow<ServerEvent>()
        var reconnects = 0
        override fun reconnect() { reconnects++ }
        override fun send(message: JsonObject) = false
    }

    // Starting the service (boot, the app coming to the front) dropped a live connection: "Connecting" flashed and an
    // event resume in flight started over (#4139).
    @Test fun aLiveConnectionIsLeftAlone() {
        for (state in listOf(ConnectionState.Online, ConnectionState.Updating)) {
            val connection = CountingConnection(state)
            ResidentConnection.resume(connection)
            assertEquals("$state", 0, connection.reconnects)
        }
    }

    @Test fun aConnectionThatIsNotUpIsKicked() {
        for (state in listOf(ConnectionState.Connecting, ConnectionState.WaitingForNetwork,
            ConnectionState.NeedsLocalNetworkPermission, ConnectionState.CertificateMismatch, ConnectionState.Revoked)) {
            val connection = CountingConnection(state)
            ResidentConnection.resume(connection)
            assertEquals("$state", 1, connection.reconnects)
        }
    }

    // The service registered its own default network callback that called reconnect() on every onAvailable / onLost,
    // on top of the runtime's, which reconnects only when the network really changed: each change dropped the
    // connection twice. The service must not watch the network itself.
    @Test fun theServiceDoesNotWatchTheNetwork() {
        val source = java.io.File(System.getProperty("jarvis.repo.root"),
            "android/platform/notify/src/main/kotlin/persona/platform/notify/HubService.kt").readText()
        assertEquals(false, source.contains("NetworkCallback"))
    }
}
