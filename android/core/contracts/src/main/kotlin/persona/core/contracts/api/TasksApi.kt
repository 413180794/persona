package persona.core.contracts.api

import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo

/** Tasks dispatched from an assistant conversation (docs/unified-entry.md; docs/protocol.md「任务和对话动态」). */
interface TasksApi {
    suspend fun todos(): List<Todo>
    /** A conversation's feed after [since] (an ISO time; null: all of it), with the plans its `plan` items name. */
    suspend fun feed(conversation: String, since: String?): FeedResponse
    suspend fun startPlan(id: String, remember: Boolean): TaskPlan
    suspend fun dismissPlan(id: String): TaskPlan
    /**
     * 重试 on a task whose agent did not start (#1396): the hub starts the same task again in the same todo and answers
     * it reopened, without its error. Refused (409) when the machine is offline or the task did not fail to start.
     */
    suspend fun retryTask(todoId: String): Todo
    /** Removes a task: its todo goes; its agents and their chats stay. */
    suspend fun removeTask(todoId: String)
    /** Sends text straight to a task's agent; the answer is the conversation's new `routed` item. */
    suspend fun route(conversation: String, todoId: String, text: String): FeedItem
    /** The conversation's 今天 card for the user's day (made on the first call of the day). */
    suspend fun today(conversation: String): FeedItem
    /** The main assistant conversation's id (GET /api/v1/assistant `main`), null when there is none. */
    suspend fun mainConversation(): String?
    /** 发送 on a message card: the draft goes out through the inbox, which files the item as done. */
    suspend fun inboxReply(itemId: String, text: String)
}
