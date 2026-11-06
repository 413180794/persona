package persona.core.hub

import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.AgentTriage
import persona.core.contracts.api.TriageSwitches
import persona.core.contracts.api.TriageUnsupported

/** GET / PUT /api/v1/agents/triage (Settings › Session triage), on the active pairing's authenticated route. */
class HttpAgentTriage internal constructor(private val request: suspend (String, RequestBody?) -> Response) : AgentTriage {
    constructor(connection: OkHttpHubConnection) : this({ method, body -> connection.request(Path, method, body) })

    override suspend fun switches() = call("GET", null)

    override suspend fun put(value: TriageSwitches) =
        call("PUT", JsonCodec.encodeToString(TriageSwitches.serializer(), value).toRequestBody("application/json".toMediaType()))

    private suspend fun call(method: String, body: RequestBody?): TriageSwitches = try {
        request(method, body).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            JsonCodec.decodeFromString(TriageSwitches.serializer(), it.body?.string().orEmpty())
        }
    } catch (failure: HubFailure) {
        // The connection throws for any non-2xx answer; an older hub has no such route.
        throw if (failure.status == 404) TriageUnsupported() else failure
    }

    private companion object {
        const val Path = "/api/v1/agents/triage"
    }
}
