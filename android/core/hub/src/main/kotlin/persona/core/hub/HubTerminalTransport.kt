package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.serialization.json.*
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody

class HubTerminalTransport(private val hub: OkHttpHubConnection) : TerminalTransport {
    override val connection = hub.state
    override val features = hub.serverFeatures
    override val events = hub.events

    override fun subscribe(key: SessionKey, epoch: String?, after: Int, size: TerminalSize?, takeover: Boolean) =
        hub.send(subscribeMessage(key, epoch, after, size, takeover))

    override fun unsubscribe(key: SessionKey) { hub.send(terminalMessage("terminal_unsubscribe", key)) }

    override fun input(key: SessionKey, text: String, size: TerminalSize?, scroll: Int,
                       at: TerminalCellPosition?, click: TerminalCellPosition?) = hub.send(inputMessage(key, text, size, scroll, at, click))

    override suspend fun screen(key: SessionKey): TerminalScreen {
        val url = HttpUrl.Builder().scheme("https").host("example.com").addPathSegment("api").addPathSegment("v1")
            .addPathSegment("sessions").addPathSegment(key.machineId).addPathSegment(key.sessionId).addPathSegment("screen")
            .addQueryParameter("lines", "200").addQueryParameter("source", "recent").addQueryParameter("format", "ansi").build()
        return hub.request(url.encodedPath + "?" + url.encodedQuery).use {
            JsonCodec.decodeFromString<TerminalScreen>(it.body!!.string())
        }
    }

    override suspend fun keys(key: SessionKey, keys: List<String>, text: String) {
        // `/keys` is a session's: a pane address reaches only the terminal stream and the screen read (docs/protocol/protocol.md).
        if (key.isPaneAddress) throw UnsupportedOperationException("A terminal opened by pane takes keys only while driven")
        val (path, body) = keysRequest(key, keys, text)
        hub.request(path, "POST", body.toString().toRequestBody("application/json".toMediaType())).close()
    }

}

/**
 * A terminal stream message. [key]'s session id goes as `session_id` as it is: a session's id, or a pane's address
 * (`pane:<pane id>`, [TerminalAddress]) for a pane with no session, which the hub takes in its place.
 */
internal fun terminalMessage(type: String, key: SessionKey, body: JsonObjectBuilder.() -> Unit = {}) = buildJsonObject {
    put("type", type); put("machine_id", key.machineId); put("session_id", key.sessionId); body()
}

internal fun subscribeMessage(key: SessionKey, epoch: String?, after: Int, size: TerminalSize?, takeover: Boolean) =
    terminalMessage("terminal_subscribe", key) {
        epoch?.let { put("epoch", it); put("after_seq", after) }
        size?.let { grid(it) }
        if (takeover) put("takeover", true)
    }

internal fun inputMessage(key: SessionKey, text: String, size: TerminalSize?, scroll: Int,
                          at: TerminalCellPosition?, click: TerminalCellPosition?) = terminalMessage("terminal_input", key) {
    if (text.isNotEmpty()) put("text", text)
    size?.let { grid(it) }
    if (scroll != 0) put("scroll", scroll)
    at?.let { putJsonArray("at") { add(it.column); add(it.row) } }
    click?.let { putJsonArray("click") { add(it.column); add(it.row) } }
}

private fun JsonObjectBuilder.grid(size: TerminalSize) = putJsonObject("control") {
    put("cols", size.cols); put("rows", size.rows)
}

/** `POST /api/v1/sessions/{m}/{s}/keys`: the path (segments encoded) and `{"keys":[…],"text":"…"}`. */
internal fun keysRequest(key: SessionKey, keys: List<String>, text: String): Pair<String, JsonObject> {
    val url = HttpUrl.Builder().scheme("https").host("example.com").addPathSegment("api").addPathSegment("v1")
        .addPathSegment("sessions").addPathSegment(key.machineId).addPathSegment(key.sessionId).addPathSegment("keys").build()
    return url.encodedPath to buildJsonObject {
        putJsonArray("keys") { keys.forEach { add(it) } }
        put("text", text)
    }
}
