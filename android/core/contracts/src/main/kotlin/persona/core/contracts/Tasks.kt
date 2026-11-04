package persona.core.contracts

import kotlinx.serialization.KeepGeneratedSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull

// Tasks dispatched from an assistant conversation (docs/unified-entry.md; docs/protocol.md「任务和对话动态」): the plans the
// assistant hands the user, the todos that are tasks, and the items the hub inserts into a conversation (its feed).

@KeepGeneratedSerializer
@Serializable(with = PlanTaskSerializer::class)
data class PlanTask(
    val title: String = "",
    val prompt: String = "",
    val machine: String = "",
    val cwd: String = "",
    val launcher: String? = null,
    val model: String? = null,
    val permission: String? = null,
    val readonly: Boolean? = null,
    val why: String? = null,
    @SerialName("todo_id") val todoId: String? = null,
    val error: String? = null,
) {
    /** The directory's last part: how the user names a project. */
    val project get() = cwd.trimEnd('/').substringAfterLast('/')
}

@KeepGeneratedSerializer
@Serializable(with = TaskPlanSerializer::class)
data class TaskPlan(
    val id: String,
    val conversation: String = "",
    val tasks: List<PlanTask> = emptyList(),
    @WireEnum("open", "started", "dismissed", "unknown")
    val state: String = "unknown",
    @WireTime @SerialName("created_at") val createdAt: String? = null,
)

/** An item the hub inserted into an assistant conversation, drawn among the transcript's lines by [at]. */
@KeepGeneratedSerializer
@Serializable(with = FeedItemSerializer::class)
data class FeedItem(
    val id: String,
    val conversation: String = "",
    @WireTime val at: String? = null,
    @WireEnum("plan", "task", "answered", "receipt", "routed", "today", "message", "unknown")
    val kind: String = "unknown",
    val ref: String? = null,
    val body: JsonElement? = null,
) {
    /** A string field of the body (receipt's title / summary / pr, routed's text, message's chat / draft…). */
    fun field(name: String): String? = ((body as? JsonObject)?.get(name) as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull
    fun number(name: String): Int? = ((body as? JsonObject)?.get(name) as? JsonPrimitive)?.intOrNull
}

@KeepGeneratedSerializer
@Serializable(with = TodoAgentSerializer::class)
data class TodoAgent(
    @SerialName("machine_id") val machineId: String = "",
    @SerialName("session_id") val sessionId: String = "",
    val title: String? = null,
    val cwd: String? = null,
    val agent: String? = null,
    @SerialName("start_error") val startError: String? = null,
    @WireTime @SerialName("released_at") val releasedAt: String? = null,
)

/** A todo (docs/todos.md); a task is one whose [origin] is an assistant conversation. */
@KeepGeneratedSerializer
@Serializable(with = TodoSerializer::class)
data class Todo(
    val id: String,
    val title: String = "",
    @WireTime @SerialName("done_at") val doneAt: String? = null,
    @WireTime @SerialName("created_at") val createdAt: String? = null,
    val origin: String? = null,
    @SerialName("plan_id") val planId: String? = null,
    val agents: List<TodoAgent> = emptyList(),
) {
    val activeAgents get() = agents.filter { it.releasedAt == null }
}

@KeepGeneratedSerializer
@Serializable(with = TodosResponseSerializer::class)
data class TodosResponse(val todos: List<Todo> = emptyList())

@KeepGeneratedSerializer
@Serializable(with = FeedResponseSerializer::class)
data class FeedResponse(val items: List<FeedItem> = emptyList(), val plans: Map<String, TaskPlan> = emptyMap())

/** The todos changed: pull them again. */
@KeepGeneratedSerializer
@Serializable(with = TodosEventSerializer::class)
class TodosEvent : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = AssistantFeedEventSerializer::class)
data class AssistantFeedEvent(val conversation: String = "", val item: FeedItem) : ServerEvent

@KeepGeneratedSerializer
@Serializable(with = TaskPlanEventSerializer::class)
data class TaskPlanEvent(val plan: TaskPlan) : ServerEvent

object PlanTaskSerializer : WireSerializer<PlanTask>(PlanTask.generatedSerializer())
object TaskPlanSerializer : WireSerializer<TaskPlan>(TaskPlan.generatedSerializer())
object FeedItemSerializer : WireSerializer<FeedItem>(FeedItem.generatedSerializer())
object TodoAgentSerializer : WireSerializer<TodoAgent>(TodoAgent.generatedSerializer())
object TodoSerializer : WireSerializer<Todo>(Todo.generatedSerializer())
object TodosResponseSerializer : WireSerializer<TodosResponse>(TodosResponse.generatedSerializer())
object FeedResponseSerializer : WireSerializer<FeedResponse>(FeedResponse.generatedSerializer())
object TodosEventSerializer : WireSerializer<TodosEvent>(TodosEvent.generatedSerializer())
object AssistantFeedEventSerializer : WireSerializer<AssistantFeedEvent>(AssistantFeedEvent.generatedSerializer())
object TaskPlanEventSerializer : WireSerializer<TaskPlanEvent>(TaskPlanEvent.generatedSerializer())
