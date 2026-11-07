package persona.core.hub

import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.SessionsResponse
import persona.core.contracts.api.AgentArchives

class HttpAgentArchives(
    baseUrl: String,
    private val token: String,
    client: OkHttpClient,
    private val available: () -> Boolean,
    private val isCurrent: () -> Boolean = { true },
) : AgentArchives {
    private val base = baseUrl.toHttpUrl()
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false).build()
    override val supported get() = available()

    private suspend fun request(method: String, segments: List<String>): String {
        if (!isCurrent()) throw CancellationException("Pairing changed")
        check(supported) { "Agent session archives unavailable" }
        val url = base.newBuilder().apply {
            (listOf("api", "v1", "sessions") + segments).forEach(::addPathSegment)
        }.build()
        val request = Request.Builder().url(url).header("Authorization", "Bearer $token")
            .method(method, if (method == "GET") null else "{}".toRequestBody("application/json".toMediaType())).build()
        return http.newCall(request).awaitResponse().use { response ->
            if (!isCurrent()) throw CancellationException("Pairing changed")
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string().orEmpty()
        }
    }

    override suspend fun list() = JsonCodec.decodeFromString(
        SessionsResponse.serializer(), request("GET", listOf("archived")),
    ).sessions

    private suspend fun write(key: SessionKey, action: String) {
        val result = JsonCodec.parseToJsonElement(request("POST", listOf(key.machineId, key.sessionId, action))).jsonObject
        check(result["ok"]?.jsonPrimitive?.booleanOrNull == true) { "Archive operation was not acknowledged" }
    }
    override suspend fun archive(key: SessionKey) = write(key, "archive")
    override suspend fun restore(key: SessionKey) = write(key, "unarchive")
}
