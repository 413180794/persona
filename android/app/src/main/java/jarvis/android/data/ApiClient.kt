package jarvis.android.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerialName
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.UUID
import java.util.concurrent.TimeUnit

/** An error answer from the hub: the HTTP status, its `error` text and, for refusals, its `code` (docs/protocol.md). */
class HubException(val status: Int, override val message: String, val code: String? = null, val fields: JsonObject? = null) :
    Exception(message)

/** REST calls to the hub (docs/protocol.md「REST」). One instance per paired hub. */
class ApiClient(val baseUrl: String, val token: String) {
    val http: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(110, TimeUnit.SECONDS) // starting / resuming a session can take ~90 s on the hub
        .pingInterval(20, TimeUnit.SECONDS)
        .build()

    private val jsonType = "application/json; charset=utf-8".toMediaType()

    fun url(path: String, query: Map<String, String> = emptyMap()) =
        baseUrl.trimEnd('/').toHttpUrl().newBuilder().apply {
            path.trimStart('/').split('/').forEach { addPathSegment(it) }
            query.forEach { (k, v) -> addQueryParameter(k, v) }
        }.build()

    fun wsUrl(): String = url("/api/v1/ws").toString().replaceFirst("http", "ws")

    suspend fun raw(method: String, path: String, body: String? = null, query: Map<String, String> = emptyMap()): String =
        withContext(Dispatchers.IO) {
            val req = Request.Builder().url(url(path, query))
                .header("Authorization", "Bearer $token")
                .header("X-Client-Request-Id", UUID.randomUUID().toString().take(16))
                .method(method, body?.toRequestBody(jsonType) ?: if (method == "GET" || method == "DELETE") null else "".toRequestBody(jsonType))
                .build()
            http.newCall(req).execute().use { resp ->
                val text = resp.body?.string().orEmpty()
                if (!resp.isSuccessful) {
                    val obj = runCatching { JsonCodec.parseToJsonElement(text) as? JsonObject }.getOrNull()
                    val msg = obj?.get("error")?.jsonPrimitive?.content ?: "HTTP ${resp.code}"
                    val rid = resp.header("X-Request-Id")
                    throw HubException(
                        resp.code, if (resp.code >= 500 && rid != null) "$msg（请求 $rid）" else msg,
                        obj?.get("code")?.jsonPrimitive?.content, obj,
                    )
                }
                text
            }
        }

    private suspend inline fun <reified T> get(path: String, query: Map<String, String> = emptyMap()): T =
        JsonCodec.decodeFromString(raw("GET", path, query = query))

    private suspend inline fun <reified B> post(path: String, body: B): String = raw("POST", path, JsonCodec.encodeToString(body))

    @Serializable private data class Health(val ok: Boolean = false, val version: String? = null)
    suspend fun health(): Boolean = get<Health>("/api/v1/health").ok

    @Serializable private data class Machines(val machines: List<Machine> = emptyList())
    suspend fun machines(): List<Machine> = get<Machines>("/api/v1/machines").machines
    suspend fun reconnect(machine: String) { raw("POST", "/api/v1/machines/$machine/reconnect") }

    @Serializable private data class Sessions(val sessions: List<Session> = emptyList())
    suspend fun sessions(): List<Session> = get<Sessions>("/api/v1/sessions").sessions

    @Serializable private data class Launchers(val launchers: List<Launcher> = emptyList())
    suspend fun launchers(machine: String): List<Launcher> = get<Launchers>("/api/v1/machines/$machine/launchers").launchers

    @Serializable
    data class ItemsPage(
        val items: List<Item> = emptyList(),
        @SerialName("has_more") val hasMore: Boolean = false,
        val history: String? = null,
        @SerialName("to_seq") val toSeq: Int? = null,
    )

    private fun sp(k: SessionKey) = "/api/v1/sessions/${k.machineId}/${k.sessionId}"

    /** A page of history: `turns` whole turns before `beforeSeq` (the latest when null). */
    suspend fun items(k: SessionKey, beforeSeq: Int? = null, turns: Int = 2): ItemsPage =
        get(sp(k) + "/items", buildMap {
            put("turns", turns.toString()); put("lazy_images", "1")
            beforeSeq?.let { put("before_seq", it.toString()) }
        })

    @Serializable
    data class NewerPage(
        val items: List<Item> = emptyList(),
        @SerialName("more_after") val moreAfter: Boolean = false,
        val history: String? = null,
        @SerialName("from_seq") val fromSeq: Int? = null,
        @SerialName("to_seq") val toSeq: Int? = null,
    )

    /** Everything after `afterSeq`, ~500 items a page (catching up a gap in the live stream). */
    suspend fun items(k: SessionKey, afterSeq: Int): NewerPage =
        get(sp(k) + "/items", mapOf("after_seq" to afterSeq.toString(), "lazy_images" to "1"))

    /** The bytes of a lazy image (`lazy_images=1` leaves them out of pages). */
    suspend fun image(k: SessionKey, itemId: String): ByteArray = withContext(Dispatchers.IO) {
        val u = url(sp(k) + "/items").newBuilder().addPathSegment(itemId).addPathSegment("image").build()
        http.newCall(Request.Builder().url(u).header("Authorization", "Bearer $token").build()).execute().use { r ->
            if (!r.isSuccessful) throw HubException(r.code, "HTTP ${r.code}")
            r.body!!.bytes()
        }
    }

    /** The whole text of an item the page cut (`full_size`). */
    suspend fun fullItem(k: SessionKey, itemId: String): Item = withContext(Dispatchers.IO) {
        val u = url(sp(k) + "/items").newBuilder().addPathSegment(itemId).addPathSegment("full").build()
        http.newCall(Request.Builder().url(u).header("Authorization", "Bearer $token").build()).execute().use { r ->
            val text = r.body?.string().orEmpty()
            if (!r.isSuccessful) throw HubException(r.code, "HTTP ${r.code}")
            JsonCodec.decodeFromString(text)
        }
    }

    @Serializable
    private data class SendBody(
        val text: String,
        @SerialName("client_msg_id") val clientMsgId: String,
        @SerialName("clear_draft") val clearDraft: String? = null,
    )

    /** clientMsgId stays the same on a retry: the node types a message once (docs/protocol.md「发消息」). */
    suspend fun send(k: SessionKey, text: String, clientMsgId: String, clearDraft: String? = null) {
        post(sp(k) + "/messages", SendBody(text, clientMsgId, clearDraft))
    }

    suspend fun interrupt(k: SessionKey) { raw("POST", sp(k) + "/interrupt") }
    suspend fun seen(k: SessionKey) { raw("POST", sp(k) + "/seen") }

    @Serializable private data class Keys(val keys: List<String>, val text: String = "")
    suspend fun keys(k: SessionKey, keys: List<String>, text: String = "") { post(sp(k) + "/keys", Keys(keys, text)) }

    @Serializable data class Screen(val text: String = "", val status: String? = null)
    suspend fun screen(k: SessionKey, lines: Int = 60): Screen = get(sp(k) + "/screen", mapOf("lines" to lines.toString()))

    @Serializable private data class Interactions(val interactions: List<Interaction> = emptyList())
    suspend fun interactions(): List<Interaction> = get<Interactions>("/api/v1/interactions").interactions
    suspend fun resolve(id: String, r: Resolution) { post("/api/v1/interactions/$id/resolve", r) }

    @Serializable
    private data class StartBody(
        @SerialName("machine_id") val machineId: String,
        val cwd: String,
        val launcher: String,
        val prompt: String? = null,
        @SerialName("permission_mode") val permissionMode: String? = null,
    )
    @Serializable private data class SessionResp(val session: Session)
    /** Starts claude in a new herdr tab; resuming or a slow shell can take ~90 s (the read timeout covers it). */
    /** `mode` null = whatever the launcher / that computer's Claude settings use (as iOS sends it). */
    suspend fun start(machine: String, cwd: String, launcher: String, prompt: String?, mode: String? = null): Session =
        JsonCodec.decodeFromString<SessionResp>(post("/api/v1/sessions", StartBody(machine, cwd, launcher, prompt, mode))).session

    @Serializable data class DirEntry(val name: String = "", val path: String = "", @SerialName("is_git") val isGit: Boolean = false)
    @Serializable data class RecentDir(val path: String = "", val name: String = "")
    @Serializable
    data class DirList(
        val path: String = "",
        val parent: String? = null,
        val entries: List<DirEntry> = emptyList(),
        val recent: List<RecentDir>? = null,
    )
    /** Directory browser for a new session; an empty path lists the home directory. */
    suspend fun dirs(machine: String, path: String): DirList = get("/api/v1/machines/$machine/dirs", mapOf("path" to path))

    /** Quits the agent and closes its herdr tab (or just its pane in a split tab). */
    suspend fun close(k: SessionKey) { raw("POST", sp(k) + "/close") }

    @Serializable private data class Rename(@SerialName("tab_name") val tabName: String, val title: String)
    /** Renames the herdr tab and the Claude session together, as the iOS app does. */
    suspend fun rename(k: SessionKey, name: String) { post(sp(k) + "/rename", Rename(name, name)) }

    @Serializable private data class SpaceName(val name: String)
    suspend fun renameSpace(machine: String, workspaceId: String, name: String) {
        post("/api/v1/machines/$machine/spaces/$workspaceId/rename", SpaceName(name))
    }

    suspend fun pins(): Pins = get("/api/v1/pins")
    suspend fun setPins(p: Pins): Pins = JsonCodec.decodeFromString(raw("PUT", "/api/v1/pins", JsonCodec.encodeToString(p)))
    suspend fun mutes(): Pins = get("/api/v1/mutes")
    suspend fun setMutes(p: Pins): Pins = JsonCodec.decodeFromString(raw("PUT", "/api/v1/mutes", JsonCodec.encodeToString(p)))

    @Serializable data class NewToken(val id: String, val token: String, val role: String = "")
    @Serializable private data class TokenReq(val name: String, val kind: String)
    /** Trades the pairing token for this device's own (docs/protocol.md「鉴权」). */
    suspend fun createToken(name: String): NewToken =
        JsonCodec.decodeFromString(post("/api/v1/auth/tokens", TokenReq(name, "phone")))
}
