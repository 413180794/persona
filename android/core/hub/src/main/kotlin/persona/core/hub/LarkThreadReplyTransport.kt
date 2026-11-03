package persona.core.hub

import kotlinx.serialization.json.JsonObject

/** Repeating an identical request reconciles the durable hub receipt by client ID. */
interface LarkThreadReplyTransport {
    suspend fun replyInThread(messageId: String, text: String, clientId: String): JsonObject
}
