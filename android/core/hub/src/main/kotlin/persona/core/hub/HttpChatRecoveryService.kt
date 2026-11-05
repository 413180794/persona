package persona.core.hub

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.*
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

class HttpChatRecoveryService internal constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
) : ChatRecoveryService {
    constructor(connection: OkHttpHubConnection) : this(connection::request)

    private val lock = Mutex()
    private val attempted = mutableSetOf<SessionKey>()

    override suspend fun resume(session: Session): ChatRecoveryResult = lock.withLock {
        if (session.key in attempted) return@withLock verify(session.key)
        if (session.cwd.isBlank() || session.launcher.isNullOrBlank() || session.assistant)
            return@withLock ChatRecoveryResult.Failed(CoreText.of(Key.RecoveryMissing))
        val body = buildJsonObject {
            put("machine_id", session.machineId)
            put("cwd", session.cwd)
            put("launcher", session.launcher)
            put("resume_session_id", session.sessionId)
        }
        // Retain this guard on cancellation and lost responses, including sheet dismissal.
        attempted += session.key
        try {
            val reply = call(api("sessions"), "POST", body)
            val restored = reply["session"]?.let { JsonCodec.decodeFromJsonElement<Session>(it) }
            if (restored != null && restored.machineId == session.machineId && restored.sessionId.isNotBlank())
                ChatRecoveryResult.Restored(restored.key)
            else ChatRecoveryResult.Unknown
        } catch (error: CancellationException) { throw error }
        catch (error: Exception) {
            if (error.definitelyRejected()) {
                attempted -= session.key
                ChatRecoveryResult.Failed(CoreText.of(Key.RecoveryFailed))
            } else ChatRecoveryResult.Unknown
        }
    }

    private suspend fun verify(key: SessionKey): ChatRecoveryResult = try {
        val response = JsonCodec.decodeFromJsonElement<SessionsResponse>(call(api("sessions"), "GET"))
        val restored = response.sessions.firstOrNull {
            it.key == key && it.status in setOf("idle", "working", "blocked", "done")
        }
        if (restored != null) ChatRecoveryResult.Restored(restored.key) else ChatRecoveryResult.Unknown
    } catch (error: CancellationException) { throw error }
    catch (_: Exception) { ChatRecoveryResult.Unknown }

    override suspend fun reconnect(machineId: String): ChatActionResult = lock.withLock {
        try {
            call(api("machines", machineId, "reconnect"), "POST")
            ChatActionResult.Done
        } catch (error: CancellationException) { throw error }
        catch (error: Exception) {
            if (error.definitelyRejected()) ChatActionResult.Failed(CoreText.of(Key.ReconnectFailed))
            else ChatActionResult.Unknown
        }
    }

    private suspend fun call(path: String, method: String, body: JsonObject = buildJsonObject {}): JsonObject =
        request(path, method, if (method == "GET") null else body.toString().toRequestBody("application/json".toMediaType())).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            val text = it.body?.string().orEmpty()
            if (text.isBlank()) buildJsonObject {} else JsonCodec.parseToJsonElement(text).jsonObject
        }

    private fun Exception.definitelyRejected(): Boolean = this is HubFailure && delivered != "unknown" &&
        (delivered == "none" || status in setOf(400, 401, 403, 404, 409, 410, 422, 429) ||
            kind in setOf(HubError.NoNetwork, HubError.PairingExpired))

    private fun api(vararg segments: String) = HttpUrl.Builder().scheme("https").host("example.com")
        .addPathSegment("api").addPathSegment("v1").apply { segments.forEach(::addPathSegment) }.build().encodedPath
}
