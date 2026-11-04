package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.*
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.concurrent.TimeUnit

/** Construct per pairing using the shared routed, pinned client; reject stale responses after unpairing. */
class HttpAssistantManagement(
    baseUrl: String,
    private val token: String,
    client: OkHttpClient,
    private val isCurrent: () -> Boolean = { true },
) : AssistantManagement {
    private val base = baseUrl.toHttpUrl()
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
        .retryOnConnectionFailure(false).build()
    /** 立即运行 answers once the hub has started the conversation (it allows itself two minutes), like starting an agent. */
    private val runHttp = http.newBuilder().readTimeout(130, TimeUnit.SECONDS).build()
    /** Compacting may start the conversation's process first, and 新建主对话 reads what the old one left: the hub allows a minute. */
    private val compactHttp = http.newBuilder().readTimeout(70, TimeUnit.SECONDS).build()

    private fun checkScope() { if (!isCurrent()) throw CancellationException("Pairing changed") }

    private suspend fun request(method: String, segments: List<String>, body: JsonElement? = null, client: OkHttpClient = http): JsonObject {
        checkScope()
        val url = base.newBuilder().apply { segments.forEach(::addPathSegment) }.build()
        val payload = body?.toString()?.toRequestBody("application/json".toMediaType())
        val request = Request.Builder().url(url).header("Authorization", "Bearer $token")
            .method(method, payload).build()
        return client.newCall(request).awaitResponse().use { response ->
            checkScope()
            if (!response.isSuccessful) throw HubFailure.from(response)
            val text = response.body?.string().orEmpty()
            if (text.isBlank()) buildJsonObject { } else JsonCodec.parseToJsonElement(text).jsonObject
        }
    }

    private suspend fun assistant(method: String = "GET", vararg path: String, body: JsonElement? = null) =
        request(method, listOf("api", "v1", "assistant") + path, body)

    private inline fun <reified T> JsonObject.list(name: String): List<T> =
        (this[name] as? JsonArray).orEmpty().map { JsonCodec.decodeFromJsonElement<T>(it) }

    override suspend fun warm() { assistant("POST", "warm", body = buildJsonObject { }) }
    override suspend fun config(): AssistantConfig = JsonCodec.decodeFromJsonElement(assistant())
    override suspend fun updateConfig(update: AssistantConfigUpdate): AssistantConfig =
        JsonCodec.decodeFromJsonElement(assistant("PUT", body = JsonCodec.encodeToJsonElement(update)))
    override suspend fun launchers(machineId: String): List<AgentLauncher> {
        require(machineId.isNotBlank())
        return request("GET", listOf("api", "v1", "machines", machineId, "launchers")).list("launchers")
    }
    override suspend fun endConversation(session: SessionKey) {
        request("POST", listOf("api", "v1", "sessions", session.machineId, session.sessionId, "close"), buildJsonObject { })
    }
    override suspend fun compactConversation(session: SessionKey) {
        require(session.sessionId.isNotBlank())
        refusals {
            request("POST", listOf("api", "v1", "assistant", "conversations", session.sessionId, "compact"), buildJsonObject {
                if (session.machineId.isNotBlank()) put("machine_id", session.machineId)
            }, compactHttp)
        }
    }
    override suspend fun newMainConversation(carrySummary: Boolean, expect: String?): NewMainResult {
        val body = buildJsonObject {
            put("carry", if (carrySummary) "summary" else "none")
            expect?.takeIf(String::isNotBlank)?.let { put("expect", it) }
        }
        val out = refusals { request("POST", listOf("api", "v1", "assistant", "main", "new"), body, compactHttp) }
        return NewMainResult((out["archived"] as? JsonPrimitive)?.contentOrNull.orEmpty(),
            (out["carried"] as? JsonPrimitive)?.booleanOrNull == true)
    }
    /** A 409 with a code is the hub refusing the action for a reason it words itself (正在回答, 主对话已经换过了…). */
    private inline fun <T> refusals(block: () -> T): T = try { block() } catch (failure: HubFailure) {
        val code = failure.code
        if (failure.status == 409 && !code.isNullOrBlank()) throw AssistantRefused(code, failure.message)
        throw failure
    }
    override suspend fun routines(): List<Routine> = assistant("GET", "routines").list("routines")
    override suspend fun setRoutines(list: List<Routine>): List<Routine> {
        val editable = list.map { it.copy(createdBy = null, lastRun = null, lastSessionId = null,
            lastStatus = null, lastError = null, nextRun = null) }
        return assistant("PUT", "routines", body = buildJsonObject {
            put("routines", JsonCodec.encodeToJsonElement(editable))
        }).list("routines")
    }
    private fun JsonObject.session(): SessionKey {
        val machine = getValue("machine_id").jsonPrimitive.content
        val session = getValue("session_id").jsonPrimitive.content
        require(machine.isNotBlank() && session.isNotBlank())
        return SessionKey(machine, session)
    }
    override suspend fun runRoutine(id: String) =
        request("POST", listOf("api", "v1", "assistant", "routines", id, "run"), buildJsonObject { }, runHttp).session()
    override suspend fun followups(): List<Followup> = assistant("GET", "followups").list("followups")
    override suspend fun cancelFollowup(id: String) { assistant("DELETE", "followups", id) }
    override suspend fun runFollowup(id: String) =
        request("POST", listOf("api", "v1", "assistant", "followups", id, "run"), buildJsonObject { }, runHttp).session()
    override suspend fun runs(): List<Run> = assistant("GET", "runs").list("runs")
    override suspend fun notes(): AssistantNotes = JsonCodec.decodeFromJsonElement(assistant())
    override suspend fun setNotes(text: String) { assistant("PUT", body = buildJsonObject { put("notes", text) }) }
    override suspend fun memory(): List<MemoryFile> = assistant("GET", "memory").list("files")
    private fun memoryName(name: String): String {
        require(name.isNotBlank() && name != "." && name != ".." && '/' !in name && '\\' !in name)
        return name
    }
    override suspend fun saveMemory(name: String, content: String) {
        assistant("PUT", "memory", memoryName(name), body = buildJsonObject { put("content", content) })
    }
    override suspend fun deleteMemory(name: String) { assistant("DELETE", "memory", memoryName(name)) }
}
