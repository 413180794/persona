package persona.core.domain.tasks

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
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

/** The to-dos show from the phone's copy until the hub answers (#4533, iOS TodoStore's StateCache). */
class TodoCacheTest {
    private class Api : TasksApi {
        var list = listOf<Todo>()
        var failure: Exception? = null
        override suspend fun todos(): List<Todo> { failure?.let { throw it }; return list }
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList())
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

    private class Cache(var stored: List<Todo>? = null) : TodoCache {
        var gate: CompletableDeferred<Unit>? = null
        val writes = mutableListOf<List<Todo>>()
        override suspend fun read(): List<Todo>? { gate?.await(); return stored }
        override suspend fun write(todos: List<Todo>) { writes += todos; stored = todos }
    }

    /** The hub's error as the HTTP client throws it (HubFailure): read by its getter. */
    private class HubError(val status: Int) : Exception("HTTP $status")

    @Test fun theLastToDosShowBeforeTheHubIsReached() {
        val cache = Cache(listOf(Todo(id = "a", title = "写周报"), Todo(id = "b", title = "修登录")))
        val store = TasksStore(Api(), Connection(), CoroutineScope(Dispatchers.Unconfined), cache)
        assertEquals(listOf("a", "b"), store.todos.value.map { it.id })
        // The copy says nothing about the hub: projects wait for its own answer.
        assertFalse(store.spacesSupported)
        assertTrue(cache.writes.isEmpty())
    }

    @Test fun theHubsAnswerReplacesTheCopyAndIsKeptForTheNextStart() {
        val cache = Cache(listOf(Todo(id = "old")))
        val api = Api().apply { list = listOf(Todo(id = "new", title = "新的")) }
        val connection = Connection()
        val store = TasksStore(api, connection, CoroutineScope(Dispatchers.Unconfined), cache)
        connection.state.value = ConnectionState.Online
        assertEquals(listOf("new"), store.todos.value.map { it.id })
        assertEquals(listOf(listOf("new")), cache.writes.map { list -> list.map { it.id } })
        // The same answer again is not written again; a change is.
        store.reloadTodos()
        assertEquals(1, cache.writes.size)
        api.list = emptyList()
        store.reloadTodos()
        assertEquals(emptyList<Todo>(), store.todos.value)
        assertEquals(emptyList<Todo>(), cache.stored)
        // The next start begins from what the hub said last.
        val next = TasksStore(Api(), Connection(), CoroutineScope(Dispatchers.Unconfined), cache)
        assertEquals(emptyList<Todo>(), next.todos.value)
    }

    @Test fun aCopyReadAfterTheHubAnsweredDoesNotComeBack() {
        val cache = Cache(listOf(Todo(id = "stale"))).apply { gate = CompletableDeferred() }
        val api = Api().apply { list = listOf(Todo(id = "fresh")) }
        val store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined), cache)
        store.reloadTodos()
        cache.gate!!.complete(Unit)
        assertEquals(listOf("fresh"), store.todos.value.map { it.id })
    }

    @Test fun aFailedReadKeepsTheCopyAndAnOlderHubClearsIt() {
        val cache = Cache(listOf(Todo(id = "a")))
        val api = Api().apply { failure = IllegalStateException("offline") }
        val store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined), cache)
        store.reloadTodos()
        assertEquals(listOf("a"), store.todos.value.map { it.id })
        assertTrue(cache.writes.isEmpty())
        // A hub without to-dos (404): nothing to show, and nothing to show on the next start either.
        api.failure = HubError(404)
        store.reloadTodos()
        assertFalse(store.todosSupported.value)
        assertEquals(emptyList<Todo>(), store.todos.value)
        assertEquals(emptyList<Todo>(), cache.stored)
    }
}
