package persona.core.hub

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.JsonCodec
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.TodosResponse
import persona.core.contracts.api.TasksApi

/** The unified entry's routes on the active pairing (docs/protocol.md「任务和对话动态」). */
class HttpTasksApi internal constructor(
    private val request: suspend (path: String, method: String, body: JsonElement?) -> Response,
) : TasksApi {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body ->
        connection.request(path, method, body?.toString()?.toRequestBody("application/json".toMediaType()))
    })

    private suspend fun call(path: String, method: String = "GET", body: JsonElement? = null): JsonObject =
        request(path, method, body).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            val text = it.body?.string().orEmpty()
            if (text.isBlank()) JsonObject(emptyMap()) else JsonCodec.parseToJsonElement(text).jsonObject
        }

    private fun path(vararg segments: String, query: Map<String, String> = emptyMap()): String {
        val url = HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1")
        segments.forEach { url.addPathSegment(it) }
        query.forEach { (k, v) -> url.addQueryParameter(k, v) }
        val built = url.build()
        return built.encodedPath + (built.encodedQuery?.let { "?$it" } ?: "")
    }

    override suspend fun todos(): List<Todo> =
        JsonCodec.decodeFromJsonElement<TodosResponse>(call(path("todos"))).todos

    override suspend fun createTodo(title: String, notes: String): Todo =
        JsonCodec.decodeFromJsonElement(call(path("todos"), "POST", buildJsonObject {
            put("title", title)
            put("notes", notes)
        })["todo"] ?: error("no todo in the answer"))

    override suspend fun patchTodo(id: String, title: String?, notes: String?, done: Boolean?): Todo =
        JsonCodec.decodeFromJsonElement(call(path("todos", id), "PATCH", buildJsonObject {
            title?.let { put("title", it) }
            notes?.let { put("notes", it) }
            done?.let { put("done", it) }
        })["todo"] ?: error("no todo in the answer"))

    override suspend fun deleteTodo(id: String) {
        call(path("todos", id), "DELETE")
    }

    override suspend fun linkTodoAgent(todoId: String, machineId: String, sessionId: String) {
        call(path("todos", todoId, "agents", machineId, sessionId), "PUT", JsonObject(emptyMap()))
    }

    override suspend fun unlinkTodoAgent(todoId: String, machineId: String, sessionId: String) {
        call(path("todos", todoId, "agents", machineId, sessionId), "DELETE")
    }

    override suspend fun feed(conversation: String, since: String?): FeedResponse =
        JsonCodec.decodeFromJsonElement(call(path("assistant", "conversations", conversation, "feed",
            query = since?.let { mapOf("since" to it) } ?: emptyMap())))

    override suspend fun startPlan(id: String, remember: Boolean): TaskPlan =
        plan(call(path("task-plans", id, "start"), "POST", buildJsonObject { put("remember", remember) }))

    override suspend fun dismissPlan(id: String): TaskPlan = plan(call(path("task-plans", id, "dismiss"), "POST", JsonObject(emptyMap())))

    override suspend fun retryTask(todoId: String): Todo =
        JsonCodec.decodeFromJsonElement(call(path("todos", todoId, "retry"), "POST", JsonObject(emptyMap()))["todo"]
            ?: error("no todo in the answer"))

    override suspend fun removeTask(todoId: String) {
        call(path("todos", todoId), "DELETE")
    }

    override suspend fun route(conversation: String, todoId: String, text: String): FeedItem =
        item(call(path("assistant", "conversations", conversation, "route"), "POST",
            buildJsonObject { put("todo_id", todoId); put("text", text) }))

    override suspend fun today(conversation: String): FeedItem =
        item(call(path("assistant", "conversations", conversation, "today"), "POST", JsonObject(emptyMap())))

    override suspend fun mainConversation(): String? =
        (call(path("assistant"))["main"] as? kotlinx.serialization.json.JsonPrimitive)?.content?.takeIf(String::isNotBlank)

    override suspend fun inboxReply(itemId: String, text: String, card: String?) {
        call(path("inbox", itemId, "reply"), "POST", buildJsonObject { put("text", text); card?.let { put("card", it) } })
    }

    private fun plan(o: JsonObject): TaskPlan = JsonCodec.decodeFromJsonElement(o["plan"] ?: error("no plan in the answer"))
    private fun item(o: JsonObject): FeedItem = JsonCodec.decodeFromJsonElement(o["item"] ?: error("no item in the answer"))
}
