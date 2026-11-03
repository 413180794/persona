package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.Session
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.*
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.Base64
import java.util.UUID
import java.util.concurrent.TimeUnit

class HttpAgentCreationService(
    override val scope: String,
    baseUrl: String,
    private val token: String,
    client: OkHttpClient,
    private val isCurrent: () -> Boolean = { true },
) : AgentCreationService {
    private val base = baseUrl.toHttpUrl()
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
        .retryOnConnectionFailure(false).build()
    // Match the start endpoint's timeout budget; actual startup latency has not been measured.
    private val startHttp = http.newBuilder().readTimeout(130, TimeUnit.SECONDS).build()
    private fun url(vararg segments: String) = base.newBuilder().apply { segments.forEach(::addPathSegment) }.build()
    private fun checkScope() { if (!isCurrent()) throw CancellationException("Pairing changed") }
    private suspend fun read(request: Request, client: OkHttpClient = http): JsonObject {
        checkScope()
        return client.newCall(request).awaitResponse().use { response ->
            checkScope()
            if (!response.isSuccessful) throw HubFailure.from(response)
            JsonCodec.parseToJsonElement(requireNotNull(response.body).string()).jsonObject
        }
    }
    private fun request(vararg segments: String) = Request.Builder().url(url(*segments)).header("Authorization", "Bearer $token")
    override suspend fun launchers(machineId: String): List<AgentLauncher> {
        val body = read(request("api", "v1", "machines", machineId, "launchers").build())
        return JsonCodec.decodeFromJsonElement(body["launchers"] ?: JsonArray(emptyList()))
    }
    override suspend fun directories(machineId: String, path: String): AgentDirectories = JsonCodec.decodeFromJsonElement(read(
        request("api", "v1", "machines", machineId, "dirs")
            .url(url("api", "v1", "machines", machineId, "dirs").newBuilder().addQueryParameter("path", path).build()).build(),
    ))
    override suspend fun start(request: AgentStartRequest, operationId: String): AgentStartResult {
        UUID.fromString(operationId)
        require(request.machineId.isNotBlank() && request.cwd.isNotBlank() && request.launcher.isNotBlank())
        val body = JsonCodec.encodeToString(request).toRequestBody("application/json".toMediaType())
        // A lost response must be reconciled through operation(), never by posting again.
        val result = try {
            outcome(read(request("api", "v1", "sessions").header("X-Jarvis-Op-Id", operationId).post(body).build(), startHttp))
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            AgentStartResult.Unknown
        }
        if (result != AgentStartResult.Unknown) return result
        return try { operation(operationId) }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { AgentStartResult.Unknown }
    }
    override suspend fun operation(operationId: String): AgentStartResult {
        UUID.fromString(operationId)
        val receipt = try { read(request("api", "v1", "ops", operationId).build()) }
        catch (failure: HubFailure) {
            // Old hubs do not record session starts; a missing receipt cannot prove non-execution.
            if (failure.status == 404) return AgentStartResult.Unknown
            throw failure
        }
        val state = receipt["state"]?.jsonPrimitive?.content
        if (receipt["kind"]?.jsonPrimitive?.content?.let { it != "start" } == true) return AgentStartResult.Unknown
        if (state == "pending") return AgentStartResult.Pending
        if (state == "unknown") return AgentStartResult.Unknown
        val result = receipt["result"] as? JsonObject ?: return AgentStartResult.Unknown
        val encoded = result["body"]?.jsonPrimitive?.content ?: return AgentStartResult.Unknown
        val body = JsonCodec.parseToJsonElement(String(Base64.getDecoder().decode(encoded), Charsets.UTF_8)).jsonObject
        return when (state) {
            "done" -> outcome(body)
            "failed" -> AgentStartResult.Rejected(body["error"]?.jsonPrimitive?.content ?: "启动被拒绝，请检查选择")
            else -> AgentStartResult.Unknown
        }
    }
    private fun outcome(body: JsonObject): AgentStartResult {
        val session = body["session"] as? JsonObject
        if (session != null) {
            val decoded = JsonCodec.decodeFromJsonElement<Session>(session)
            if (decoded.machineId.isNotBlank() && decoded.sessionId.isNotBlank()) return AgentStartResult.Created(decoded)
        }
        return if (body["state"]?.jsonPrimitive?.content == "pending") AgentStartResult.Pending else AgentStartResult.Unknown
    }
}
