package jarvis.android.lark

import jarvis.android.data.ApiClient
import jarvis.android.data.HubException
import jarvis.android.data.JsonCodec
import jarvis.android.ui.media.ImageMath
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import okhttp3.Request
import java.io.File

// The hub's Feishu routes the chat page uses (docs/protocol.md「飞书 API」; server/internal/hub/lark.go). Failures are
// HubException, as everywhere else.

@Serializable private data class LarkSendBody(val text: String)
@Serializable private data class LarkReactBody(val key: String, val remove: Boolean)
@Serializable private data class LarkKeys(val keys: List<String> = emptyList())
@Serializable private data class LarkComments(val comments: List<LarkMomentsComment> = emptyList())

private fun conv(id: String) = "/api/v1/lark/conversations/$id"
private fun msg(id: String) = "/api/v1/lark/messages/$id"

/**
 * A page of a chat, old → new: the latest (no cursor), older than [before], newer than [after], or around [around]
 * (`readPosition + 1` opens at the first unread; never the latest page for an unread chat); a topic group's threads with [topic].
 */
suspend fun ApiClient.larkMessages(
    feedId: String, before: String? = null, after: String? = null, around: String? = null, topic: Boolean = false, limit: Int = 40,
): LarkMessagesPage = JsonCodec.decodeFromString(raw("GET", conv(feedId) + "/messages", query = buildMap {
    if (!before.isNullOrEmpty()) put("before", before)
    if (!after.isNullOrEmpty()) put("after", after)
    if (!around.isNullOrEmpty()) put("around", around)
    put("limit", "$limit")
    if (topic) put("topic", "1")
}))

/** Replies in one message's thread. */
suspend fun ApiClient.larkThread(messageId: String): LarkMessagesPage =
    JsonCodec.decodeFromString(raw("GET", msg(messageId) + "/thread", query = mapOf("limit" to "60")))

/** Sends [text] to the chat as the user. */
suspend fun ApiClient.larkSend(feedId: String, text: String) {
    raw("POST", conv(feedId) + "/messages", JsonCodec.encodeToString(LarkSendBody(text)))
}

/** Reports what the reader has seen (only messages that were on screen; never just for opening the chat). */
suspend fun ApiClient.larkRead(feedId: String, report: LarkReadReport) {
    raw("POST", conv(feedId) + "/read", JsonCodec.encodeToString(report))
}

suspend fun ApiClient.larkEmojis(): LarkEmojis = JsonCodec.decodeFromString(raw("GET", "/api/v1/lark/emojis"))

suspend fun ApiClient.larkQuickReactions(): List<String> = JsonCodec.decodeFromString<LarkKeys>(raw("GET", "/api/v1/lark/reactions/quick")).keys

/** Adds or removes the user's reaction [key] on a message. */
suspend fun ApiClient.larkReact(messageId: String, key: String, remove: Boolean) {
    raw("POST", msg(messageId) + "/reactions", JsonCodec.encodeToString(LarkReactBody(key, remove)))
}

/** Presses a callback button on a card, as the user. */
suspend fun ApiClient.larkCardAction(messageId: String, actionId: String) {
    raw("POST", msg(messageId) + "/card-actions/" + actionId, "{}")
}

/** A person's profile card; [feedId] = the chat they were tapped in (the card then shows their group nickname). */
suspend fun ApiClient.larkProfile(userId: String, feedId: String? = null): LarkProfile =
    JsonCodec.decodeFromString(raw("GET", "/api/v1/lark/users/$userId", query = if (feedId.isNullOrEmpty()) emptyMap() else mapOf("feed_id" to feedId)))

suspend fun ApiClient.larkUrlPreview(messageId: String, url: String): LarkUrlPreview =
    JsonCodec.decodeFromString(raw("GET", msg(messageId) + "/url-preview", query = mapOf("url" to url)))

suspend fun ApiClient.larkMomentsPost(postId: String): LarkMomentsPost =
    JsonCodec.decodeFromString(raw("GET", "/api/v1/lark/moments/posts/$postId"))

suspend fun ApiClient.larkMomentsComments(postId: String): List<LarkMomentsComment> =
    JsonCodec.decodeFromString<LarkComments>(raw("GET", "/api/v1/lark/moments/posts/$postId/comments")).comments

/**
 * A Feishu media URL as the hub gave it: hub-relative (`/api/v1/lark/media/…`, already percent-encoded: passed through
 * untouched, with the token) or absolute (the token only for the hub's own host). [size] asks the hub for a thumbnail.
 */
fun ApiClient.larkMediaRequest(url: String, size: Int? = null, range: String? = null): Request? {
    val withSize = if (size == null) url else url.substringBefore('?') + "?" +
        (url.substringAfter('?', "").split('&').filter { it.isNotEmpty() && !it.startsWith("size=") } + "size=$size").joinToString("&")
    val target = ImageMath.resolve(baseUrl, withSize) ?: return null
    return Request.Builder().url(target.url).apply {
        if (target.hub) header("Authorization", "Bearer $token")
        if (range != null) header("Range", range)
    }.get().build()
}

/** The bytes of a Feishu media URL (see [larkMediaRequest]). */
suspend fun ApiClient.larkMediaBytes(url: String, size: Int? = null): ByteArray = withContext(Dispatchers.IO) {
    val req = larkMediaRequest(url, size) ?: throw HubException(400, "链接不可用")
    http.newCall(req).execute().use { r ->
        if (!r.isSuccessful) throw HubException(r.code, "HTTP ${r.code}")
        r.body?.bytes() ?: ByteArray(0)
    }
}

/** Downloads a Feishu file to [into] (a `.part` first, renamed when complete). */
suspend fun ApiClient.larkDownload(url: String, into: File): File = withContext(Dispatchers.IO) {
    val req = larkMediaRequest(url) ?: throw HubException(400, "链接不可用")
    val part = File(into.parentFile, into.name + ".part")
    http.newCall(req).execute().use { r ->
        if (!r.isSuccessful) throw HubException(r.code, "HTTP ${r.code}")
        r.body?.byteStream()?.use { input -> part.outputStream().use { input.copyTo(it) } } ?: throw HubException(r.code, "空文件")
    }
    if (!part.renameTo(into)) throw HubException(500, "保存失败")
    into
}
