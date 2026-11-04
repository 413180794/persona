package persona.core.domain.tasks

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.contracts.draftPlaceholder

/** The card 「按任务进度起草的回复」 (#1519): sent is what the hub records on it, and it goes out once. */
class MessageCardTest {
    private fun card(id: String = "m1", extra: Map<String, String> = emptyMap()) = FeedItem(id, "c1", "2026-11-04T10:00:00Z", "message", "example:1",
        buildJsonObject { put("draft", "在修了"); extra.forEach { (k, v) -> put(k, v) } })

    @Test fun sentIsReadFromTheCard() {
        assertNull(card().sent)
        assertEquals("修好了", card(extra = mapOf("sent_at" to "2026-11-04T10:23:00Z", "sent" to "修好了")).sent?.text)
        assertEquals("在修了", card(extra = mapOf("sent_at" to "2026-11-04T10:23:00Z")).sent?.text) // from before the record
    }

    @Test fun aCardSentElsewhereTakesItsNewBody() {
        val list = listOf(card())
        assertSame(list, AssistantLines.merge(list, listOf(card())))
        val sent = card(extra = mapOf("sent_at" to "2026-11-04T10:23:00Z"))
        assertEquals(listOf(sent), AssistantLines.merge(list, listOf(sent)))
    }

    @Test fun findsABlankLeftToFillIn() {
        assertEquals("【预计今晚 / 几点前】", draftPlaceholder("确实是 bug，【预计今晚 / 几点前】修好"))
        assertEquals("{{链接}}", draftPlaceholder("MR 在 {{链接}}"))
        assertNull(draftPlaceholder("确实是 bug，修好了告诉你"))
    }

    private class Api(val gate: CompletableDeferred<Unit>, val items: List<FeedItem>) : TasksApi {
        val replies = mutableListOf<Triple<String, String, String?>>()
        override suspend fun todos() = emptyList<Todo>()
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(items)
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("unused")
        override suspend fun dismissPlan(id: String): TaskPlan = error("unused")
        override suspend fun retryTask(todoId: String): Todo = error("unused")
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("unused")
        override suspend fun today(conversation: String): FeedItem = error("unused")
        override suspend fun mainConversation(): String? = null
        override suspend fun inboxReply(itemId: String, text: String, card: String?) {
            replies += Triple(itemId, text, card)
            gate.await()
        }
    }

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject): Boolean = true
    }

    @Test fun sendsOnceHoweverManyTapsAndDrawsItSent() = runBlocking {
        val gate = CompletableDeferred<Unit>()
        val api = Api(gate, listOf(card()))
        val store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.load("c1")
        val errors = mutableListOf<Throwable>()
        store.sendMessage(card(), "修好了") { errors += it }
        store.sendMessage(card(), "修好了") { errors += it }
        assertTrue("m1" in store.busy.value)
        gate.complete(Unit)
        assertEquals(listOf(Triple("example:1", "修好了", "m1")), api.replies)
        assertEquals("修好了", store.feeds.value["c1"]?.single()?.sent?.text)
        assertTrue("m1" !in store.busy.value)
        store.sendMessage(card(), "修好了") { errors += it } // a stale copy: already sent
        assertEquals(1, api.replies.size)
        assertTrue(errors.isEmpty())
    }

    @Test fun aDraftWithABlankIsNotSent() {
        val gate = CompletableDeferred(Unit)
        val api = Api(gate, emptyList())
        val store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined))
        val errors = mutableListOf<Throwable>()
        store.sendMessage(card(), "【预计今晚 / 几点前】修好") { errors += it }
        assertTrue(api.replies.isEmpty())
        assertTrue(errors.single().message.orEmpty().contains("【预计今晚 / 几点前】"))
    }
}
