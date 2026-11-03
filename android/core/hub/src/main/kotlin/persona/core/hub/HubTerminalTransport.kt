package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.serialization.json.*
import okhttp3.HttpUrl

class HubTerminalTransport(private val hub: OkHttpHubConnection) : TerminalTransport {
    override val connection = hub.state
    override val features = hub.serverFeatures
    override val events = hub.events

    override fun subscribe(key: SessionKey, epoch: String?, after: Int, size: TerminalSize?, takeover: Boolean) =
        hub.send(message("terminal_subscribe", key) {
            epoch?.let { put("epoch", it); put("after_seq", after) }
            size?.let { grid(it) }
            if (takeover) put("takeover", true)
        })

    override fun unsubscribe(key: SessionKey) { hub.send(message("terminal_unsubscribe", key)) }

    override fun input(key: SessionKey, text: String, size: TerminalSize?, scroll: Int,
                       at: TerminalCellPosition?, click: TerminalCellPosition?) = hub.send(message("terminal_input", key) {
        if (text.isNotEmpty()) put("text", text)
        size?.let { grid(it) }
        if (scroll != 0) put("scroll", scroll)
        at?.let { putJsonArray("at") { add(it.column); add(it.row) } }
        click?.let { putJsonArray("click") { add(it.column); add(it.row) } }
    })

    override suspend fun screen(key: SessionKey): TerminalScreen {
        val url = HttpUrl.Builder().scheme("https").host("example.com").addPathSegment("api").addPathSegment("v1")
            .addPathSegment("sessions").addPathSegment(key.machineId).addPathSegment(key.sessionId).addPathSegment("screen")
            .addQueryParameter("lines", "200").addQueryParameter("source", "recent").addQueryParameter("format", "ansi").build()
        return hub.request(url.encodedPath + "?" + url.encodedQuery).use {
            JsonCodec.decodeFromString<TerminalScreen>(it.body!!.string())
        }
    }

    private fun message(type: String, key: SessionKey, body: JsonObjectBuilder.() -> Unit = {}) = buildJsonObject {
        put("type", type); put("machine_id", key.machineId); put("session_id", key.sessionId); body()
    }
    private fun JsonObjectBuilder.grid(size: TerminalSize) = putJsonObject("control") {
        put("cols", size.cols); put("rows", size.rows)
    }
}
