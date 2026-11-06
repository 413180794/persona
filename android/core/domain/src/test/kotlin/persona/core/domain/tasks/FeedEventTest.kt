package persona.core.domain.tasks

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.AssistantFeedEvent
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi

/** A live assistant_feed item for a conversation not loaded yet must not stand in for its history (#4689). */
class FeedEventTest {
    private fun item(id: String, at: String) = FeedItem(id, "c1", at, "note", "example:1", buildJsonObject {})

    private class Api(val items: List<FeedItem>) : TasksApi {
        val asked = mutableListOf<String?>()
        override suspend fun todos() = emptyList<Todo>()
        override suspend fun feed(conversation: String, since: String?): FeedResponse {
            asked += since
            return FeedResponse(items.filter { item -> since?.let { item.at.orEmpty() > it } ?: true })
        }
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("unused")
        override suspend fun dismissPlan(id: String): TaskPlan = error("unused")
        override suspend fun retryTask(todoId: String): Todo = error("unused")
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("unused")
        override suspend fun today(conversation: String): FeedItem = error("unused")
        override suspend fun mainConversation(): String? = null
        override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
    }

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject): Boolean = true
    }

    @Test fun aLiveItemBeforeTheFirstLoadStillPullsTheWholeFeed() = runBlocking {
        val older = item("a1", "2026-11-04T09:00:00Z")
        val live = item("a2", "2026-11-04T10:00:00Z")
        val api = Api(listOf(older, live))
        val connection = Connection()
        val store = TasksStore(api, connection, CoroutineScope(Dispatchers.Unconfined))
        connection.events.emit(AssistantFeedEvent("c1", live))
        store.load("c1")
        assertEquals(listOf<String?>(null), api.asked)
        assertEquals(listOf("a1", "a2"), store.feeds.value["c1"]?.map { it.id })
        store.load("c1") // loaded once: later loads only ask for what came after
        assertEquals(listOf(null, "2026-11-04T10:00:00Z"), api.asked)
    }
}
