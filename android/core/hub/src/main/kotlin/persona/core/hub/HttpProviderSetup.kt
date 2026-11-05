package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.NewProvider
import persona.core.contracts.api.ProviderPreset
import persona.core.contracts.api.ProviderSetup
import persona.core.contracts.api.HubLauncher
import persona.core.contracts.api.ImportedProvider
import persona.core.contracts.api.LauncherCheck
import persona.core.contracts.api.LauncherCheckAnswer
import persona.core.contracts.api.LauncherImportResult
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import okhttp3.HttpUrl
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

    override suspend fun launchers(): List<HubLauncher> {
        val list = JsonCodec.parseToJsonElement(call("/api/v1/launchers", "GET")).jsonObject["launchers"] as? JsonArray
        return list?.let { JsonCodec.decodeFromJsonElement(ListSerializer(HubLauncher.serializer()), it) }.orEmpty()
    }

    override suspend fun launcher(name: String): HubLauncher {
        val one = JsonCodec.parseToJsonElement(call(path(name), "GET")).jsonObject["launcher"] ?: throw HubFailure(HubError.Protocol)
        return JsonCodec.decodeFromJsonElement(HubLauncher.serializer(), one)
    }

    override suspend fun remove(name: String) { call(path(name), "DELETE") }

    override suspend fun restore(name: String) { call(path(name, "restore"), "POST", "{}".toRequestBody(json)) }

    override suspend fun check(name: String, machine: String?): LauncherCheckAnswer {
        val url = HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1/launchers").addPathSegment(name)
            .addPathSegment("check").apply { machine?.takeIf { it.isNotBlank() }?.let { addQueryParameter("machine", it) } }.build()
        val target = url.encodedPath + (url.encodedQuery?.let { "?$it" } ?: "")
        val answer = JsonCodec.parseToJsonElement(call(target, "POST", "{}".toRequestBody(json))).jsonObject
        val check = answer["check"] ?: throw HubFailure(HubError.Protocol)
        return LauncherCheckAnswer((answer["machine"] as? JsonPrimitive)?.contentOrNull?.takeIf { it.isNotBlank() },
            JsonCodec.decodeFromJsonElement(LauncherCheck.serializer(), check))
    }

    override suspend fun importPreview(machine: String): List<ImportedProvider> {
        val url = HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1/launchers/import")
            .addQueryParameter("machine", machine).build()
        val list = JsonCodec.parseToJsonElement(call("${url.encodedPath}?${url.encodedQuery}", "GET")).jsonObject["providers"] as? JsonArray
        return list?.let { JsonCodec.decodeFromJsonElement(ListSerializer(ImportedProvider.serializer()), it) }.orEmpty()
    }

    override suspend fun import(machine: String, providers: List<String>): LauncherImportResult {
        val body = buildJsonObject {
            put("machine", machine)
            put("providers", JsonArray(providers.map(::JsonPrimitive)))
        }
        return JsonCodec.decodeFromString(LauncherImportResult.serializer(), call("/api/v1/launchers/import", "POST", body.toString().toRequestBody(json)))
    }

    private fun path(name: String, vararg more: String): String {
        require(name.isNotBlank())
        return HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1/launchers").addPathSegment(name)
            .apply { more.forEach(::addPathSegment) }.build().encodedPath
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
