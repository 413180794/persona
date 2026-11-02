package jarvis.core.hub

import jarvis.core.contracts.JsonCodec
import jarvis.core.contracts.api.InboxActionsTransport
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.contentOrNull
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

/** Resolve the active route per call; credentials and scope must belong to that route. */
class HttpInboxActionsTransport(
    client: OkHttpClient,
    private val endpoint: () -> HttpUrl,
    private val token: String,
    private val isCurrent: () -> Boolean,
) : InboxActionsTransport {
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
        .retryOnConnectionFailure(false).build()

    private suspend fun request(itemId: String? = null): String {
        if (!isCurrent()) throw CancellationException("Pairing changed")
        val url = endpoint().newBuilder().encodedPath("/api/v1/inbox").query(null)
            .addPathSegment(itemId ?: "refresh").build()
        val request = Request.Builder().url(url).header("Authorization", "Bearer $token").apply {
            if (itemId == null) post("{}".toRequestBody("application/json".toMediaType()))
        }.build()
        return http.newCall(request).awaitResponse().use { response ->
            if (!isCurrent()) throw CancellationException("Pairing changed")
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string().orEmpty()
        }
    }

    override suspend fun collectNow() { request() }
    override suspend fun context(itemId: String): String {
        val result = JsonCodec.parseToJsonElement(request(itemId)).jsonObject
        val item = result["item"]?.jsonObject ?: result
        return item["context"]?.jsonPrimitive?.contentOrNull.orEmpty()
    }
}
