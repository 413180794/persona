package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.InboxAssistantTransport
import persona.core.contracts.api.LarkThreadTransport
import persona.core.contracts.api.MailReplyTarget
import persona.core.contracts.api.MailReplyTargetChanged
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.*
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

class HttpInboxAssistantTransport private constructor(
    override val scope: String,
    private val request: suspend (String, String, RequestBody?) -> Response,
    private val mediaOrigin: () -> HttpUrl,
    private val isCurrent: () -> Boolean,
) : InboxAssistantTransport, LarkThreadTransport, LarkThreadReplyTransport {
    constructor(scope: String, connection: OkHttpHubConnection, isCurrent: () -> Boolean = { true }) : this(
        scope, { path, method, body -> connection.request(path, method, body) },
        { connection.activeRoute().endpoint.baseUrl() }, isCurrent,
    )

    /** Fixed origins are only available to tests in this module. */
    internal constructor(
        scope: String, baseUrl: String, token: String, client: OkHttpClient,
        isCurrent: () -> Boolean = { true },
    ) : this(scope, fixedRequest(baseUrl, token, client), { baseUrl.toHttpUrl() }, isCurrent)

    private fun url(vararg segments: String): HttpUrl = HttpUrl.Builder().scheme("https").host("example.com").apply {
        segments.forEach(::addPathSegment)
    }.build()

    private fun checkScope() { if (!isCurrent()) throw CancellationException("Pairing changed") }

    private suspend fun json(url: HttpUrl, body: JsonObject? = null): JsonObject {
        checkScope()
        return request(url.apiPath(), if (body == null) "GET" else "POST",
            body?.toString()?.toRequestBody("application/json".toMediaType())).use { response ->
            checkScope()
            val text = response.body?.string().orEmpty()
            if (text.isBlank()) JsonObject(emptyMap()) else JsonCodec.parseToJsonElement(text).jsonObject
        }
    }

    override suspend fun inbox() = json(url("api", "v1", "inbox"))
    override suspend fun larkConversations(cursor: String?) = json(
        url("api", "v1", "lark", "conversations").newBuilder().apply {
            addQueryParameter("scope", "chats")
            cursor?.let { addQueryParameter("cursor", it) }
        }.build(),
    )
    override suspend fun replyInThread(messageId: String, text: String, clientId: String): JsonObject {
        require(messageId.isNotBlank() && text.isNotBlank() && clientId.isNotBlank())
        return json(url("api", "v1", "lark", "messages", messageId, "replies"),
            buildJsonObject { put("text", text); put("thread", true); put("client_id", clientId) })
    }
    override suspend fun thread(messageId: String, before: String?) = json(
        url("api", "v1", "lark", "messages", messageId, "thread").newBuilder().apply {
            addQueryParameter("limit", "60")
            before?.let { addQueryParameter("before", it) }
        }.build(),
    )
    override suspend fun setDone(itemId: String, done: Boolean) = json(url("api", "v1", "inbox", itemId, "done"),
        buildJsonObject { put("done", done) })
    override suspend fun reply(itemId: String?, feedId: String?, text: String, clientId: String): JsonObject {
        require(clientId.isNotBlank() && text.isNotBlank())
        require((itemId == null) != (feedId == null))
        val target = if (itemId != null) url("api", "v1", "inbox", itemId, "reply")
            else url("api", "v1", "lark", "conversations", requireNotNull(feedId), "messages")
        return json(target, buildJsonObject { put("text", text); put("client_id", clientId) })
    }
    override suspend fun replyMail(itemId: String, text: String, clientId: String, target: MailReplyTarget): JsonObject {
        require(text.isNotBlank() && clientId.isNotBlank() && target.uid > 0 && target.version.isNotBlank())
        try {
            return json(url("api", "v1", "inbox", itemId, "reply"), buildJsonObject {
                put("text", text); put("client_id", clientId)
                put("expected_uid", target.uid); put("expected_version", target.version)
            })
        } catch (e: HubFailure) {
            if (e.status == 409 && e.code == "mail_reply_target_changed" && e.delivered == "none")
                throw MailReplyTargetChanged()
            throw e
        }
    }
    override suspend fun messages(feedId: String, around: Long?, before: String?, after: String?) = json(
        url("api", "v1", "lark", "conversations", feedId, "messages").newBuilder().apply {
            around?.let { addQueryParameter("around", it.toString()) }
            before?.let { addQueryParameter("before", it) }
            after?.let { addQueryParameter("after", it) }
        }.build(),
    )
    override suspend fun assistant() = json(url("api", "v1", "assistant"))
    override suspend fun createConversation(main: Boolean) = json(url("api", "v1", "assistant", "conversations"),
        buildJsonObject { put("prompt", ""); put("main", main) })
    override suspend fun pins() = json(url("api", "v1", "pins"))
    override suspend fun mutes() = json(url("api", "v1", "mutes"))

    override suspend fun readDocument(url: String): JsonObject {
        val target = url.toHttpUrl()
        require(target.scheme in setOf("http", "https") && target.username.isEmpty() && target.password.isEmpty())
        return json(this.url("api", "v1", "lark", "raw", "documents", "read").newBuilder()
            .addQueryParameter("url", url).addQueryParameter("format", "json").build())
    }

    override suspend fun documentMedia(ref: String): ByteArray {
        val path = requireNotNull(mediaOrigin().resolve(ref))
        require(path.encodedPath.startsWith("/api/v1/lark/raw/documents/")) { "Invalid document media path" }
        return readMedia(ref, document = true)
    }

    override suspend fun media(ref: String): ByteArray = readMedia(ref, document = false)

    private suspend fun readMedia(ref: String, document: Boolean): ByteArray {
        checkScope()
        val base = mediaOrigin()
        val media = requireNotNull(base.resolve(ref))
        require(media.scheme == base.scheme && media.host == base.host && media.port == base.port &&
            media.username.isEmpty() && media.password.isEmpty() && media.fragment == null) { "Invalid media origin" }
        require(document && media.encodedPath.startsWith("/api/v1/lark/raw/documents/") ||
            media.encodedPath.startsWith("/api/v1/lark/media/") ||
            Regex("/api/v1/lark/messages/[^/]+/media/.+").matches(media.encodedPath)) { "Invalid media path" }
        return request(media.apiPath(), "GET", null).use { response ->
                checkScope()
                requireNotNull(response.body).bytes()
            }
    }

    private fun HttpUrl.apiPath() = encodedPath + (encodedQuery?.let { "?$it" } ?: "")

    companion object {
        private fun fixedRequest(baseUrl: String, token: String, client: OkHttpClient): suspend (String, String, RequestBody?) -> Response {
            val base = baseUrl.toHttpUrl()
            // Never forward pairing credentials to redirected media or repeat ambiguous writes.
            val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
                .retryOnConnectionFailure(false).build()
            return { path, method, body ->
                val target = requireNotNull(base.resolve(path))
                http.newCall(Request.Builder().url(target).header("Authorization", "Bearer $token")
                    .method(method, body).build()).awaitResponse().also { response ->
                    if (!response.isSuccessful) throw response.use { HubFailure.from(it) }
                }
            }
        }
    }
}
