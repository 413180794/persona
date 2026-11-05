package persona.core.navigation

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/**
 * The message a hub link asked for (`?at=<item id>`, #2194), waiting for its conversation's page to land on it. The
 * route itself stays the conversation's (one chat, one entry on the stack, the Persona home showing it in place):
 * whoever opens the link records the message here, and the chat page of that conversation takes it once, whether it
 * opens now or is already on screen.
 */
object MessageJumps {
    data class Jump(val machineId: String, val sessionId: String, val itemId: String)

    var pending: Jump? by mutableStateOf(null)
        private set

    fun request(machineId: String, sessionId: String, itemId: String) {
        pending = Jump(machineId, sessionId, itemId)
    }

    /** The message to land on in this conversation, once: null when the pending jump is another chat's or there is none. */
    fun take(machineId: String, sessionId: String): String? {
        val jump = pending?.takeIf { it.machineId == machineId && it.sessionId == sessionId } ?: return null
        pending = null
        return jump.itemId
    }
}
