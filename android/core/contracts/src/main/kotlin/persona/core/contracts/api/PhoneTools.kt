package persona.core.contracts.api

import kotlinx.coroutines.flow.Flow
import kotlinx.serialization.json.*

data class PhoneToolSpec(val name: String, val description: String, val inputSchema: JsonObject) {
    fun toWire() = buildJsonObject {
        put("name", name); put("description", description); put("input_schema", inputSchema)
    }
}

data class PhoneToolCall(val callId: String, val name: String, val args: JsonObject)

data class PhoneToolResult(val content: JsonArray, val isError: Boolean = false) {
    companion object {
        fun text(text: String, isError: Boolean = false) = PhoneToolResult(buildJsonArray {
            add(buildJsonObject { put("type", "text"); put("text", text) })
        }, isError)
    }
}

/** Bound to the same authenticated connection and pairing generation as HubConnection. */
interface PhoneToolsTransport {
    val calls: Flow<PhoneToolCall>
    fun discardPending()
    fun register(deviceId: String, deviceName: String, simulator: Boolean, tools: List<PhoneToolSpec>): Boolean
    fun result(callId: String, result: PhoneToolResult): Boolean
}
