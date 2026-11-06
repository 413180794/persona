package persona.core.domain.tasks

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
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

/** "Say to 'xx'" to a to-do that is gone (#4691): the message goes to Persona instead of a refused /route. */
class RouteTest {
    private class Api : TasksApi {
        var list = emptyList<Todo>()
        val routed = mutableListOf<Triple<String, String, String>>()
        override suspend fun todos() = list
        override suspend fun feed(conversation: String, since: String?) = FeedResponse(emptyList())
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("unused")
        override suspend fun dismissPlan(id: String): TaskPlan = error("unused")
        override suspend fun retryTask(todoId: String): Todo = error("unused")
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem {
            routed += Triple(conversation, todoId, text)
            return FeedItem("f1", conversation, "2026-11-06T10:00:00Z", "routed", "example:1", JsonObject(emptyMap()))
        }
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

    @Test fun aRouteToAMissingTodoFallsBackToPersona() {
        val api = Api()
        val store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.setRoute("c1", "t1")
        assertFalse(store.sendRouted("c1", "hi") {})
        assertTrue(api.routed.isEmpty())
        assertFalse("c1" in store.routes.value)
    }

    @Test fun aRouteToAListedTodoGoesToIt() {
        val api = Api()
        api.list = listOf(Todo(id = "t1"))
        val store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.reloadTodos()
        store.setRoute("c1", "t1")
        assertTrue(store.sendRouted("c1", "hi") {})
        assertEquals(listOf(Triple("c1", "t1", "hi")), api.routed)
    }

    @Test fun deletingOrLosingTheTodoDropsItsRoute() = runBlocking {
        val api = Api()
        api.list = listOf(Todo(id = "t1"), Todo(id = "t2"))
        val store = TasksStore(api, Connection(), CoroutineScope(Dispatchers.Unconfined))
        store.reloadTodos()
        store.setRoute("c1", "t1")
        store.setRoute("c2", "t2")
        store.deleteTodo("t1")
        assertEquals(mapOf("c2" to "t2"), store.routes.value)
        api.list = emptyList() // deleted on another device
        store.reloadTodos()
        assertEquals(emptyMap<String, String>(), store.routes.value)
    }
}
