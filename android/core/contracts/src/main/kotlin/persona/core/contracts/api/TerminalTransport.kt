package persona.core.contracts.api

import persona.core.contracts.ServerEvent
import persona.core.contracts.SessionKey
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.Serializable

data class TerminalSize(val cols: Int, val rows: Int) {
    init { require(cols >= 10 && rows >= 4) }
}
data class TerminalCellPosition(val column: Int, val row: Int)

@Serializable
data class TerminalScreen(val text: String = "", val status: String = "")

/** One paired hub's terminal operations; inputs are never queued across disconnects. */
interface TerminalTransport {
    val connection: StateFlow<ConnectionState>
    val features: StateFlow<Set<String>>
    val events: Flow<ServerEvent>
    fun subscribe(key: SessionKey, epoch: String?, after: Int, size: TerminalSize?, takeover: Boolean = false): Boolean
    fun unsubscribe(key: SessionKey)
    fun input(key: SessionKey, text: String = "", size: TerminalSize? = null,
              scroll: Int = 0, at: TerminalCellPosition? = null, click: TerminalCellPosition? = null): Boolean
    suspend fun screen(key: SessionKey): TerminalScreen

    /**
     * The key endpoint every hub and node has (`POST …/keys`): [text] first, then [keys] by herdr's names. What the
     * phone types while it only watches the pane or reads its screen goes here, not into the stream. Throws when the
     * hub did not take it.
     */
    suspend fun keys(key: SessionKey, keys: List<String>, text: String = ""): Unit =
        throw UnsupportedOperationException("This terminal transport cannot send keys")
}
