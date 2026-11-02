package jarvis.core.hub

import jarvis.core.contracts.api.InboxAssistantTransport
import jarvis.core.contracts.api.LarkConversationTransport
import jarvis.core.contracts.api.LarkThreadTransport
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.*
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

class HttpLarkConversationTransport(
    private val delegate: InboxAssistantTransport,
    baseUrl: String,
    private val token: String,
    client: OkHttpClient,
    private val isCurrent: () -> Boolean,
) : LarkConversationTransport, LarkThreadTransport, LarkThreadReplyTransport, InboxAssistantTransport by delegate {
    override suspend fun replyInThread(messageId: String, text: String, clientId: String) =
        (delegate as LarkThreadReplyTransport).replyInThread(messageId, text, clientId)
    override suspend fun thread(messageId: String, before: String?) =
        (delegate as LarkThreadTransport).thread(messageId, before)
    private val base = baseUrl.toHttpUrl()
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
        .retryOnConnectionFailure(false).build()

    override suspend fun markRead(feedId: String, position: Long, badgeCount: Long?, messageIds: List<String>) {
        require(feedId.isNotBlank() && position >= 0)
        if (!isCurrent()) throw CancellationException("Pairing changed")
        val url = base.newBuilder().addPathSegments("api/v1/lark/conversations")
            .addPathSegment(feedId).addPathSegment("read").build()
        val body = buildJsonObject {
            put("position", position)
            badgeCount?.let { put("badge_count", it) }
            putJsonArray("message_ids") { messageIds.distinct().forEach { add(it) } }
        }
        val request = Request.Builder().url(url).header("Authorization", "Bearer $token")
            .post(body.toString().toRequestBody("application/json".toMediaType())).build()
        http.newCall(request).awaitResponse().use {
            if (!isCurrent()) throw CancellationException("Pairing changed")
            if (!it.isSuccessful) throw HubFailure.from(it)
        }
    }
}
