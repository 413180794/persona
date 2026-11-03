package persona.core.contracts.api

import kotlinx.serialization.json.JsonObject

/** Thread history is independent of the parent conversation's position window. */
interface LarkThreadTransport {
    suspend fun thread(messageId: String, before: String? = null): JsonObject
}
