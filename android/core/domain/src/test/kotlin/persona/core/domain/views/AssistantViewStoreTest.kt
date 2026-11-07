package persona.core.domain.views

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.JsonCodec
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.generated.ExtensionAssistantHTMLActionInput
import persona.core.contracts.generated.ExtensionViewActionInput
import persona.core.contracts.api.AssistantViewAnswer
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.contracts.generated.ExtensionViewAction
import persona.core.domain.tasks.TasksStore

// The cards' state per hub and conversation (docs/assistant/assistant-views.md "Live cards" "Persisted submissions") and how their actions go.
class AssistantViewStoreTest {
    private fun json(text: String): JsonElement = JsonCodec.parseToJsonElement(text)
    private fun view(id: String, version: Long, title: String) =
        json("""{"id":"$id","version":$version,"view":{"title":"$title","fallback":"f","sections":[]},"updated_at":"2026-11-05T10:00:0${version}Z"}""")
    private fun answer(key: String, status: String, revision: Long?, at: String = "2026-11-05T10:00:00Z") =
        json("""{"key":"$key","action":"a","values":{},"client_msg_id":"c","status":"$status",${revision?.let { "\"revision\":$it," } ?: ""}"at":"$at","updated_at":"$at"}""")

    @Test fun versionsAndRevisionsOnlyGoForward() {
        val store = AssistantViewStore()
        store.view("c1", view("view_1", 2, "新"))
        store.view("c1", view("view_1", 1, "旧"))
        assertEquals(2L, store.of("c1").views.getValue("view_1").version)
        // A snapshot read before the event does not take the card back.
        store.snapshot("c1", mapOf("view_1" to view("view_1", 1, "旧")), emptyMap(), listOf("i1/0"))
        assertEquals(2L, store.of("c1").views.getValue("view_1").version)
        store.view("c1", view("view_1", 3, "更新"))
        assertEquals(3L, store.of("c1").views.getValue("view_1").version)

        store.answer("c1", answer("view_1", "sent", 3))
        store.answer("c1", answer("view_1", "pending", 2))
        assertEquals("sent", store.of("c1").answers.getValue("view_1").status)
        // A failure after it was sent is later: the revision says so, never how far the status got.
        store.answer("c1", answer("view_1", "failed", 4))
        assertEquals("failed", store.of("c1").answers.getValue("view_1").status)
        store.snapshot("c1", emptyMap(), mapOf("view_1" to answer("view_1", "pending", 1)), emptyList())
        assertEquals("failed", store.of("c1").answers.getValue("view_1").status)
        assertTrue("i1/0" in store.of("c1").blocks)
    }

    @Test fun conversationsAreKeptApart() {
        val store = AssistantViewStore()
        store.view("c1", view("view_1", 1, "一"))
        store.answer("c2", answer("i/0", "sent", 1))
        store.block("c2", "i/0")
        assertNull(store.of("c2").views["view_1"])
        assertNull(store.of("c1").answers["i/0"])
        assertTrue(store.of("c1").blocks.isEmpty())
        // Not a card at all: nothing changes.
        store.view("c1", json("""{"id":"view_2","version":0,"view":{}}"""))
        store.view("c1", json("""{"id":"view_3","version":"2","view":{}}"""))
        assertEquals(setOf("view_1"), store.of("c1").views.keys)
    }

    @Test fun answersWithoutRevisionsFollowTime() {
        val a = JsonCodec.decodeFromJsonElement(ExtensionViewAction.serializer(), answer("k", "pending", null, "2026-11-05T10:00:00Z"))
        val b = JsonCodec.decodeFromJsonElement(ExtensionViewAction.serializer(), answer("k", "sent", null, "2026-11-05T10:00:05Z"))
        assertTrue(AssistantViewStore.newer(a, b))
        assertTrue(!AssistantViewStore.newer(b, a))
    }

    private class Api : TasksApi {
        override val features = MutableStateFlow(setOf("assistant_views", "assistant_html_bridge"))
        val sent = mutableListOf<Pair<String, ExtensionViewActionInput>>()
        val html = mutableListOf<ExtensionAssistantHTMLActionInput>()
        var next: () -> AssistantViewAnswer = { error("down") }
        var feeds = 0
        override suspend fun todos() = emptyList<Todo>()
        override suspend fun feed(conversation: String, since: String?): FeedResponse {
            feeds++
            return FeedResponse(views = mapOf("view_1" to JsonCodec.parseToJsonElement(
                """{"id":"view_1","version":5,"view":{"fallback":"f","sections":[]}}""")))
        }
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("unused")
        override suspend fun dismissPlan(id: String): TaskPlan = error("unused")
        override suspend fun retryTask(todoId: String): Todo = error("unused")
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("unused")
        override suspend fun today(conversation: String): FeedItem = error("unused")
        override suspend fun mainConversation(): String? = null
        override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
        override suspend fun viewAction(conversation: String, key: String, input: ExtensionViewActionInput): AssistantViewAnswer {
            sent += key to input
            return next()
        }
        override suspend fun viewHtmlAction(conversation: String, key: String, input: ExtensionAssistantHTMLActionInput): AssistantViewAnswer {
            html += input
            return next()
        }
    }

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject): Boolean = true
    }

    private val reply = AssistantAction("choose-b", null, reply = "选 B")

    @Test fun aRetryKeepsItsIdAndANewRequestGetsOne() = runBlocking {
        val api = Api()
        var n = 0
        val store = AssistantViewStore()
        val actions = AssistantCardActions(api, store, reload = {}, newId = { "id-${++n}" })
        assertTrue(actions.submit("c1", "i/0", reply, emptyMap(), confirmed = false, version = 1) is CardOutcome.Failed)
        api.next = { AssistantViewAnswer.Recorded(ExtensionViewAction(key = "i/0", action = "choose-b", clientMsgId = "id-1", status = "pending", revision = 1, at = "t")) }
        assertEquals(CardOutcome.Recorded, actions.submit("c1", "i/0", reply, emptyMap(), confirmed = false, version = 1))
        assertEquals(listOf("id-1", "id-1"), api.sent.map { it.second.clientMsgId })
        assertEquals(1L, api.sent.last().second.expectedVersion)
        assertNull("Not confirmed: the field is left out", api.sent.last().second.confirmed)
        assertEquals("pending", store.of("c1").answers.getValue("i/0").status)
        // Another answer, or the same one at a newer version, is a new request.
        actions.submit("c1", "i/0", reply, mapOf("x" to "1"), confirmed = true, version = 1)
        actions.submit("c1", "i/0", reply, mapOf("x" to "1"), confirmed = true, version = 2)
        assertEquals(listOf("id-1", "id-1", "id-2", "id-3"), api.sent.map { it.second.clientMsgId })
        assertEquals(true, api.sent.last().second.confirmed)
        assertEquals(mapOf("x" to "1"), api.sent.last().second.values)
        // Never the reply's words: the hub reads them from the card it saved.
        assertTrue(api.sent.none { JsonCodec.encodeToString(ExtensionViewActionInput.serializer(), it.second).contains("选 B") })
    }

    // A hub that did not say assistant_views (an older one, cards switched off, no hello yet) is sent no action; the
    // bridge needs assistant_html_bridge too.
    @Test fun nothingGoesToAHubThatDidNotSayItTakesCards() = runBlocking {
        val api = Api()
        api.next = { AssistantViewAnswer.Recorded(ExtensionViewAction(key = "i/0", action = "choose-b", clientMsgId = "c", status = "pending", revision = 1, at = "t")) }
        val actions = AssistantCardActions(api, AssistantViewStore(), reload = {})
        api.features.value = emptySet()
        assertEquals(CardOutcome.Unsupported, actions.submit("c1", "i/0", reply, emptyMap(), confirmed = false, version = 1))
        api.features.value = setOf("assistant_views")
        assertEquals(CardOutcome.Unsupported, actions.html("c1", "view_1", "guide", null, 2, text = "继续"))
        assertTrue(api.sent.isEmpty() && api.html.isEmpty())
        assertEquals(CardOutcome.Recorded, actions.submit("c1", "i/0", reply, emptyMap(), confirmed = false, version = 1))
        assertEquals(1L, api.sent.single().second.expectedVersion)
        // An API that cannot tell counts as none.
        val unknown = object : TasksApi by api { override val features: kotlinx.coroutines.flow.StateFlow<Set<String>>? = null }
        assertEquals(CardOutcome.Unsupported, AssistantCardActions(unknown, AssistantViewStore(), reload = {})
            .submit("c1", "i/1", reply, emptyMap(), confirmed = false, version = 1))
        assertEquals(1, api.sent.size)
    }

    // An HTML page's reply tried again keeps its id though a link was opened in between.
    @Test fun anHtmlReplyKeepsItsIdAcrossALink() = runBlocking {
        val api = Api()
        var n = 0
        val actions = AssistantCardActions(api, AssistantViewStore(), reload = {}, newId = { "id-${++n}" })
        actions.html("c1", "view_1", "guide", null, 2, text = "继续")
        api.next = { AssistantViewAnswer.Open("https://example.com", 2) }
        actions.html("c1", "view_1", "guide", null, 2, url = "https://example.com")
        actions.html("c1", "view_1", "guide", null, 2, text = "继续")
        assertEquals(listOf("reply", "open", "reply"), api.html.map { it.kind })
        assertEquals(listOf("id-1", "id-2", "id-1"), api.html.map { it.clientMsgId })
        assertTrue(api.html.all { it.confirmed && it.expectedVersion == 2L && it.componentKey == "guide" })
    }

    @Test fun conflictsPullTheCardAgainAndNeverResend() = runBlocking {
        val api = Api()
        val tasks = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined))
        api.next = { AssistantViewAnswer.ViewChanged(null, null) }
        assertEquals(CardOutcome.Changed, tasks.cardActions.submit("c1", "view_1", reply, emptyMap(), confirmed = true, version = 4))
        assertEquals("One request, not retried at the new version", 1, api.sent.size)
        assertEquals(1, api.feeds)
        assertEquals(5L, tasks.cards.of("c1").views.getValue("view_1").version)
        api.next = { AssistantViewAnswer.AlreadyAnswered(null) }
        assertEquals(CardOutcome.AlreadyAnswered, tasks.cardActions.submit("c1", "view_1", reply, emptyMap(), confirmed = true, version = 5))
        assertEquals(2, api.feeds)
        api.next = { AssistantViewAnswer.Open("https://example.com", 5) }
        assertEquals(CardOutcome.Open("https://example.com"), tasks.cardActions.html("c1", "view_1", "guide", null, 5, url = "https://example.com"))
        assertEquals("open", api.html.single().kind)
        assertEquals(true, api.html.single().confirmed)
        assertTrue(tasks.cardActions.busy.value.isEmpty())
    }

    @Test fun theHubsEventsReachTheCards() = runBlocking {
        val api = Api()
        val connection = Connection()
        val tasks = TasksStore(api, connection, CoroutineScope(Dispatchers.Unconfined))
        connection.events.emit(ServerEvent.decode("""{"type":"assistant_view","conversation":"c1","view":{"id":"view_9","version":2,"view":{"fallback":"f","sections":[]}}}""")!!)
        connection.events.emit(ServerEvent.decode("""{"type":"assistant_view_block","conversation":"c1","key":"item#0/1"}""")!!)
        connection.events.emit(ServerEvent.decode("""{"type":"assistant_view_action","conversation":"c1","action":{"key":"view_9","action":"a","client_msg_id":"c","status":"sent","revision":2,"at":"t"}}""")!!)
        connection.events.emit(ServerEvent.decode("""{"type":"assistant_feed","conversation":"c2","item":{"id":"feed_1","conversation":"c2","kind":"view","ref":"view_5",
            "body":{"view":{"id":"view_5","version":1,"view":{"fallback":"f","sections":[]}}}}}""")!!)
        val c1 = tasks.cards.of("c1")
        assertEquals(2L, c1.views.getValue("view_9").version)
        assertTrue("item#0/1" in c1.blocks)
        assertEquals("sent", c1.answers.getValue("view_9").status)
        assertEquals(1L, tasks.cards.of("c2").views.getValue("view_5").version)
        assertEquals("view", tasks.feeds.value.getValue("c2").single().kind)
    }
}
