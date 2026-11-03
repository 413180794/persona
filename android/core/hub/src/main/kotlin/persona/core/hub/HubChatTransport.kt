package persona.core.hub

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.serialization.encodeToString
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/** Shares the selected connection's authenticated HTTP client and route. */
class HubChatTransport private constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
) : ChatTransport {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body -> connection.request(path, method, body) })

    /** Fixed origins are only available to tests in this module. */
    internal constructor(http: HubHttpClient, endpoint: Endpoint, token: String) : this({ path, method, body ->
        val url = endpoint.baseUrl().resolve(path) ?: throw HubFailure(HubError.Protocol, retryable = false)
        http.client.newCall(Request.Builder().url(url).header("Authorization", "Bearer $token")
            .method(method, body).build()).awaitResponse().also { response ->
            if (!response.isSuccessful) throw response.use { HubFailure.from(it) }
        }
    })

    override suspend fun machines() = get<MachinesResponse>(api("machines")).machines
    override suspend fun sessions() = get<SessionsResponse>(api("sessions")).sessions
    override suspend fun interactions() = get<InteractionsResponse>(api("interactions")).interactions

    override suspend fun items(session: SessionKey, query: ItemsQuery): ItemsResponse {
        val url = path("sessions", session.machineId, session.sessionId, "items").apply {
            addQueryParameter("lazy_images", "1")
            when {
                query.afterSeq != null -> addQueryParameter("after_seq", query.afterSeq.toString())
                query.fromSeq != null -> {
                    addQueryParameter("from_seq", query.fromSeq.toString())
                    addQueryParameter("through_seq", query.throughSeq.toString())
                }
                else -> {
                    addQueryParameter("turns", "2")
                    addQueryParameter("fold_work", "1")
                    query.beforeSeq?.let { addQueryParameter("before_seq", it.toString()) }
                }
            }
        }.build()
        return get(url.encodedPath + "?" + url.encodedQuery)
    }

    override suspend fun resolve(id: String, resolution: Resolution) {
        try {
            request(api("interactions", id, "resolve"), "POST",
                JsonCodec.encodeToString(resolution).toRequestBody("application/json; charset=utf-8".toMediaType())).close()
        } catch (error: HubFailure) {
            if (error.status == 410) throw InteractionGoneException()
            if (error.code == "prompt_changed") throw InteractionPromptChangedException()
            throw error
        }
    }

    override suspend fun image(session: SessionKey, itemId: String): ByteArray =
        request(api("sessions", session.machineId, session.sessionId, "items", itemId, "image"), "GET", null)
            .use { it.body!!.bytes() }

    override suspend fun fullItem(session: SessionKey, itemId: String): Item =
        get(api("sessions", session.machineId, session.sessionId, "items", itemId, "full"))

    private suspend inline fun <reified T> get(path: String): T = request(path, "GET", null).use {
        try { JsonCodec.decodeFromString<T>(it.body!!.string()) }
        catch (error: Exception) { throw HubFailure(HubError.Protocol, retryable = false, cause = error) }
    }

    // Only the encoded path/query are used; the authenticated caller chooses the actual origin.
    private fun path(vararg segments: String) = HttpUrl.Builder().scheme("https").host("example.com")
        .addPathSegment("api").addPathSegment("v1").apply { segments.forEach(::addPathSegment) }
    private fun api(vararg segments: String) = path(*segments).build().encodedPath
}
