package persona.core.demo

import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.hub.HubFailure
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.*
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test
import java.time.Instant

@OptIn(ExperimentalCoroutinesApi::class)
class DemoHubTest {
    private val fixedNow = Instant.parse("2030-01-02T12:00:00Z")
    private fun asset() = checkNotNull(javaClass.classLoader?.getResourceAsStream("demo/Demo.json"))
        .bufferedReader().use { it.readText() }

    @Test fun packagedDataLocalChatAndReconnectUseExistingContracts() = runTest {
        val source = asset()
        val hub = DemoHub(source, backgroundScope, { fixedNow })
        val events = mutableListOf<ServerEvent>()
        val notices = mutableListOf<String>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { hub.events.collect { events += it } }
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { hub.notices.collect { notices += it } }
        assertTrue(hub.machines().isNotEmpty())
        assertTrue(hub.sessions().any { it.assistant })
        val resolved = DemoData.load(source, fixedNow)
        assertFalse(resolved.toString().contains("@rel:"))
        assertFalse(resolved.toString().contains("@unix:"))
        assertEquals(fixedNow.toString(), hub.machines().first().lastSeen)
        assertTrue(resolved.getValue("machines").jsonArray.first().jsonObject
            .getValue("system").jsonObject.getValue("boot_time").jsonPrimitive.long < fixedNow.epochSecond)
        hub.sessions().forEach { session ->
            val page = hub.items(session.key, ItemsQuery())
            assertEquals(page.items.map { it.seq }.sorted().distinct(), page.items.map { it.seq })
            page.items.forEach { item ->
                if (item.fullSize != null) assertTrue(hub.fullItem(session.key, item.id).text!!.length >= item.text.orEmpty().length)
                if (item.image?.data?.isNotEmpty() == true) assertTrue(hub.image(session.key, item.id).isNotEmpty())
            }
        }
        assertTrue(hub.inbox().getValue("items").jsonArray.isNotEmpty())
        resolved.getValue("lark").jsonObject.keys.forEach { feed ->
            val messages = hub.messages(feed, null, null, null).getValue("messages").jsonArray
            assertTrue(messages.isNotEmpty())
            val last = messages.last().jsonObject.getValue("position").jsonPrimitive.content
            assertTrue(hub.messages(feed, null, null, last).getValue("messages").jsonArray.isEmpty())
        }
        hub.pins()
        hub.mutes()

        val conversation = JsonCodec.decodeFromJsonElement<Session>(hub.createConversation(true).getValue("session"))
        assertEquals(conversation.sessionId, hub.assistant().getValue("main").jsonPrimitive.content)
        val key = conversation.key
        assertTrue(hub.send(envelope("subscribe", key)))
        runCurrent()
        val request = MessageRequest(hub.hubScope, key, "试一下本地对话", "demo-message")
        assertEquals(MessageTransportResult.Accepted, hub.send(request))
        assertEquals(MessageTransportResult.Accepted, hub.send(request))
        assertEquals(listOf(request.text, DemoText().reply), hub.items(key, ItemsQuery()).items.map { it.text })
        // The chat hides the outbox copy by this id.
        assertEquals(request.clientMessageId, hub.items(key, ItemsQuery()).items.first().id)
        assertTrue(events.filterIsInstance<ItemsEvent>().any { !it.backlog && it.items.size == 2 })
        assertTrue(hub.send(request.copy(text = "不同内容")) is MessageTransportResult.HubError)
        assertTrue(hub.send(request.copy(scope = HubScope("demo", "another"))) is MessageTransportResult.HubError)
        val interaction = hub.interactions().first()
        hub.resolve(interaction.id, Resolution(approve = true))
        assertFalse(hub.interactions().any { it.id == interaction.id })
        assertTrue(events.any { it == InteractionResolvedEvent(interaction.id) })
        assertTrue(notices.all { it == DemoText().notSent })
        assertEquals(2, notices.size)
        hub.send(buildJsonObject { envelope("subscribe", key).forEach { (k, v) -> put(k, v) }; put("since_seq", 1) })
        runCurrent()
        val catchup = events.filterIsInstance<ItemsEvent>().last()
        assertNull(catchup.hasMore)
        assertEquals(listOf(2), catchup.items.map { it.seq })
        hub.reconnect()
        runCurrent()
        assertTrue(events.any { it is MachinesEvent })
        assertTrue(events.any { it is Hello })
        hub.close()
        assertFalse(hub.readyToSend.value)
        assertNull(hub.inboxConnection.current.value)
        assertFalse(hub.send(envelope("subscribe", key)))
        val fresh = DemoHub(source, backgroundScope, { fixedNow })
        assertNotEquals(hub.hubScope, fresh.hubScope)
        assertFalse(fresh.sessions().any { it.key == key })
        assertTrue(fresh.interactions().any { it.id == interaction.id })
        fresh.close()
    }

    @Test fun paginationAndSideEffectsFailClosedWithoutTouchingExternalServices() = runTest {
        val source = JsonCodec.parseToJsonElement(asset()).jsonObject
        val session = JsonCodec.decodeFromJsonElement<Session>(source.getValue("sessions").jsonArray.first())
        val expanded = JsonArray((1..181).map { seq -> JsonCodec.encodeToJsonElement(
            Item(id = "demo-item-$seq", seq = seq, role = "assistant", kind = "text", text = "演示内容"),
        ) })
        val fixture = JsonObject(source + ("items" to JsonObject(source.getValue("items").jsonObject +
            (session.sessionId to expanded))))
        val hub = DemoHub(fixture.toString(), backgroundScope, { fixedNow })
        val latest = hub.items(session.key, ItemsQuery())
        assertTrue(latest.hasMore)
        val older = hub.items(session.key, ItemsQuery(beforeSeq = latest.items.first().seq))
        val oldest = hub.items(session.key, ItemsQuery(beforeSeq = older.items.first().seq))
        assertFalse(oldest.hasMore)
        assertEquals((1..181).toList(), (oldest.items + older.items + latest.items).map { it.seq })
        assertTrue(hub.items(session.key, ItemsQuery(afterSeq = 0)).moreAfter)
        assertEquals((80..85).toList(), hub.items(session.key, ItemsQuery(fromSeq = 80, throughSeq = 85)).items.map { it.seq })

        val notices = mutableListOf<String>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { hub.notices.collect { notices += it } }
        val before = hub.inbox()
        val itemId = before.getValue("items").jsonArray.first().jsonObject.getValue("id").jsonPrimitive.content
        val mutations: List<suspend () -> Unit> = listOf(
            { hub.reply(itemId, null, "演示回复", "reply-1") },
            { hub.reply(null, "f1", "演示回复", "reply-2") },
            { hub.setDone(itemId, true) },
        )
        mutations.forEach { mutate ->
            val error = try { mutate(); fail("Expected a demo refusal"); null } catch (error: HubFailure) { error }
            assertEquals("demo", error!!.code)
            assertEquals("no", error.delivered)
            assertFalse(error.retryable)
            assertEquals(DemoText().notSent, error.message)
        }
        listOf("terminal_input", "register_phone", "future_write").forEach { type ->
            assertFalse(hub.send(envelope(type, session.key)))
        }
        assertEquals(before, hub.inbox())
        assertEquals(6, notices.size)
        listOf<suspend () -> Unit>(
            { hub.media("https://example.com/private") },
            { hub.items(SessionKey("missing", session.sessionId), ItemsQuery()) },
        ).forEach { read ->
            val error = try { read(); fail("Expected a local 404"); null } catch (error: HubFailure) { error }
            assertEquals(404, error!!.status)
            assertEquals(DemoText().missing, error.message)
        }
        hub.close()
    }

    private fun envelope(type: String, key: SessionKey) = buildJsonObject {
        put("type", type); put("machine_id", key.machineId); put("session_id", key.sessionId)
    }
}
