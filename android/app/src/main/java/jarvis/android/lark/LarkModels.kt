package jarvis.android.lark

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonDecoder
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import java.time.Instant
import java.time.OffsetDateTime

// Feishu (lark-connector) wire types as the hub forwards them (docs/protocol.md「飞书 API」, iOS LarkModels.swift).
// `parts[].metadata` stays raw JSON: cards carry their whole element tree there.

/** RFC 3339 with or without an offset; Go's zero time and garbage are null. */
fun parseLarkTime(raw: String?): Instant? {
    if (raw.isNullOrBlank()) return null
    val t = runCatching { OffsetDateTime.parse(raw).toInstant() }.getOrNull() ?: runCatching { Instant.parse(raw) }.getOrNull() ?: return null
    return if (t.epochSecond > 0) t else null
}

/** `Instant?` as an RFC 3339 string: a zero time or something unreadable decodes to null instead of failing the page. */
object LarkTimeSerializer : KSerializer<Instant?> {
    override val descriptor: SerialDescriptor = PrimitiveSerialDescriptor("jarvis.LarkTime", PrimitiveKind.STRING)

    override fun deserialize(decoder: Decoder): Instant? {
        if (decoder is JsonDecoder) {
            val el = decoder.decodeJsonElement()
            return if (el is JsonPrimitive && el.isString) parseLarkTime(el.content) else null
        }
        return parseLarkTime(decoder.decodeString())
    }

    override fun serialize(encoder: Encoder, value: Instant?) {
        encoder.encodeString(value?.toString() ?: "0001-01-01T00:00:00Z")
    }
}

// --- Lenient JSON access -------------------------------------------------------------------------------------------

operator fun JsonElement?.get(key: String): JsonElement? = (this as? JsonObject)?.get(key)?.takeUnless { it is JsonNull }

val JsonElement?.str: String? get() = (this as? JsonPrimitive)?.takeUnless { it is JsonNull }?.contentOrNull

val JsonElement?.dbl: Double? get() = (this as? JsonPrimitive)?.contentOrNull?.toDoubleOrNull()

val JsonElement?.int: Int? get() = dbl?.toInt()

val JsonElement?.bool: Boolean?
    get() = (this as? JsonPrimitive)?.let { it.booleanOrNull ?: it.contentOrNull?.toBooleanStrictOrNull() }

val JsonElement?.arr: List<JsonElement>? get() = (this as? JsonArray)?.toList()

val JsonElement?.obj: JsonObject? get() = this as? JsonObject

// --- Conversations ----------------------------------------------------------------------------------------------------

@Serializable
data class LarkConversation(
    val id: String,
    val name: String = "",
    val avatar: String? = null,
    @SerialName("is_group") val isGroup: Boolean = false,
    @SerialName("is_topic") val isTopic: Boolean = false,
    val unread: Int = 0,
    @SerialName("marked_unread") val markedUnread: Boolean = false,
    @SerialName("at_me") val atMe: Boolean = false,
    val muted: Boolean = false,
    val tags: List<String> = emptyList(),
    val preview: String = "",
    @SerialName("is_mine") val isMine: Boolean = false,
    @Serializable(with = LarkTimeSerializer::class) @SerialName("updated_at") val updatedAt: Instant? = null,
    @SerialName("read_position") val readPosition: String? = null,
    @SerialName("mention_position") val mentionPosition: String? = null,
)

@Serializable
data class LarkConversationsPage(
    val conversations: List<LarkConversation> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

// --- Messages -----------------------------------------------------------------------------------------------------------

/** One element of a message as lark-connector projects it (`MessagePart`). */
@Serializable
data class LarkPart(
    val kind: String = "",
    val text: String? = null,
    val url: String? = null,
    val refId: String? = null,
    val metadata: JsonElement? = null,
) {
    fun meta(key: String): JsonElement? = metadata[key]
    fun metaString(key: String): String? = meta(key).str
    fun metaBool(key: String): Boolean = meta(key).bool ?: false

    /** Ancestor chain of a structured rich-text leaf (paragraph / list / quote / heading), outermost first. */
    val blocks: List<LarkRichBlock>
        get() {
            val list = meta("blocks").arr ?: return emptyList()
            val out = ArrayList<LarkRichBlock>(list.size)
            for (item in list) {
                val id = item["id"].str ?: return emptyList()
                val type = item["type"].str ?: return emptyList()
                out += LarkRichBlock(id, type, item["listType"].int ?: 0, item["start"].int ?: 1)
            }
            return out
        }
}

data class LarkRichBlock(val id: String, val type: String, val listType: Int = 0, val start: Int = 1)

@Serializable
data class LarkReplyTo(
    val id: String = "",
    @SerialName("sender_name") val senderName: String = "",
    val text: String = "",
    val recalled: Boolean = false,
    val parts: List<LarkPart>? = null,
)

@Serializable
data class LarkReaction(
    val key: String,
    val count: Int = 0,
    val mine: Boolean = false,
    val users: List<String>? = null,
)

@Serializable
data class LarkMessage(
    val id: String,
    val position: String = "0",
    @SerialName("sender_id") val senderId: String? = null,
    @SerialName("sender_name") val senderName: String = "",
    @SerialName("sender_avatar") val senderAvatar: String? = null,
    @SerialName("sender_type") val senderType: String? = null,
    @SerialName("is_mine") val isMine: Boolean = false,
    val type: String = "",
    val text: String = "",
    @Serializable(with = LarkTimeSerializer::class) @SerialName("created_at") val createdAt: Instant? = null,
    val recalled: Boolean = false,
    val edited: Boolean = false,
    @SerialName("reply_count") val replyCount: Int = 0,
    @SerialName("thread_reply_count") val threadReplyCount: Int? = null,
    @SerialName("badge_count") val badgeCount: Int? = null,
    @SerialName("at_me") val atMe: Boolean = false,
    @SerialName("is_read") val isRead: Boolean? = null,
    val parts: List<LarkPart>? = null,
    @SerialName("reply_to") val replyTo: LarkReplyTo? = null,
    val reactions: List<LarkReaction>? = null,
) {
    val isSystem: Boolean get() = type == "SYSTEM" || (parts?.firstOrNull()?.metaBool("system") ?: false)
    val replies: Int get() = maxOf(threadReplyCount ?: 0, replyCount)
}

@Serializable
data class LarkMessagesPage(
    val messages: List<LarkMessage> = emptyList(),
    @SerialName("has_older") val hasOlder: Boolean = false,
    @SerialName("has_newer") val hasNewer: Boolean? = null,
    @SerialName("older_cursor") val olderCursor: String? = null,
    @SerialName("newer_cursor") val newerCursor: String? = null,
)

/** A message inside a merge-forward card: lark-connector's own `MessageView` (camelCase), media already on the hub. */
@Serializable
data class LarkForwardedMessage(
    val id: String = "",
    val senderName: String? = null,
    val senderAvatarUrl: String? = null,
    @Serializable(with = LarkTimeSerializer::class) val createdAt: Instant? = null,
    val parts: List<LarkPart>? = null,
    val textPreview: String? = null,
    val isRecalled: Boolean = false,
)

@Serializable
data class LarkReadReport(
    val position: String,
    @SerialName("badge_count") val badgeCount: Int? = null,
    @SerialName("message_ids") val messageIds: List<String>? = null,
)

// --- Emoji ------------------------------------------------------------------------------------------------------------

@Serializable
data class LarkEmoji(val label: String = "", val url: String = "")

@Serializable
data class LarkEmojiGroup(val title: String = "", val keys: List<String> = emptyList())

@Serializable
data class LarkEmojis(
    val emojis: Map<String, LarkEmoji> = emptyMap(),
    val groups: List<LarkEmojiGroup>? = null,
)

// --- People, link previews, removed ---------------------------------------------------------------------------------

/** A person's profile card (lark-connector `UserProfileDetail`, the fields the card shows). */
@Serializable
data class LarkProfile(
    val displayName: String? = null,
    val enName: String? = null,
    val alias: String? = null,
    val groupNickname: String? = null,
    val avatarUrl: String? = null,
    val coverUrl: String? = null,
    val description: String? = null,
    val medalName: String? = null,
    val timezone: String? = null,
    val userType: String? = null,
    val customStatus: Status? = null,
    val workStatus: WorkStatus? = null,
    val relation: Relation? = null,
    val fields: List<Field>? = null,
) {
    @Serializable data class Field(val key: String = "", val label: String? = null, val value: JsonElement? = null, val url: String? = null)
    @Serializable data class Status(val title: String? = null)
    @Serializable data class WorkStatus(val status: Int? = null)
    @Serializable data class Relation(val external: Boolean = false, val tenantName: String? = null)
}

/** The card Feishu shows under a link (a removed post: author, excerpt, counts). */
@Serializable
data class LarkUrlPreview(
    val title: String? = null,
    val host: String? = null,
    val url: String? = null,
    val card: Card? = null,
) {
    @Serializable
    data class Card(
        val author: String? = null,
        val avatarUrl: String? = null,
        val body: String? = null,
        val footer: String? = null,
        val subtitle: String? = null,
    )
}

@Serializable
data class LarkMomentsContent(val text: String? = null, val segments: List<Segment>? = null) {
    @Serializable data class Segment(val kind: String? = null, val text: String? = null, val key: String? = null, val emojiKey: String? = null)
}

@Serializable
data class LarkMomentsPost(
    val id: String = "",
    val userName: String? = null,
    val userId: String? = null,
    val userAvatarUrl: String? = null,
    val content: LarkMomentsContent? = null,
    val images: List<Image>? = null,
    val reactionGroups: List<ReactionGroup>? = null,
    val reactionCount: Int? = null,
    val commentCount: Int? = null,
    val createTime: Double? = null,
) {
    @Serializable data class Image(val url: String? = null, val width: Double? = null, val height: Double? = null)
    @Serializable data class ReactionGroup(val type: String = "", val count: Int = 0, val selfInvolved: Boolean = false)
}

@Serializable
data class LarkMomentsComment(
    val id: String = "",
    val userName: String? = null,
    val userId: String? = null,
    val userAvatarUrl: String? = null,
    val content: LarkMomentsContent? = null,
    val createTime: Double? = null,
)

/** What a chat page knows about the chat before its first page arrives (name, kind, where to land). */
@Serializable
data class LarkChatInfo(
    val feedId: String,
    val name: String = "",
    val avatar: String? = null,
    val isGroup: Boolean = true,
    val isTopic: Boolean = false,
    /** Unread messages from others when opened (the 「N 条新消息」 count). */
    val unread: Int = 0,
    val mentionPosition: String? = null,
)
