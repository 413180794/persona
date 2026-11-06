package persona.core.contracts

import java.io.File
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.*
import persona.core.contracts.api.DeliveryState
import org.junit.Assert.*
import org.junit.Test

class RealtimeContractTest {
    private val projection = Json(JsonCodec) { encodeDefaults = true }
    private fun shared(name: String): JsonArray = JsonCodec.parseToJsonElement(
        File(System.getProperty("persona.repo.root"), "spec/fixtures/wire/$name.json").readText(),
    ).jsonArray

    private fun sample(name: String): List<ServerEvent> = JsonCodec.parseToJsonElement(
        requireNotNull(javaClass.getResource("/wire/$name.json")).readText(),
    ).jsonArray.map { input ->
        val event = requireNotNull(ServerEvent.decode(input.toString()))
        val encoded = JsonCodec.parseToJsonElement(projection.encodeToString<ServerEvent>(event)).jsonObject
        // Every supplied protocol field must survive, including opaque tool argument keys.
        for ((key, value) in input.jsonObject) assertEquals("$name/$key", value, encoded[key])
        assertEquals(encoded, JsonCodec.parseToJsonElement(projection.encodeToString<ServerEvent>(
            requireNotNull(ServerEvent.decode(encoded.toString())),
        )))
        event
    }

    @Test fun decodesRealtimeSamplesAndSharedWirePayloads() {
        val events = sample("realtime")
        assertEquals("示例回复", ((events[0] as DeltaEvent).text).zhAny())
        assertFalse((events[0] as DeltaEvent).turnFinished)
        assertTrue((events[1] as DeltaEvent).turnFinished)
        assertEquals("working", ((events[2] as ScreenEvent).status).zhAny())
        assertEquals(JsonPrimitive(true), (events[3] as ToolCallEvent).args.jsonObject
            .getValue("custom_Key").jsonObject["keep_snake_case"])
        assertEquals("Stop", ((events[4] as HookEvent).event).zhAny())
        assertEquals(listOf("feed_example"), ((events[5] as LarkEvent).feedIds).zhAny())
        assertTrue(events[6] is InboxEvent)
        assertEquals(SessionKey("m1", "s1"), (events[7] as ErrorEvent).sessionKey)
        assertEquals(DeliveryOutcome.Unknown, (events[7] as ErrorEvent).body.deliveryOutcome)
        assertTrue(events[8] is PersonasEvent)
        assertEquals("aurora", (events[9] as LookEvent).id)

        val terminal = sample("terminal")
        assertArrayEquals(byteArrayOf(27, 91, 50, 74, 79, 75), (terminal[0] as TerminalEvent).frameBytes)
        assertFalse((terminal[0] as TerminalEvent).streamEnded)
        assertEquals("active", ((terminal[1] as TerminalControlEvent).state).zhAny())
        assertEquals("taken", ((terminal[2] as TerminalControlEvent).state).zhAny())
        assertTrue((terminal[3] as TerminalEvent).streamEnded)
        assertTrue((terminal[3] as TerminalEvent).unsupported)

        // Shared HTTP errors use the same flat body when sent over WebSocket.
        shared("errors").forEach { fixture ->
            val input = fixture.jsonObject.getValue("input").jsonObject
            val body = JsonCodec.decodeFromJsonElement<ErrorBody>(input)
            val event = ServerEvent.decode(JsonObject(input + ("type" to JsonPrimitive("error"))).toString()) as ErrorEvent
            assertEquals(body, event.body)
            assertNull(event.sessionKey)
            assertEquals(DeliveryOutcome.NotDelivered, event.body.deliveryOutcome)
        }
        // Reuse the existing sessions event golden, including envelope machine identity.
        shared("sessions").filter { it.jsonObject["decoder"]?.jsonPrimitive?.content == "event" }.forEach {
            val event = ServerEvent.decode(it.jsonObject.getValue("input").toString()) as SessionsEvent
            assertTrue(event.sessions.all { session -> session.machineId == event.machineId })
        }
        assertNull(ServerEvent.decode("""{"type":"future","items":false}"""))
        assertNull(ServerEvent.decode("""{"type":"delta","text":"missing identity"}"""))
        assertNull(ServerEvent.decode("""{"type":"tool_call","call_id":false,"name":"example"}"""))
        assertNull(ServerEvent.decode("""{"type":"terminal","machine_id":"m1","session_id":"s1","epoch":"e","seq":1,"data":"!"}"""))
        assertEquals("unknown", ((ServerEvent.decode("""{"type":"terminal_control","machine_id":"m1","session_id":"s1","state":"future"}""") as TerminalControlEvent).state).zhAny())
        val screen = ServerEvent.decode("""{"type":"screen","machine_id":"m1","session_id":"s1","status":"future","ts":false,"text":"保留"}""") as ScreenEvent
        assertEquals("unknown", (screen.status).zhAny())
        assertNull(screen.ts)
        assertEquals("保留", (screen.text).zhAny())
    }

    @Test fun mapsLifecycleAndUnknownResultsWithoutGuessingFromOfflineOrTimestamps() {
        val input = shared("sessions").first().jsonObject.getValue("input")
        val existing = JsonCodec.decodeFromJsonElement<SessionsResponse>(input).sessions.first()
        val working = existing.copy(status = "working", state = "working")
        fun state(list: List<Session>?, online: Boolean? = true, stop: Boolean = false) =
            SessionRuntimeState.map(working, list, online, stopRequested = stop)
        assertFalse(state(null).ended)
        assertFalse(state(emptyList(), false).ended)
        assertFalse(state(emptyList(), null).ended)
        assertFalse(state(listOf(working.copy(status = "offline"))).ended)
        assertFalse(state(listOf(working.copy(status = "done", state = "unread"))).ended)
        assertTrue(state(emptyList()).ended)
        val replacement = working.copy(sessionId = "s_new")
        val replaced = state(listOf(replacement))
        assertTrue(replaced.ended)
        assertTrue(replaced.replaced)
        assertFalse(state(listOf(working, replacement)).replaced)
        assertFalse(state(listOf(replacement.copy(machineId = "m_other"))).replaced)
        assertFalse(state(listOf(replacement.copy(spaceId = "other", paneId = "other"))).replaced)
        // another live agent of the same tab (a pane it split off, #2320) is a sibling, not the gone session's replacement
        assertEquals(0, existing.paneIndex)
        val sibling = replacement.copy(paneId = "w1:p2E", paneIndex = 1)
        assertFalse(state(listOf(sibling)).replaced)
        assertTrue(state(listOf(sibling)).ended) // gone from the full list all the same
        assertTrue(state(listOf(sibling, replacement)).replaced)
        // without pane ids the tab is all there is to go by
        assertTrue(state(listOf(replacement.copy(paneId = null))).replaced)
        assertTrue(state(listOf(working), stop = true).stopping)
        assertFalse(state(listOf(working.copy(status = "done")), stop = true).stopping)
        assertFalse(state(emptyList(), stop = true).stopping)
        assertTrue(SessionRuntimeState.map(working, listOf(working), true, closeConfirmed = true).ended)
        assertNull(state(listOf(working)).turnDurationSeconds)
        assertEquals(12L, SessionRuntimeState.map(working, listOf(working), true,
            recordedTurnDurationSeconds = 12).turnDurationSeconds)
        assertNull(SessionRuntimeState.map(working, listOf(working), true,
            recordedTurnDurationSeconds = -1).turnDurationSeconds)
        for (delivery in DeliveryState.entries) {
            val expected = delivery in listOf(DeliveryState.Unknown, DeliveryState.Verifying, DeliveryState.NeedsConfirmation)
            assertEquals(delivery.name, expected, delivery.resultUnknown)
            assertEquals(expected, SessionRuntimeState.map(working, listOf(working), true,
                deliveryStates = listOf(delivery)).resultUnknown)
        }
        assertEquals(DeliveryOutcome.Unknown, ErrorBody("示例").deliveryOutcome)
        assertEquals(DeliveryOutcome.Unknown, DeliveryOutcome.of("future"))
        assertEquals(DeliveryOutcome.Delivered, DeliveryOutcome.of("yes"))
    }
}
