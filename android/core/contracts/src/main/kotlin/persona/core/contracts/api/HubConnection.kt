package persona.core.contracts.api

import persona.core.contracts.ServerEvent
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.json.JsonObject

enum class ConnectionState {
    Online, Connecting, Updating, WaitingForNetwork, NeedsLocalNetworkPermission, CertificateMismatch, Revoked,
}

interface HubConnection {
    val state: StateFlow<ConnectionState>

    /** Everything the hub pushes over the WebSocket, decoded. Hot: a late collector misses earlier events. */
    val events: SharedFlow<ServerEvent>

    /**
     * Whether the socket now open resumed the hub's broadcast stream (#1574): the hub replayed everything this client
     * missed while it was away, so what reacts to coming back Online need not refetch. False before a hello, and for
     * hubs that do not resume.
     */
    val resumed: Boolean get() = false

    fun reconnect()

    /** Sends one client message (subscribe, seen, register_phone…) over the open WebSocket; false when not connected. */
    fun send(message: JsonObject): Boolean
}
