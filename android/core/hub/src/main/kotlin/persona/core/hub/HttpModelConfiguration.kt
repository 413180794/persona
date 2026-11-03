package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import kotlinx.serialization.json.JsonObject
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/** 设置 › 模型配置 over the pairing's connection (docs/model-config.md「接口」); 404 and 409 mean different things here. */
class HttpModelConfiguration internal constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
) : ModelConfiguration {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body -> connection.request(path, method, body) })

    override suspend fun catalog(): ModelCatalog = try {
        JsonCodec.decodeFromString(call(path(), "GET"))
    } catch (failure: HubFailure) {
        throw if (failure.status == 404) ModelConfigurationUnsupported() else failure
    }

    override suspend fun purpose(id: String): ModelPurposeDetail = try {
        JsonCodec.decodeFromString(call(path(id), "GET"))
    } catch (failure: HubFailure) {
        throw missing(failure)
    }

    override suspend fun save(purpose: ModelPurpose, draft: ModelConfigurationDraft): ModelPurposeSave = try {
        ModelPurposeSave.Saved(JsonCodec.decodeFromString(call(path(purpose.id), "PATCH", draft.patch(purpose))))
    } catch (failure: HubFailure) {
        if (failure.status == 409) ModelPurposeSave.Conflict else throw missing(failure)
    }

    /** A hub without model configuration answers 404 from its router, without the JSON error of a missing purpose. */
    private fun missing(failure: HubFailure): Exception = when {
        failure.status != 404 -> failure
        failure.code == null && failure.message.startsWith("HTTP ") -> ModelConfigurationUnsupported()
        else -> ModelPurposeNotFound(failure.message)
    }

    private suspend fun call(path: String, method: String, body: JsonObject? = null): String =
        request(path, method, body?.toString()?.toRequestBody(json)).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            it.body?.string().orEmpty()
        }

    private fun path(vararg purpose: String) = HttpUrl.Builder().scheme("https").host("example.com")
        .addPathSegments("api/v1/model-config").apply {
            purpose.forEach { addPathSegment("purposes").addPathSegment(it) }
        }.build().encodedPath

    private companion object {
        val json = "application/json; charset=utf-8".toMediaType()
    }
}
