package persona.platform.notify

import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection

/** What the resident service does to the connection when it starts (#4139). */
internal object ResidentConnection {
    /**
     * Kicks a connection that is not up (cutting a retry wait short); leaves a live one alone, so starting the
     * service (boot, the app coming to the front) does not drop it and flash "Connecting".
     */
    fun resume(connection: HubConnection) {
        when (connection.state.value) {
            ConnectionState.Online, ConnectionState.Updating -> Unit
            else -> connection.reconnect()
        }
    }
}
