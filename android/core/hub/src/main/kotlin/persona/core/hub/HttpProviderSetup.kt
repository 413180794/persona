package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.NewProvider
import persona.core.contracts.api.ProviderPreset
import persona.core.contracts.api.ProviderSetup
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.jsonObject
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/** 新增模型供应商 over the pairing's connection (docs/launchers.md「接口」). */
class HttpProviderSetup internal constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
) : ProviderSetup {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body -> connection.request(path, method, body) })

    // A hub from before the extension base has no such route (its GET /launchers/{name} answers 404 for "presets").
    override suspend fun presets(): List<ProviderPreset> = try {
        val presets = JsonCodec.parseToJsonElement(call("/api/v1/launchers/presets", "GET")).jsonObject["presets"] as? JsonArray
        presets?.let { JsonCodec.decodeFromJsonElement(ListSerializer(ProviderPreset.serializer()), it) }.orEmpty()
    } catch (failure: HubFailure) {
        if (failure.status == 404) emptyList() else throw failure
    }

    override suspend fun add(provider: NewProvider) {
        call("/api/v1/launchers", "POST", JsonCodec.encodeToString(NewProvider.serializer(), provider).toRequestBody(json))
    }

    private suspend fun call(path: String, method: String, body: RequestBody? = null): String =
        request(path, method, body).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            it.body?.string().orEmpty()
        }

    private companion object {
        val json = "application/json; charset=utf-8".toMediaType()
    }
}
