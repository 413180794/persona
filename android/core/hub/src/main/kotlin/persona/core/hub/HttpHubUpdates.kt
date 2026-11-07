package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.HubUpdateRecord
import persona.core.contracts.api.HubUpdateStatus
import persona.core.contracts.api.HubUpdates
import persona.core.contracts.api.HubUpdatesUnsupported
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/** Settings › Hub version over the pairing's connection (docs/operations/hub-update.md "In the app", `/api/v1/hub/update*`). */
class HttpHubUpdates internal constructor(
    private val request: suspend (path: String, method: String, body: RequestBody?) -> Response,
) : HubUpdates {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body -> connection.request(path, method, body) })

    override suspend fun status(): HubUpdateStatus = try { read(call(Base, "GET"), HubUpdateStatus.serializer()) }
    catch (failure: HubFailure) {
        // A hub from before updates in the app has no such route: its mux answers 404.
        if (failure.status == 404) throw HubUpdatesUnsupported()
        throw failure
    }

    // OkHttp sends no POST without a body: an empty object.
    override suspend fun check() = read(call("$Base/check", "POST", JsonObject(emptyMap())), HubUpdateStatus.serializer())

    override suspend fun start(now: Boolean) =
        read(call(Base, "POST", buildJsonObject { if (now) put("now", true) }), HubUpdateStatus.serializer())

    override suspend fun settings(auto: Boolean?, noticeSeen: Boolean?) = read(call("$Base/settings", "PUT", buildJsonObject {
        auto?.let { put("auto", it) }
        noticeSeen?.let { put("auto_notice_seen", it) }
    }), HubUpdateStatus.serializer())

    override suspend fun history(): List<HubUpdateRecord> {
        val list = read(call("$Base/history", "GET"), JsonObject.serializer())["history"]
        return if (list == null || list is JsonNull) emptyList() else decode { JsonCodec.decodeFromJsonElement(ListSerializer(HubUpdateRecord.serializer()), list) }
    }

    private fun <T> read(body: String, serializer: KSerializer<T>): T = decode { JsonCodec.decodeFromString(serializer, body) }

    /** Decoding errors are the hub's protocol, not the network's. */
    private inline fun <T> decode(block: () -> T): T = try { block() }
    catch (error: SerializationException) { throw HubFailure(HubError.Protocol, cause = error) }
    catch (error: IllegalArgumentException) { throw HubFailure(HubError.Protocol, cause = error) }

    // The pairing's connection throws a HubFailure for a status that is not 2xx; a bare request returns it.
    private suspend fun call(path: String, method: String, body: JsonObject? = null): String =
        request(path, method, body?.toString()?.toRequestBody(Json)).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string().orEmpty()
        }

    private companion object {
        const val Base = "/api/v1/hub/update"
        val Json = "application/json; charset=utf-8".toMediaType()
    }
}
