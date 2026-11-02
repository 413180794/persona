package jarvis.core.contracts.api

import kotlinx.serialization.json.*

/** Adds visible-message receipts without changing legacy inbox transports. */
interface LarkConversationTransport : InboxAssistantTransport {
    suspend fun markRead(feedId: String, position: Long, badgeCount: Long?, messageIds: List<String>)
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
