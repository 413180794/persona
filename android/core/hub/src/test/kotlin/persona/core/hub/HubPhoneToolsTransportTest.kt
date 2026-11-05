package persona.core.hub

import persona.core.contracts.ServerEvent
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.PhoneToolCall
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test

class HubPhoneToolsTransportTest {
    private class Socket : HubConnection {
        val sent = mutableListOf<JsonObject>()
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject): Boolean { sent += message; return true }
    }

    @Test fun callsWithoutArgumentsAreEmptyObjectsAndOtherShapesAreRefused() = runBlocking {
        val socket = Socket()
        val transport = HubPhoneToolsTransport(socket)
        fun call(id: String, args: JsonElement?) = buildJsonObject {
            put("type", "tool_call"); put("call_id", id); put("name", "phone_device_info"); args?.let { put("args", it) }
        }
        assertTrue(transport.accept(call("missing", null)))
        assertEquals(PhoneToolCall("missing", "phone_device_info", JsonObject(emptyMap())), transport.calls.first())
        assertTrue(transport.accept(call("null", JsonNull)))
        assertEquals(PhoneToolCall("null", "phone_device_info", JsonObject(emptyMap())), transport.calls.first())
        assertTrue(transport.accept(call("array", JsonArray(emptyList()))))
        val refusal = socket.sent.single()
        assertEquals("array", (refusal.getValue("call_id").jsonPrimitive.content).zhAny())
        assertTrue(refusal.getValue("is_error").jsonPrimitive.boolean)
        assertFalse(transport.accept(buildJsonObject { put("type", "sessions") }))
    }
}
