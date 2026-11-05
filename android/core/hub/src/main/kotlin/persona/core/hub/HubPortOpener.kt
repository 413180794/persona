package persona.core.hub

import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import persona.core.contracts.api.PortGone
import persona.core.contracts.api.PortOpener
import persona.core.contracts.api.PortProbe
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/**
 * [PortOpener] on the active pairing: `…/open` through [connection], the bytes through [forwarder]. [close] when the
 * group's services are torn down (re-pair, unpair) frees its loopback ports.
 */
class HubPortOpener(
    private val connection: OkHttpHubConnection,
    // Each connection to a forwarded port follows the live route with the pairing's pinned client and token.
    private val forwarder: PortForwarder = PortForwarder(connection.http.client) {
        val route = connection.route.value ?: throw HubFailure(HubError.NoNetwork)
        route.endpoint.baseUrl() to connection.token
    },
) : PortOpener, java.io.Closeable {
    override suspend fun open(machineId: String, port: Int, path: String, secure: Boolean, listed: Boolean): String {
        if (!listed) {
            try {
                connection.request("/api/v1/machines/${encode(machineId)}/ports/$port/open", "POST", "{}".toRequestBody(JSON)).close()
            } catch (failure: HubFailure) {
                throw if (failure.status == 404) PortGone(port) else failure
            }
        }
        val local = forwarder.localPort(PortForwarder.Key(machineId, port))
        return "${if (secure) "https" else "http"}://localhost:$local$path"
    }

    override suspend fun probe(machineId: String, port: Int): PortProbe =
        connection.request("/api/v1/machines/${encode(machineId)}/ports/$port").use { response ->
            val o = persona.core.contracts.JsonCodec.parseToJsonElement(response.body?.string().orEmpty()).jsonObject
            PortProbe(o["listening"]?.jsonPrimitive?.booleanOrNull == true, o["title"]?.jsonPrimitive?.contentOrNull?.takeIf(String::isNotBlank))
        }

    override fun close() = forwarder.close()

    private fun encode(segment: String) = java.net.URLEncoder.encode(segment, "UTF-8").replace("+", "%20")

    private companion object { val JSON = "application/json".toMediaType() }
}
