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

    fun reconnect()

    /** Sends one client message (subscribe, seen, register_phone…) over the open WebSocket; false when not connected. */
    fun send(message: JsonObject): Boolean
}
