package persona.feature.inbox

import persona.core.contracts.api.InboxAssistantTransport
import kotlinx.serialization.json.*
import java.net.URLEncoder
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter

internal fun JsonObject.text(key: String) = (get(key) as? JsonPrimitive)?.contentOrNull.orEmpty()
internal fun JsonObject.number(key: String) = (get(key) as? JsonPrimitive)?.longOrNull
internal fun JsonObject.flag(key: String) = (get(key) as? JsonPrimitive)?.booleanOrNull == true
internal fun JsonObject.cursor(key: String) = text(key).takeIf { it.isNotBlank() }
internal fun JsonObject.messages() = (get("messages") as? JsonArray).orEmpty().mapNotNull { it as? JsonObject }

internal fun larkMessage(data: JsonObject, transport: InboxAssistantTransport, feed: String): LarkMessage {
    fun image(ref: String, width: Int = 1, height: Int = 1) = LarkPart.Image(
        AuthenticatedLarkImage("${transport.scope}:$ref") { transport.media(ref) }, width = width, height = height)
    val source = LarkContentSource(transport)
    val id = data.text("id")
    val external = "https://applink.feishu.cn/client/chat/open?chatId=${URLEncoder.encode(feed, "UTF-8")}&messageId=${URLEncoder.encode(id, "UTF-8")}"
    val parts = if (data.flag("recalled")) listOf(LarkPart.Text("消息已撤回")) else
        (data["parts"] as? JsonArray).orEmpty().mapNotNull { it as? JsonObject }.map { part ->
            val meta = part["metadata"] as? JsonObject ?: JsonObject(emptyMap())
            val text = part.text("text").ifBlank { data.text("text") }
            when (part.text("kind")) {
                "image", "emotion" -> image(part.text("url"), meta.number("width")?.toInt() ?: 1, meta.number("height")?.toInt() ?: 1)
                "file" -> LarkPart.File(part.text("url"), meta.text("name").ifBlank { text.ifBlank { "文件" } }, meta.text("mimeType").ifBlank { "application/octet-stream" })
                "text", "at", "code" -> LarkPart.Text(text, source)
                "card", "interactive" -> larkInteractivePart(meta, text, transport)
                "link" -> LarkPart.Text(listOf(text, part.text("url")).filter { it.isNotBlank() }.distinct().joinToString("\n"), source)
                else -> LarkPart.External(text, external, if (part.text("kind") == "media") "video" else "card", meta.text("duration"))
            }
        }.ifEmpty { listOf(LarkPart.Text(data.text("text"), source)) }
    val quote = data["reply_to"] as? JsonObject
    return LarkMessage(id, data.text("sender_id"), data.text("sender_name"), data.number("position"), data.text("created_at"),
        data.flag("is_mine"), parts, data.cursor("client_id") ?: data.cursor("clientId"),
        delivery = when (data.text("local_state")) {
            "failed" -> LarkDelivery.Failed
            "sending", "pending" -> LarkDelivery.Sending
            else -> LarkDelivery.Sent
        },
        avatar = larkAvatar(data.cursor("sender_avatar"), transport.scope, transport::media), bot = data.text("sender_type") == "bot",
        system = data.text("type") == "SYSTEM" || data.flag("recalled"), edited = data.flag("edited"),
        quote = quote?.let { LarkQuote(it.text("id"), it.text("sender_name"), if (it.flag("recalled")) "消息已撤回" else it.text("text")) },
        badgeCount = data.number("badge_count"), isRead = data.flag("is_read"),
        replyCount = maxOf(0L, data.number("reply_count") ?: 0L, data.number("thread_reply_count") ?: 0L),
        thread = larkThreadSource(data, transport, feed))
}

internal fun larkDate(time: String): LocalDate? = runCatching {
    Instant.parse(time).atZone(ZoneId.systemDefault()).toLocalDate()
}.getOrNull()

internal fun larkTime(time: String): String = runCatching {
    DateTimeFormatter.ofPattern("HH:mm").format(Instant.parse(time).atZone(ZoneId.systemDefault()))
}.getOrDefault(time)

internal fun LarkMessage.listKey() = clientId?.let { "client:$it" } ?: "message:$id"
