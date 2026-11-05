package persona.core.contracts.api

import kotlinx.serialization.json.*

/** Adds visible-message receipts without changing legacy inbox transports. */
interface LarkConversationTransport : InboxAssistantTransport {
    suspend fun markRead(feedId: String, position: Long, badgeCount: Long?, messageIds: List<String>)
    suspend fun react(messageId: String, key: String, remove: Boolean): Unit = Unit
    suspend fun cardAction(messageId: String, actionId: String): Unit = Unit
    suspend fun userProfile(userId: String, feedId: String? = null): JsonObject = JsonObject(emptyMap())
    suspend fun quickReactions(): List<String> = emptyList()
    suspend fun emojis(): JsonObject = JsonObject(emptyMap())
    /** The account's message display settings (`GET /api/v1/lark/message-display`), shared with the web and iOS. */
    suspend fun messageDisplay(): JsonObject = JsonObject(emptyMap())
}

/** Decode raw socket frames before the compatibility event decoder discards unknown events. */
data class LarkInvalidation(val feedIds: Set<String>) {
    companion object {
        fun decode(frame: JsonObject): LarkInvalidation? {
            if ((frame["type"] as? JsonPrimitive)?.contentOrNull != "lark") return null
            val ids = frame["feed_ids"] as? JsonArray ?: return null
            return LarkInvalidation(ids.mapNotNull { (it as? JsonPrimitive)?.takeIf { id -> id.isString }?.content }.toSet())
        }
    }
}
