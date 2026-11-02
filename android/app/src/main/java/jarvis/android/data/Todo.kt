package jarvis.android.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.time.Instant

/**
 * A todo item representing a task to be done and tracked across agents.
 * Mirroring docs/umbrella.final.md and docs/protocol.md.
 */
@Serializable
data class SessionTodo(
    val id: String = "",
    val title: String = "",
)

@Serializable
data class TodoAgent(
    @SerialName("machine_id") val machineId: String = "",
    @SerialName("session_id") val sessionId: String = "",
    @SerialName("linked_at") val linkedAt: String = "",
    @SerialName("released_at") val releasedAt: String? = null,
    val title: String = "",
    val cwd: String = "",
    val agent: String? = null,
    @SerialName("start_error") val startError: String? = null,
) {
    val key: SessionKey get() = SessionKey(machineId, sessionId)
    val isCurrent: Boolean get() = releasedAt.isNullOrEmpty()
}

@Serializable
data class Todo(
    val id: String = "",
    val title: String = "",
    val notes: String = "",
    @SerialName("done_at") val doneAt: String? = null,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
    val agents: List<TodoAgent> = emptyList(),
) {
    val isDone: Boolean get() = !doneAt.isNullOrEmpty()
    val created: Instant? get() = parseTime(createdAt)
    val updated: Instant? get() = parseTime(updatedAt)
    val done: Instant? get() = parseTime(doneAt)

    val currentAgents: List<TodoAgent> get() = agents.filter { it.isCurrent }
    val releasedAgents: List<TodoAgent> get() = agents.filter { !it.isCurrent }
}

/**
 * Todo states in order of rank (umbrella.final.md「进度」):
 * 已完成 -> 等你 -> 待查看 -> 进行中 -> 待跟进 -> 未开始
 */
enum class TodoState(val title: String, val rank: Int) {
    Done("已完成", 0),
    NeedsYou("等你", 1),
    Unread("待查看", 2),
    Working("进行中", 3),
    FollowUp("待跟进", 4),
    NotStarted("未开始", 5);
}

/**
 * Calculates a todo's state from its current active agents and their session states.
 */
fun Todo.state(sessionLookup: (SessionKey) -> Session?): TodoState {
    if (isDone) return TodoState.Done
    val cur = currentAgents
    if (cur.isEmpty()) return TodoState.NotStarted
    val sessions = cur.map { a -> sessionLookup(a.key) }
    val states = sessions.map { it?.agentState ?: AgentState.Idle }
    val isStarting = sessions.any { it?.starting == true }
    return when {
        states.any { it == AgentState.NeedsYou } -> TodoState.NeedsYou
        states.any { it == AgentState.Unread } -> TodoState.Unread
        isStarting || states.any { it == AgentState.Working } -> TodoState.Working
        else -> TodoState.FollowUp
    }
}

/**
 * Sorts todos: uncompleted first (in TodoState rank order, then newest created first, then by id);
 * completed todos at the bottom (newest done/created first).
 */
fun List<Todo>.ordered(sessionLookup: (SessionKey) -> Session?): List<Todo> {
    val uncompleted = filter { !it.isDone }
    val completed = filter { it.isDone }
    val sortedUncompleted = uncompleted.sortedWith(
        compareBy<Todo> { it.state(sessionLookup).rank }
            .thenByDescending { it.created ?: Instant.EPOCH }
            .thenBy { it.id }
    )
    val sortedCompleted = completed.sortedWith(
        compareByDescending<Todo> { it.done ?: it.created ?: Instant.EPOCH }
            .thenBy { it.id }
    )
    return sortedUncompleted + sortedCompleted
}

/**
 * Formats the todo context tag as in Knowledge.kt / protocol.md.
 */
fun todoContextXml(title: String, notes: String): String =
    KbContext.xml("待办 · $title", notes)

/**
 * The prompt to send when handing off a todo to an agent.
 */
fun todoHandOffPrompt(title: String, notes: String, instruction: String): String {
    val contextTag = todoContextXml(title, notes)
    val ins = instruction.trim()
    return if (ins.isEmpty()) contextTag else "$contextTag\n\n$ins"
}
