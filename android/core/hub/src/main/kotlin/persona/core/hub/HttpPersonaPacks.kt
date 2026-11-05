package persona.core.hub

import okhttp3.HttpUrl
import okhttp3.Response
import persona.core.contracts.JsonCodec
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import persona.core.contracts.api.LookCurrent
import persona.core.contracts.api.LookListing
import persona.core.contracts.api.PersonaInfo
import persona.core.contracts.api.PersonaListing
import persona.core.contracts.api.PersonaPacks
import persona.core.contracts.api.PersonaPacksUnsupported

/** GET /api/v1/personas and its files, and the looks (GET /api/v1/looks, PUT …/current), on the active pairing's authenticated route. */
class HttpPersonaPacks internal constructor(
    /** PUT with a JSON body (the choice). */
    private val send: suspend (String, String) -> Response = { _, _ -> throw HubFailure(HubError.Server, "HTTP 404", status = 404) },
    private val request: suspend (String) -> Response,
) : PersonaPacks {
    constructor(connection: OkHttpHubConnection) : this(
        { path, json -> connection.request(path, "PUT", json.toRequestBody("application/json".toMediaType())) },
        { path -> connection.request(path, "GET", null) },
    )

    override suspend fun looks(): LookListing = try {
        request("/api/v1/looks").use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            JsonCodec.decodeFromString<LookListing>(it.body?.string().orEmpty())
        }
    } catch (failure: HubFailure) {
        throw if (failure.status == 404) PersonaPacksUnsupported() else failure
    }

    override suspend fun choose(id: String, ifUnset: Boolean): LookCurrent = try {
        val body = buildJsonObject { put("id", id); if (ifUnset) put("if_unset", true) }.toString()
        send("/api/v1/looks/current", body).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            JsonCodec.decodeFromString<LookCurrent>(it.body?.string().orEmpty())
        }
    } catch (failure: HubFailure) {
        throw if (failure.status == 404) PersonaPacksUnsupported() else failure
    }

    override suspend fun list(): List<PersonaInfo> = try {
        request("/api/v1/personas").use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            JsonCodec.decodeFromString<PersonaListing>(it.body?.string().orEmpty()).personas
        }
    } catch (failure: HubFailure) {
        // The connection throws for any non-2xx answer; an older hub has no such route.
        throw if (failure.status == 404) PersonaPacksUnsupported() else failure
    }

    override suspend fun file(id: String, file: String): ByteArray {
        val path = HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1/personas")
            .addPathSegment(id).addPathSegments(file).build().encodedPath
        return request(path).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            it.body?.bytes() ?: ByteArray(0)
        }
    }
}
