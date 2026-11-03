package persona.core.hub

import okhttp3.HttpUrl
import okhttp3.Response
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.PersonaInfo
import persona.core.contracts.api.PersonaListing
import persona.core.contracts.api.PersonaPacks
import persona.core.contracts.api.PersonaPacksUnsupported

/** GET /api/v1/personas and its files, on the active pairing's authenticated route. */
class HttpPersonaPacks internal constructor(private val request: suspend (String) -> Response) : PersonaPacks {
    constructor(connection: OkHttpHubConnection) : this({ path -> connection.request(path, "GET", null) })

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
