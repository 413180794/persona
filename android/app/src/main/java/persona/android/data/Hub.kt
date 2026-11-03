package persona.android.data

import persona.core.contracts.ServerEvent
import persona.core.contracts.api.ConnectionState
import persona.core.hub.OkHttpHubConnection
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject

enum class ConnState { Connecting, Online, Offline }

/** Compatibility handle for the shell, platform services and share uploads; owns no data stores. */
class Hub(
    val api: ApiClient,
    private val scope: CoroutineScope,
    /** HTTP transports must follow this connection's live route and credentials. */
    val connection: OkHttpHubConnection,
) {
    private val mutableConnection = MutableStateFlow(ConnState.Connecting)
    val conn: StateFlow<ConnState> = mutableConnection.asStateFlow()
    val serverEvents: SharedFlow<ServerEvent> get() = connection.events
    private var observer: Job? = null

    fun start() {
        if (observer != null) return
        observer = scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.state.collect { state ->
                mutableConnection.value = when (state) {
                    ConnectionState.Online -> ConnState.Online
                    ConnectionState.WaitingForNetwork, ConnectionState.Revoked -> ConnState.Offline
                    else -> ConnState.Connecting
                }
            }
        }
    }

    fun stop() {
        observer?.cancel()
        observer = null
        mutableConnection.value = ConnState.Offline
    }

    fun kick() {
        if (connection.state.value != ConnectionState.Online) connection.reconnect()
    }

    fun send(message: JsonObject): Boolean = connection.send(message)

}
