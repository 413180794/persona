package jarvis.core.hub

import jarvis.core.contracts.HubScope
import jarvis.core.contracts.JsonCodec
import jarvis.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerializationException
import kotlinx.serialization.decodeFromString
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request

class HttpKnowledgeRepository(
    override val scope: HubScope,
    baseUrl: String,
    private val token: String,
    client: OkHttpClient,
    private val isCurrent: () -> Boolean,
    private val dispatcher: CoroutineDispatcher = Dispatchers.Default,
) : KnowledgeRepository {
    private val base = baseUrl.toHttpUrl().also {
        require(it.username.isEmpty() && it.password.isEmpty() && it.query == null &&
            it.fragment == null && it.encodedPath == "/")
    }
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
        .retryOnConnectionFailure(false).build()

    private fun url(vararg parts: String): HttpUrl = base.newBuilder()
        .addPathSegment("api").addPathSegment("v1").apply { parts.forEach(::addPathSegment) }.build()

    private fun checkScope() {
        if (!isCurrent()) throw CancellationException("Pairing changed")
    }

    private suspend inline fun <reified T> get(url: HttpUrl): T {
        checkScope()
        val request = Request.Builder().url(url).header("Authorization", "Bearer $token").build()
        val body = http.newCall(request).awaitResponse().use { response ->
            checkScope()
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string() ?: throw HubFailure(HubError.Protocol)
        }
        val decoded = withContext(dispatcher) {
            try { JsonCodec.decodeFromString<T>(body) }
            catch (error: SerializationException) { throw HubFailure(HubError.Protocol, cause = error) }
        }
        checkScope()
        return decoded
    }

    override suspend fun list(kind: KnowledgeKind, tag: String?): KnowledgeList = get(
        url("kb", kind.wire).newBuilder().apply {
            tag?.takeIf { it.isNotBlank() }?.let { addQueryParameter("tag", it) }
        }.build(),
    )

    override suspend fun detail(kind: KnowledgeKind, id: String): KnowledgeDetail {
        require(id.isNotBlank() && id != "." && id != "..")
        val detail = get<KnowledgeDetailResponse>(url("kb", kind.wire, id)).detail
        if (detail.kind != kind.wire || detail.id != id) throw HubFailure(HubError.Protocol)
        return detail
    }

    override suspend fun search(query: String): List<KnowledgeHit> {
        if (query.isBlank()) return emptyList()
        return get<KnowledgeSearch>(url("kb-search").newBuilder()
            .addQueryParameter("q", query.trim()).build()).hits
    }
}
