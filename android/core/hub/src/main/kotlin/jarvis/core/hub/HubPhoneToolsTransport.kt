package jarvis.core.hub

import jarvis.core.contracts.api.*
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.serialization.json.*

/** Feed accept from the socket text hook before ServerEvent.decode, which ignores tool_call. */
class HubPhoneToolsTransport(private val connection: HubConnection) : PhoneToolsTransport, AutoCloseable {
    private val queue = Channel<PhoneToolCall>(16)
    override val calls = queue.receiveAsFlow()

    fun accept(message: JsonObject): Boolean {
        if ((message["type"] as? JsonPrimitive)?.contentOrNull != "tool_call") return false
        val id = (message["call_id"] as? JsonPrimitive)?.takeIf { it.isString }?.content
            ?.takeIf { it.isNotBlank() } ?: return true
        val name = (message["name"] as? JsonPrimitive)?.takeIf { it.isString }?.content
        val args = message["args"] as? JsonObject
        if (name.isNullOrBlank() || args == null) {
            result(id, PhoneToolResult.text("工具参数格式不正确。", true))
        } else if (!queue.trySend(PhoneToolCall(id, name, args)).isSuccess) {
            result(id, PhoneToolResult.text("手机工具正在忙，请稍后重试。", true))
        }
        return true
    }

    override fun register(deviceId: String, deviceName: String, simulator: Boolean, tools: List<PhoneToolSpec>): Boolean {
        require(deviceId.isNotBlank())
        return connection.send(buildJsonObject {
            put("type", "register_phone"); put("device_id", deviceId); put("platform", "android")
            put("device_name", deviceName); put("simulator", simulator)
            put("tools", JsonArray(tools.map { it.toWire() }))
        })
    }

    override fun result(callId: String, result: PhoneToolResult) = connection.send(buildJsonObject {
        put("type", "tool_result"); put("call_id", callId)
        put("content", result.content); put("is_error", result.isError)
    })

    override fun discardPending() { while (queue.tryReceive().isSuccess) { /* Discard calls from a previous socket. */ } }

    override fun close() { queue.close() }
}
