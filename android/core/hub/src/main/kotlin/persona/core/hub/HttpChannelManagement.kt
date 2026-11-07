package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.BindPrompt
import persona.core.contracts.api.ChannelChange
import persona.core.contracts.api.ChannelDetail
import persona.core.contracts.api.ChannelManagement
import persona.core.contracts.api.ChannelNotFound
import persona.core.contracts.api.ChannelReadOnly
import persona.core.contracts.api.ChannelRejected
import persona.core.contracts.api.ChannelsUnsupported
import persona.core.contracts.api.HubChannel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonObject
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/**
 * Settings › Extensions › Channels over the pairing's connection (docs/extensions/channels.md "API"). The hub answers a binding step and a test
 * message only after asking the chat app (a QR code to fetch, a message to send): those two wait longer than the
 * client's usual read timeout.
 */
class HttpChannelManagement internal constructor(
    private val request: suspend (path: String, method: String, body: RequestBody?, readTimeoutSeconds: Long?) -> Response,
    override val available: Flow<Boolean> = flowOf(true),
) : ChannelManagement {
    constructor(connection: OkHttpHubConnection, available: Flow<Boolean>) : this({ path, method, body, timeout ->
        connection.request(path, method, body, readTimeoutSeconds = timeout)
    }, available)

    override suspend fun channels(): List<HubChannel> {
        val text = try { call(path(), "GET") } catch (failure: HubFailure) {
            // A hub from before channels has no such route: its mux answers a bare 404.
            if (failure.status == 404) throw ChannelsUnsupported()
            throw failure
        }
        val list = (parse(text) as? JsonObject)?.get("channels") as? JsonArray ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(ListSerializer(HubChannel.serializer()), list) }
    }

    override suspend fun channel(id: String) = detail(call(path(id), "GET"))

    override suspend fun update(id: String, change: ChannelChange) = detail(call(path(id), "PUT", buildJsonObject {
        change.enabled?.let { put("enabled", it) }
        change.target?.let { target ->
            putJsonObject("target") {
                put("kind", target.kind)
                target.machineId?.takeIf { it.isNotEmpty() }?.let { put("machine_id", it) }
                target.sessionId?.takeIf { it.isNotEmpty() }?.let { put("session_id", it) }
            }
        }
        if (change.features.isNotEmpty()) putJsonObject("features") { change.features.forEach { (key, on) -> put(key, on) } }
    }))

    override suspend fun bind(id: String, step: String?, values: Map<String, String>?): BindPrompt =
        read(call(path(id, "bind"), "POST", buildJsonObject {
            step?.takeIf { it.isNotEmpty() }?.let { put("step", it) }
            values?.let { v -> putJsonObject("values") { v.forEach { (key, value) -> put(key, value) } } }
        }, SlowSeconds), BindPrompt.serializer())

    override suspend fun unbind(id: String) = detail(call(path(id, "binding"), "DELETE"))

    // OkHttp sends no POST without a body: an empty object.
    override suspend fun test(id: String) { call(path(id, "test"), "POST", JsonObject(emptyMap()), SlowSeconds) }

    private fun <T> read(body: String, serializer: KSerializer<T>): T = decode { JsonCodec.decodeFromJsonElement(serializer, parse(body)) }

    private fun detail(body: String): ChannelDetail = decode { ChannelDetail.decode(parse(body)) }

    private fun parse(body: String): JsonElement = try { JsonCodec.parseToJsonElement(body) }
    catch (error: SerializationException) { throw HubFailure(HubError.Protocol, cause = error) }

    /** Decoding errors are the hub's protocol, not the network's. */
    private inline fun <T> decode(block: () -> T): T = try { block() }
    catch (error: IllegalArgumentException) { throw HubFailure(HubError.Protocol, cause = error) }

    // The pairing's connection throws a HubFailure for a status that is not 2xx; a bare request returns it.
    private suspend fun call(path: String, method: String, body: JsonObject? = null, timeout: Long? = null): String = try {
        request(path, method, body?.toString()?.toRequestBody(json), timeout).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string().orEmpty()
        }
    } catch (failure: HubFailure) {
        throw refused(failure) ?: failure
    }

    /** The hub's refusals that the page words itself: a value it will not take, a channel set up elsewhere, none such. */
    private fun refused(failure: HubFailure): Exception? = when {
        failure.status == 400 && failure.code == "invalid" -> ChannelRejected(failure.message)
        failure.status == 409 && failure.code == "read_only" -> ChannelReadOnly(failure.message)
        failure.status == 404 && failure.code == "not_found" -> ChannelNotFound()
        else -> null
    }

    /** `/api/v1/channels[/id[/…]]`; channel ids are checked so that none becomes another path. */
    private fun path(id: String? = null, vararg rest: String): String {
        if (id != null) require(id.matches(ChannelId)) { "Invalid channel id" }
        return HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1/channels").apply {
            if (id != null) addPathSegment(id)
            rest.forEach(::addPathSegment)
        }.build().encodedPath
    }

    private companion object {
        val json = "application/json; charset=utf-8".toMediaType()
        // docs/extensions/channels.md: ^[a-z][a-z0-9_]*$, at most 32 for an extension's.
        val ChannelId = Regex("[a-z][a-z0-9_]{0,63}")
        /** A bind step may fetch a QR code from the chat app, a test message waits on it (the hub gives it 30 s). */
        const val SlowSeconds = 40L
    }
}
