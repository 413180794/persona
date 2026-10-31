package jarvis.android.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerialName
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.HttpUrl.Companion.toHttpUrl
import okio.source
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
    /** The hub installs its own jarvisd build on the machine and reconnects; progress comes with the machine list. */
    suspend fun deploy(machine: String) { raw("POST", "/api/v1/machines/$machine/deploy") }

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
        val images: List<ImagePayload>? = null,
    )

    /**
     * clientMsgId stays the same on a retry: the node types a message once (docs/protocol.md「发消息」).
     * Images go inline as base64 (JPEG, the long side at most 1600 px, as iOS sends them).
     */
    suspend fun send(k: SessionKey, text: String, clientMsgId: String, clearDraft: String? = null, images: List<ImagePayload> = emptyList()) {
        post(sp(k) + "/messages", SendBody(text, clientMsgId, clearDraft, images.ifEmpty { null }))
    }

    @Serializable data class Uploaded(val path: String = "", val name: String = "", val size: Long = 0)

    /**
     * Streams a file to `machine`'s temp directory (docs/protocol.md「发送文件」) and returns where it landed; the app
     * puts that path in the composer, it doesn't send anything by itself. `size` must be exact (Content-Length).
     */
    suspend fun upload(machine: String, name: String, size: Long, open: () -> java.io.InputStream): Uploaded = withContext(Dispatchers.IO) {
        val body = object : okhttp3.RequestBody() {
            override fun contentType() = "application/octet-stream".toMediaType()
            override fun contentLength() = size
            override fun writeTo(sink: okio.BufferedSink) { open().use { sink.writeAll(it.source()) } }
        }
        val req = Request.Builder().url(url("/api/v1/machines/$machine/files", mapOf("name" to name)))
            .header("Authorization", "Bearer $token").post(body).build()
        // Big files: the hub relays them to the node in 1 MB chunks, which can take minutes.
        http.newBuilder().writeTimeout(10, TimeUnit.MINUTES).readTimeout(10, TimeUnit.MINUTES).build().newCall(req).execute().use { resp ->
            val text = resp.body?.string().orEmpty()
            if (!resp.isSuccessful) {
                val obj = runCatching { JsonCodec.parseToJsonElement(text) as? JsonObject }.getOrNull()
                throw HubException(resp.code, obj?.get("error")?.jsonPrimitive?.content ?: "HTTP ${resp.code}", obj?.get("code")?.jsonPrimitive?.content, obj)
            }
            JsonCodec.decodeFromString(text)
        }
    }

    suspend fun interrupt(k: SessionKey) { raw("POST", sp(k) + "/interrupt") }
    suspend fun seen(k: SessionKey) { raw("POST", sp(k) + "/seen") }

    @Serializable private data class Keys(val keys: List<String>, val text: String = "")
    suspend fun keys(k: SessionKey, keys: List<String>, text: String = "") { post(sp(k) + "/keys", Keys(keys, text)) }

    @Serializable data class Screen(val text: String = "", val status: String? = null)
    suspend fun screen(k: SessionKey, lines: Int = 60, ansi: Boolean = false): Screen =
        get(sp(k) + "/screen", mapOf("lines" to lines.toString()) + if (ansi) mapOf("format" to "ansi") else emptyMap())

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
        /** Open the new tab in this herdr workspace (the space page's "在这个空间开新标签"). */
        @SerialName("workspace_id") val workspaceId: String? = null,
        /** Answer at once and start in the background (docs/protocol.md「新建会话」): the chat opens right away. */
        val async: Boolean? = null,
        /** What the tab name is made from when the first message goes as a message of its own, not `prompt`. */
        @SerialName("name_hint") val nameHint: String? = null,
    )
    @Serializable private data class SessionResp(val session: Session)
    /** Starts claude in a new herdr tab, in the background: the session comes back at once, the first message goes in
     *  once claude is ready. */
    /** `mode` null = whatever the launcher / that computer's Claude settings use (as iOS sends it). */
    /** As iOS: no `prompt`, the first message is sent as an ordinary message once the chat is open (on screen at once). */
    suspend fun start(machine: String, cwd: String, launcher: String, nameHint: String?, mode: String? = null, workspaceId: String? = null): Session =
        JsonCodec.decodeFromString<SessionResp>(post("/api/v1/sessions", StartBody(machine, cwd, launcher, null, mode, workspaceId, async = true, nameHint = nameHint))).session

    @Serializable private data class ModesBody(
        @SerialName("permission_mode") val permissionMode: String? = null,
        val effort: String? = null,
    )
    /** Switch a running session's permission mode or effort (node/modes.go; bypassPermissions only at start). */
    suspend fun setModes(k: SessionKey, permissionMode: String? = null, effort: String? = null) {
        post(sp(k) + "/modes", ModesBody(permissionMode, effort))
    }

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

    /** The assistant (the Jarvis tab, docs/assistant.md): where it runs and its conversations, newest first. */
    @Serializable
    data class AssistantState(
        @SerialName("machine_id") val machineId: String = "",
        val launcher: String = "",
        val main: String? = null,
        val conversations: List<Session> = emptyList(),
    )
    suspend fun assistant(): AssistantState = get("/api/v1/assistant")

    // --- The assistant's own pages (docs/assistant.md): routines, follow-ups, runs, notes, memory ---

    /** A routine: the assistant starts a conversation with `prompt` at `time` (HH:MM, the user's zone) on `weekdays`. */
    @OptIn(ExperimentalSerializationApi::class)
    @Serializable
    data class Routine(
        // Everything the app edits is always on the wire: JsonCodec leaves out values equal to their default, and
        // the hub reads what's missing as empty or false — a 09:00 routine lost its time (400 "needs a time"), an
        // enabled one would turn off. The hub's own fields below stay optional.
        @EncodeDefault val id: String = "",
        @EncodeDefault val title: String = "",
        @EncodeDefault val prompt: String = "",
        @EncodeDefault val time: String = "09:00",
        /** 1 = Monday … 7 = Sunday; empty = every day. */
        @EncodeDefault val weekdays: List<Int> = emptyList(),
        @EncodeDefault val enabled: Boolean = true,
        @SerialName("created_by") val createdBy: String? = null,
        @SerialName("last_run") val lastRun: String? = null,
        @SerialName("last_session_id") val lastSessionId: String? = null,
        @SerialName("last_status") val lastStatus: String? = null,
        @SerialName("last_error") val lastError: String? = null,
        @SerialName("next_run") val nextRun: String? = null,
    )
    @Serializable private data class Routines(val routines: List<Routine> = emptyList())
    suspend fun routines(): List<Routine> = get<Routines>("/api/v1/assistant/routines").routines
    /** Replaces the whole list; the hub keeps its own fields (last run, next run, who made it). */
    suspend fun setRoutines(list: List<Routine>): List<Routine> =
        JsonCodec.decodeFromString<Routines>(raw("PUT", "/api/v1/assistant/routines", JsonCodec.encodeToString(Routines(list.map {
            it.copy(lastRun = null, lastSessionId = null, lastStatus = null, lastError = null, nextRun = null, createdBy = null)
        })))).routines

    @Serializable data class RunStarted(@SerialName("session_id") val sessionId: String = "", @SerialName("machine_id") val machineId: String = "")
    suspend fun runRoutine(id: String): RunStarted = JsonCodec.decodeFromString(raw("POST", "/api/v1/assistant/routines/$id/run", "{}"))

    /** Something the assistant promised to come back to (schedule_followup). */
    @Serializable
    data class Followup(
        val id: String = "",
        val at: String? = null,
        val title: String? = null,
        val prompt: String = "",
        @SerialName("session_id") val sessionId: String? = null,
    )
    @Serializable private data class Followups(val followups: List<Followup> = emptyList())
    suspend fun followups(): List<Followup> = get<Followups>("/api/v1/assistant/followups").followups
    suspend fun cancelFollowup(id: String) { raw("DELETE", "/api/v1/assistant/followups/$id") }
    suspend fun runFollowup(id: String): RunStarted = JsonCodec.decodeFromString(raw("POST", "/api/v1/assistant/followups/$id/run", "{}"))

    /** One run of a routine or follow-up, including failed and skipped ones. */
    @Serializable
    data class Run(
        val id: String = "",
        val kind: String = "",
        val title: String = "",
        val at: String? = null,
        val status: String = "",
        val error: String? = null,
        val manual: Boolean = false,
        @SerialName("session_id") val sessionId: String? = null,
    )
    @Serializable private data class Runs(val runs: List<Run> = emptyList())
    suspend fun runs(): List<Run> = get<Runs>("/api/v1/assistant/runs").runs

    /** What the user told the assistant about themselves (「关于我」); what it is told in effect is `notes_effective`. */
    @Serializable data class Notes(val notes: String? = null, @SerialName("notes_effective") val effective: String = "")
    suspend fun notes(): Notes = get("/api/v1/assistant")
    @Serializable private data class SetNotes(val notes: String)
    suspend fun setNotes(text: String) { raw("PUT", "/api/v1/assistant", JsonCodec.encodeToString(SetNotes(text))) }

    /** A file of the assistant's own memory (Claude Code auto memory). */
    @Serializable data class MemoryFile(val name: String = "", val content: String = "", @SerialName("updated_at") val updatedAt: String? = null)
    @Serializable private data class Memory(val files: List<MemoryFile>? = null)
    suspend fun memory(): List<MemoryFile> = get<Memory>("/api/v1/assistant/memory").files.orEmpty()
    @Serializable private data class Content(val content: String)
    // url() encodes each path segment itself; a "/" in a name would split it, and memory names never hold one.
    suspend fun saveMemory(name: String, content: String) {
        raw("PUT", "/api/v1/assistant/memory/" + name.replace("/", "_"), JsonCodec.encodeToString(Content(content)))
    }
    suspend fun deleteMemory(name: String) { raw("DELETE", "/api/v1/assistant/memory/" + name.replace("/", "_")) }

    // --- Inbox (docs/inbox.md) ---

    @Serializable private data class InboxList(val items: List<InboxItem> = emptyList())
    suspend fun inbox(): List<InboxItem> = get<InboxList>("/api/v1/inbox").items
    /** One item with its `context` (the conversation / mail as text). */
    suspend fun inboxItem(id: String): InboxItem = get("/api/v1/inbox/$id")
    @Serializable private data class Done(val done: Boolean) // no default: always on the wire, false included
    suspend fun setInboxDone(id: String, done: Boolean): InboxItem = JsonCodec.decodeFromString(post("/api/v1/inbox/$id/done", Done(done)))
    @Serializable private data class Instruction(val instruction: String = "")
    @Serializable private data class DraftText(val text: String = "")
    /** A reply drafted in the user's voice; not sent. */
    suspend fun inboxDraft(id: String, instruction: String = ""): String =
        JsonCodec.decodeFromString<DraftText>(post("/api/v1/inbox/$id/draft", Instruction(instruction))).text
    @Serializable private data class ReplyText(val text: String)
    /** Sends: Feishu in the original chat, mail as a reply; the hub then marks it done (done_by=replied). */
    suspend fun inboxReply(id: String, text: String): InboxItem = JsonCodec.decodeFromString(post("/api/v1/inbox/$id/reply", ReplyText(text)))
    suspend fun refreshInbox(): List<InboxItem> = JsonCodec.decodeFromString<InboxList>(raw("POST", "/api/v1/inbox/refresh", "{}")).items

    @Serializable private data class NewConversation(val prompt: String, val main: Boolean? = null)
    /** Starts a conversation with `prompt` as its first message; the first one becomes the main conversation. */
    suspend fun startConversation(prompt: String, main: Boolean): Session =
        JsonCodec.decodeFromString<SessionResp>(post("/api/v1/assistant/conversations", NewConversation(prompt, main.takeIf { it }))).session

    @Serializable private data class Warm(val launcher: String = "")
    /** The Jarvis tab is showing: the hub gets a claude process ready so the first message skips its startup. */
    suspend fun warmAssistant() { post("/api/v1/assistant/warm", Warm()) }

    @Serializable data class NewToken(val id: String, val token: String, val role: String = "")
    @Serializable private data class TokenReq(val name: String, val kind: String)
    /** Trades the pairing token for this device's own (docs/protocol.md「鉴权」). */
    suspend fun createToken(name: String): NewToken =
        JsonCodec.decodeFromString(post("/api/v1/auth/tokens", TokenReq(name, "phone")))
}
