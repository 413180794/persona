package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.HubPlugin
import persona.core.contracts.api.PluginConfigRejected
import persona.core.contracts.api.PluginDetail
import persona.core.contracts.api.PluginManagement
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/**
 * 设置 › 插件 over the pairing's connection (docs/plugins.md「hub 的接口」). The hub's own routes are under the
 * reserved `-` segment of a plugin (`/api/v1/plugins/{id}/-/…`); everything else there goes to the plugin.
 */
class HttpPluginManagement internal constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
    override val available: Flow<Boolean> = flowOf(true),
) : PluginManagement {
    constructor(connection: OkHttpHubConnection, available: Flow<Boolean>) : this({ path, method, body ->
        connection.request(path, method, body)
    }, available)

    override suspend fun plugins(): List<HubPlugin> {
        val list = (parse(call(path(), "GET")) as? JsonObject)?.get("plugins") as? JsonArray
            ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(ListSerializer(HubPlugin.serializer()), list) }
    }

    override suspend fun plugin(id: String) = detail(call(path(id, "-", ""), "GET"))

    override suspend fun setEnabled(id: String, enabled: Boolean) =
        detail(call(path(id, "-", "enabled"), "PUT", buildJsonObject { put("enabled", enabled) }))

    override suspend fun setConfig(id: String, config: JsonObject) =
        detail(call(path(id, "-", "config"), "PUT", buildJsonObject { put("config", config) }))

    override suspend fun setSecret(id: String, name: String, value: String) =
        detail(call(path(id, "-", "secrets", secret(name)), "PUT", buildJsonObject { put("value", value) }))

    override suspend fun clearSecret(id: String, name: String) =
        detail(call(path(id, "-", "secrets", secret(name)), "DELETE"))

    private fun detail(body: String): PluginDetail = decode { PluginDetail.decode(parse(body)) }

    private fun parse(body: String): JsonElement = try { JsonCodec.parseToJsonElement(body) }
    catch (error: SerializationException) { throw HubFailure(HubError.Protocol, cause = error) }

    /** Decoding errors are the hub's protocol, not the network's. */
    private inline fun <T> decode(block: () -> T): T = try { block() }
    catch (error: IllegalArgumentException) { throw HubFailure(HubError.Protocol, cause = error) }

    // The pairing's connection throws a HubFailure for a status that is not 2xx; a bare request returns it.
    private suspend fun call(path: String, method: String, body: JsonObject? = null): String = try {
        request(path, method, body?.toString()?.toRequestBody(json)).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string().orEmpty()
        }
    } catch (failure: HubFailure) {
        throw rejected(failure) ?: failure
    }

    /** 400 `{code: "invalid_config", field, message}`: the field the hub refused and why. */
    private fun rejected(failure: HubFailure): PluginConfigRejected? =
        if (failure.status == 400 && failure.code == "invalid_config") {
            PluginConfigRejected(failure.field.orEmpty(), failure.detail?.takeIf { it.isNotBlank() } ?: failure.message)
        } else null

    /** `/api/v1/plugins[/id/…]`; an empty last segment keeps the trailing slash the detail route has. */
    private fun path(id: String? = null, vararg rest: String): String {
        if (id != null) require(id.matches(PluginId)) { "Invalid plugin id" }
        return HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1/plugins").apply {
            if (id != null) addPathSegment(id)
            rest.forEach(::addPathSegment)
        }.build().encodedPath
    }

    // Secret names come from the plugin's manifest; anything else must not become a path ("..").
    private fun secret(name: String): String = name.also { require(it.matches(SecretName)) { "Invalid secret name" } }

    private companion object {
        val json = "application/json; charset=utf-8".toMediaType()
        val PluginId = Regex("[A-Za-z0-9_-]{1,64}")
        val SecretName = Regex("[A-Za-z][A-Za-z0-9_]{0,63}")
    }
}
