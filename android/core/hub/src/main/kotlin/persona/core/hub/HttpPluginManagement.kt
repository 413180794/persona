package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.HubPlugin
import persona.core.contracts.api.PluginCatalog
import persona.core.contracts.api.PluginConfigRejected
import persona.core.contracts.api.PluginDetail
import persona.core.contracts.api.PluginListing
import persona.core.contracts.api.PluginManagement
import persona.core.contracts.api.PluginMarket
import persona.core.contracts.api.PluginPreview
import persona.core.contracts.api.PluginUiContext
import persona.core.contracts.api.PluginUiPlace
import persona.core.contracts.api.PluginUiResult
import persona.core.contracts.api.PluginUiUnsupported
import persona.core.contracts.api.PluginView
import kotlinx.serialization.KSerializer
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonObject
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/**
 * 设置 › 插件 over the pairing's connection (docs/plugins.md「hub 的接口」). The hub's own routes are under the
 * reserved `-` segment of a plugin (`/api/v1/plugins/{id}/-/…`); everything else there goes to the plugin. The hub-wide
 * ones (markets, the catalog, where plugins draw) are under `/api/v1/plugins/-/…`.
 */
class HttpPluginManagement internal constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
    override val available: Flow<Boolean> = flowOf(true),
) : PluginManagement {
    constructor(connection: OkHttpHubConnection, available: Flow<Boolean>) : this({ path, method, body ->
        connection.request(path, method, body)
    }, available)

    override suspend fun plugins(): List<HubPlugin> = listing().plugins

    override suspend fun listing(): PluginListing {
        val body = parse(call(path(), "GET")) as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val list = body["plugins"] as? JsonArray ?: throw HubFailure(HubError.Protocol)
        fun flag(name: String) = (body[name] as? JsonPrimitive)?.booleanOrNull == true
        return PluginListing(decode { JsonCodec.decodeFromJsonElement(ListSerializer(HubPlugin.serializer()), list) },
            canInstall = flag("can_install"), markets = flag("markets"))
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

    // ---- declarative UI (docs/plugins.md「声明式 UI」)

    override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?): List<PluginUiPlace> {
        val url = hubUrl("ui").addQueryParameter("placement", placement).apply {
            launcher?.takeIf { it.isNotEmpty() }?.let { addQueryParameter("launcher", it) }
            machine?.takeIf { it.isNotEmpty() }?.let { addQueryParameter("machine", it) }
        }.build()
        val list = (parse(call(url.encodedPath + "?" + url.encodedQuery, "GET")) as? JsonObject)?.get("ui") as? JsonArray
            ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(ListSerializer(PluginUiPlace.serializer()), list) }
    }

    override suspend fun renderUi(plugin: String, ui: String, context: PluginUiContext?, state: String?): PluginView {
        val body = parse(uiCall(plugin, ui, uiBody(context, state, null, null))) as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val view = body["view"] as? JsonObject ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(PluginView.serializer(), view) }
    }

    override suspend fun uiAction(
        plugin: String, ui: String, action: String, context: PluginUiContext?, state: String?, values: Map<String, String>?,
    ): PluginUiResult = read(uiCall(plugin, ui, uiBody(context, state, action, values)), PluginUiResult.serializer())

    private fun uiBody(context: PluginUiContext?, state: String?, action: String?, values: Map<String, String>?) = buildJsonObject {
        if (context != null && (context.launcher != null || context.machine != null)) putJsonObject("context") {
            context.launcher?.let { put("launcher", it) }
            context.machine?.let { put("machine", it) }
        }
        state?.takeIf { it.isNotEmpty() }?.let { put("state", it) }
        action?.let { put("action", it) }
        values?.let { v -> putJsonObject("values") { v.forEach { (key, value) -> put(key, value) } } }
    }

    /** A plugin that says the launcher's gateway is not its own answers 404 `unsupported`: the section is hidden. */
    private suspend fun uiCall(plugin: String, ui: String, body: JsonObject): String = try {
        call(path(plugin, "-", "ui", id(ui)), "POST", body)
    } catch (failure: HubFailure) {
        if (failure.status == 404 && failure.code == "unsupported") throw PluginUiUnsupported()
        throw failure
    }

    // ---- plugin markets (docs/plugins.md「插件市场」)

    override suspend fun markets(): List<PluginMarket> {
        val list = (parse(call(hubPath("markets"), "GET")) as? JsonObject)?.get("markets") as? JsonArray
            ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(ListSerializer(PluginMarket.serializer()), list) }
    }

    override suspend fun addMarket(spec: String, token: String?): PluginMarket =
        read(call(hubPath("markets"), "POST", buildJsonObject {
            put("spec", spec)
            token?.takeIf { it.isNotEmpty() }?.let { put("token", it) }
        }), PluginMarket.serializer())

    override suspend fun removeMarket(id: String) { call(hubPath("markets", id(id)), "DELETE") }

    override suspend fun refreshMarket(id: String): PluginMarket =
        // OkHttp sends no POST without a body: an empty object.
        read(call(hubPath("markets", id(id), "refresh"), "POST", JsonObject(emptyMap())), PluginMarket.serializer())

    override suspend fun setMarketToken(id: String, token: String): PluginMarket =
        read(call(hubPath("markets", id(id), "token"), "PUT", buildJsonObject { put("token", token) }), PluginMarket.serializer())

    override suspend fun catalog(query: String, market: String?, refresh: Boolean): PluginCatalog {
        val url = hubUrl("catalog").apply {
            query.trim().takeIf { it.isNotEmpty() }?.let { addQueryParameter("q", it) }
            market?.takeIf { it.isNotEmpty() }?.let { addQueryParameter("market", it) }
            if (refresh) addQueryParameter("refresh", "1")
        }.build()
        val path = url.encodedPath + (url.encodedQuery?.let { "?$it" } ?: "")
        return read(call(path, "GET"), PluginCatalog.serializer())
    }

    override suspend fun previewFromMarket(market: String, plugin: String): PluginPreview =
        read(call(hubPath("catalog", "preview"), "POST", buildJsonObject { put("market", market); put("plugin", plugin) }),
            PluginPreview.serializer())

    override suspend fun install(preview: PluginPreview): PluginDetail =
        detail(call(hubPath("install"), "POST", agreed(preview)))

    override suspend fun upgrade(id: String, preview: PluginPreview): PluginDetail =
        detail(call(path(id, "-", "upgrade"), "POST", agreed(preview)))

    override suspend fun uninstall(id: String, keepData: Boolean) {
        call(path(id, "-", "") + if (keepData) "?keep_data=1" else "", "DELETE")
    }

    /** The preview and the permissions the user saw and agreed to. */
    private fun agreed(preview: PluginPreview) = buildJsonObject {
        put("preview_id", preview.previewId)
        put("permissions_digest", preview.permissionsDigest)
    }

    private fun <T> read(body: String, serializer: KSerializer<T>): T = decode { JsonCodec.decodeFromJsonElement(serializer, parse(body)) }

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

    /** `/api/v1/plugins/-/…`: the hub's own routes for all plugins. */
    private fun hubUrl(vararg rest: String) = HttpUrl.Builder().scheme("https").host("example.com")
        .addPathSegments("api/v1/plugins").addPathSegment("-").apply { rest.forEach(::addPathSegment) }

    private fun hubPath(vararg rest: String): String = hubUrl(*rest).build().encodedPath

    // Market and UI ids come from the hub; anything else must not become a path.
    private fun id(value: String): String = value.also { require(it.matches(PluginId)) { "Invalid id" } }

    // Secret names come from the plugin's manifest; anything else must not become a path ("..").
    private fun secret(name: String): String = name.also { require(it.matches(SecretName)) { "Invalid secret name" } }

    private companion object {
        val json = "application/json; charset=utf-8".toMediaType()
        val PluginId = Regex("[A-Za-z0-9_-]{1,64}")
        val SecretName = Regex("[A-Za-z][A-Za-z0-9_]{0,63}")
    }
}
