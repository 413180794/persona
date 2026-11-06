package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.HubSent
import persona.core.contracts.api.SentPage
import persona.core.contracts.api.SentWire
import kotlinx.serialization.SerializationException
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.RequestBody
import okhttp3.Response

/** Settings › Sent as you over the pairing's connection (docs/connectors.md "Sent as the user", `GET /api/v1/connectors/sent`, answered by the hub's manager). */
class HttpHubSent internal constructor(
    private val request: suspend (path: String, method: String, body: RequestBody?) -> Response,
) : HubSent {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body -> connection.request(path, method, body) })

    override suspend fun page(before: Long?, limit: Int?, connector: String?): SentPage {
        val body = request(sentPath(before, limit, connector), "GET", null).use { response ->
            // The pairing's connection throws a HubFailure for a status that is not 2xx; a bare request returns it.
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string().orEmpty()
        }
        // Not JSON at all is the hub's protocol gone wrong; JSON that is not an object is an empty page (SentWire.page).
        return try { SentWire.page(JsonCodec.parseToJsonElement(body)) }
        catch (error: SerializationException) { throw HubFailure(HubError.Protocol, cause = error) }
        catch (error: IllegalArgumentException) { throw HubFailure(HubError.Protocol, cause = error) }
    }

    private companion object {
        const val Path = "/api/v1/connectors/sent"

        /** The path with the query: `before`, `limit` and `connector` only when they say something. */
        fun sentPath(before: Long?, limit: Int?, connector: String?): String {
            val url = "https://example.com$Path".toHttpUrl().newBuilder().apply {
                if (before != null && before > 0) addQueryParameter("before", before.toString())
                if (limit != null && limit > 0) addQueryParameter("limit", limit.toString())
                if (!connector.isNullOrEmpty()) addQueryParameter("connector", connector)
            }.build()
            return url.encodedPath + url.encodedQuery?.let { "?$it" }.orEmpty()
        }
    }
}
