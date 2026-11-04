package persona.core.domain.tasks

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import persona.core.contracts.AgentState
import persona.core.contracts.AssistantFeedEvent
import persona.core.contracts.FeedItem
import persona.core.contracts.JsonCodec
import persona.core.contracts.Session
import persona.core.contracts.TaskPlan
import persona.core.contracts.TaskPlanEvent
import persona.core.contracts.Todo
import persona.core.contracts.TodosEvent
import persona.core.contracts.draftPlaceholder
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.domain.lines.ApiErrorInfo
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ToolCall
import persona.core.domain.lines.WorkItem

// Tasks dispatched from an assistant conversation (docs/unified-entry.md; iOS TasksStore / Shared/Tasks.swift, web
// lib/feed.ts): the todos that are tasks, the plans, each conversation's feed, and where those go among the chat's lines.

/** Where a task is, from its todo and its agents' states (docs/todos.md「状态计算规则」). */
enum class TaskState(val label: String, val rank: Int) {
    NeedsYou("等你", 0),
    /** An agent's turn was cut off by the model service (#1516): not done; 继续 tells it to go on. */
    Interrupted("中断了", 0),
    Working("在做", 1), NotStarted("正在开", 1), Unread("做完了", 2), Followup("停着", 3), Done("完成", 4),
    /** Its agent did not start (#1077): the hub ended it with why; 重试 starts it again (#1396). */
    Failed("没开起来", 4);

    companion object {
        /** [interrupted]: one of its agents stopped on a model API error (`Session.interrupted`). */
        fun of(todo: Todo, agents: List<AgentState>, interrupted: Boolean = false): TaskState = when {
            todo.doneAt != null && !todo.startError.isNullOrBlank() -> Failed
            todo.doneAt != null -> Done
            AgentState.NeedsYou in agents -> NeedsYou
            interrupted -> Interrupted
            AgentState.Unread in agents -> Unread
            AgentState.Working in agents -> Working
            agents.isNotEmpty() -> Followup
            else -> NotStarted
        }
    }
}

/** A receipt's outcome: "interrupted" (#1516) also for a "done" one from an older hub whose summary is the API error
 *  that cut the turn off; "failed"; else "done". */
fun receiptState(item: FeedItem): String = when (val st = item.field("state")) {
    "failed", "interrupted" -> st
    else -> if (ApiErrorInfo.parse(item.field("summary")) != null) "interrupted" else "done"
}

/** Why the model service cut the agent off (#1516), in words, and whether 继续 can help; null when it was not. */
data class Interruption(val reason: String, val canContinue: Boolean) {
    companion object {
        fun of(s: Session?): Interruption? {
            if (s?.interrupted != true) return null
            val info = ApiErrorInfo.parse(s.turnError)
            return Interruption(info?.reason ?: "模型请求失败", info?.needsAction != true)
        }
    }
}

/** A reminder or an event the assistant added on the phone, read from its tool call. */
data class QuickResult(val reminder: Boolean, val callId: String, val title: String, val time: String?, val place: String?) {
    companion object {
        fun of(call: ToolCall): QuickResult? {
            val short = call.name.substringAfterLast("__")
            val reminder = when (short) {
                "phone_reminders_create" -> true
                "phone_calendar_create_event" -> false
                else -> return null
            }
            val result = call.result?.item ?: return null
            if (result.isError == true) return null
            val o = runCatching { JsonCodec.parseToJsonElement(result.text.orEmpty()) as? JsonObject }.getOrNull() ?: return null
            fun s(k: String) = (o[k] as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull
            if (s("id").isNullOrEmpty()) return null
            return QuickResult(reminder, call.use.id, s("title").orEmpty(), s(if (reminder) "due" else "start"), s(if (reminder) "list" else "calendar"))
        }
    }
}

/** What the assistant's conversation adds to the transcript's lines, drawn by the chat's host. */
sealed interface AssistantExtra {
    data class Feed(val item: FeedItem) : AssistantExtra
    data class Quick(val result: QuickResult) : AssistantExtra
    data class Day(val date: LocalDate) : AssistantExtra
}

object AssistantLines {
    /** Keeps one copy of each item, oldest first. */
    fun merge(list: List<FeedItem>, items: List<FeedItem>): List<FeedItem> {
        val have = list.associateBy { it.id }
        // One it has whose body changed (a message card sent) takes the new body.
        val changed = items.filter { have[it.id]?.let { old -> old.body != it.body } == true }.associateBy { it.id }
        val fresh = items.filter { it.id !in have }
        if (fresh.isEmpty() && changed.isEmpty()) return list
        val kept = if (changed.isEmpty()) list else list.map { changed[it.id] ?: it }
        if (fresh.isEmpty()) return kept
        return (kept + fresh).sortedWith(compareBy<FeedItem>({ time(it.at) ?: Instant.EPOCH }, { it.id }))
    }

    /**
     * The chat's lines (in reading order) with the feed among them — each item at the end of the exchange it happened
     * in, before the first message sent after it — each turn's quick results after its work line, and a day line over
     * the first line of each day.
     */
    fun lines(lines: List<ChatLine>, feed: List<FeedItem>, zone: ZoneId = ZoneId.systemDefault()): List<ChatLine> {
        val out = ArrayList<ChatLine>(lines.size + feed.size)
        var next = 0
        var pending = emptyList<ChatLine>()
        fun flush() { out += pending; pending = emptyList() }
        for (line in lines) {
            if (line is ChatLine.Step || line is ChatLine.End) { out += line; continue }
            flush()
            val at = inputTime(line)
            if (at != null) {
                while (next < feed.size && (time(feed[next].at) ?: Instant.EPOCH) < at) out += feedLine(feed[next++])
            }
            out += line
            if (line is ChatLine.Work) {
                pending = line.steps.mapNotNull { (it as? WorkItem.Tool)?.call?.let(QuickResult::of) }
                    .map { ChatLine.Extra("quick-" + it.callId, AssistantExtra.Quick(it)) }
            }
        }
        flush()
        while (next < feed.size) out += feedLine(feed[next++])
        return days(out, zone)
    }

    private fun feedLine(item: FeedItem) = ChatLine.Extra("feed-" + item.id, AssistantExtra.Feed(item))

    private fun days(lines: List<ChatLine>, zone: ZoneId): List<ChatLine> {
        val out = ArrayList<ChatLine>(lines.size + 4)
        var last: LocalDate? = null
        for (line in lines) {
            val t = lineTime(line)
            if (t != null) {
                val d = t.atZone(zone).toLocalDate()
                if (d != last) {
                    out += ChatLine.Extra("day-$d", AssistantExtra.Day(d))
                    last = d
                }
            }
            out += line
        }
        return out
    }

    private fun inputTime(line: ChatLine): Instant? = when (line) {
        is ChatLine.Row -> if (line.row.type == "user") line.row.start else null
        is ChatLine.MergedUsers -> line.users.firstOrNull()?.start
        is ChatLine.UserImages -> line.images.firstOrNull()?.time
        else -> null
    }

    private fun lineTime(line: ChatLine): Instant? = when (line) {
        is ChatLine.Row -> line.row.start
        is ChatLine.Work -> line.start
        is ChatLine.MergedUsers -> line.users.firstOrNull()?.start
        is ChatLine.ApiError -> line.item.time
        is ChatLine.Extra -> (line.payload as? AssistantExtra.Feed)?.item?.at?.let(::time)
        else -> null
    }

    fun time(iso: String?): Instant? = iso?.let { runCatching { Instant.parse(it) }.getOrNull() }

    /** 今天 · 11月3日 周二, 昨天 · …, or the date. */
    fun dayLabel(date: LocalDate, today: LocalDate = LocalDate.now()): String {
        val week = "日一二三四五六"[date.dayOfWeek.value % 7]
        val day = "${date.monthValue}月${date.dayOfMonth}日 周$week"
        return when (date) {
            today -> "今天 · $day"
            today.minusDays(1) -> "昨天 · $day"
            else -> if (date.year == today.year) day else "${date.year}年$day"
        }
    }
}

/**
 * The conversation's tasks, plans and feed for one hub, kept up to date by its `todos`, `assistant_feed` and
 * `task_plan` events and pulled again on (re)connect.
 */
class TasksStore(private val api: TasksApi, connection: HubConnection, private val scope: CoroutineScope) {
    private val _todos = MutableStateFlow<List<Todo>>(emptyList())
    val todos: StateFlow<List<Todo>> = _todos.asStateFlow()
    private val _todosSupported = MutableStateFlow(true)
    val todosSupported: StateFlow<Boolean> = _todosSupported.asStateFlow()
    private val _plans = MutableStateFlow<Map<String, TaskPlan>>(emptyMap())
    val plans: StateFlow<Map<String, TaskPlan>> = _plans.asStateFlow()
    private val _feeds = MutableStateFlow<Map<String, List<FeedItem>>>(emptyMap())
    val feeds: StateFlow<Map<String, List<FeedItem>>> = _feeds.asStateFlow()
    /** Conversation → the task the next message goes straight to (「对「xx」说」). */
    val routes = MutableStateFlow<Map<String, String>>(emptyMap())
    val busy = MutableStateFlow<Set<String>>(emptySet())
    /** Message cards (inbox item ids) put off with 稍后. */
    val later = MutableStateFlow<Set<String>>(emptySet())
    private val todayAsked = HashMap<String, LocalDate>()

    init {
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.events.collect { e ->
                when (e) {
                    is TodosEvent -> reloadTodos()
                    is AssistantFeedEvent -> received(listOf(e.item))
                    is TaskPlanEvent -> _plans.update { it + (e.plan.id to e.plan) }
                    else -> Unit
                }
            }
        }
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.state.collect { if (it == ConnectionState.Online) { reloadTodos(); _feeds.value.keys.forEach(::load) } }
        }
    }

    fun reloadTodos() {
        scope.launch {
            runCatching { api.todos() }
                .onSuccess { list ->
                    _todosSupported.value = true
                    if (list != _todos.value) _todos.value = list
                }
                .onFailure { error ->
                    val status = (error as? Exception)?.let { e ->
                        val prop = e::class.java.methods.firstOrNull { it.name == "getStatus" }
                        prop?.invoke(e) as? Int
                    }
                    if (status == 404 || error.message?.contains("404") == true) {
                        _todosSupported.value = false
                        _todos.value = emptyList()
                    }
                }
        }
    }

    suspend fun createTodo(title: String, notes: String = ""): Todo {
        val created = api.createTodo(title, notes)
        reloadTodos()
        return created
    }

    suspend fun setTodoDone(todo: Todo, done: Boolean): Todo {
        val before = _todos.value
        val now = Instant.now().toString()
        _todos.update { list -> list.map { if (it.id == todo.id) it.copy(doneAt = if (done) now else null, doneBy = if (done) "user" else null) else it } }
        return try {
            val updated = api.patchTodo(todo.id, done = done)
            _todos.update { list -> list.map { if (it.id == updated.id) updated else it } }
            updated
        } catch (t: Throwable) {
            _todos.value = before
            throw t
        }
    }

    suspend fun updateTodo(id: String, title: String? = null, notes: String? = null): Todo {
        val updated = api.patchTodo(id, title = title, notes = notes)
        _todos.update { list -> list.map { if (it.id == updated.id) updated else it } }
        return updated
    }

    suspend fun deleteTodo(id: String) {
        api.deleteTodo(id)
        _todos.update { list -> list.filterNot { it.id == id } }
    }

    suspend fun linkTodoAgent(todoId: String, machineId: String, sessionId: String) {
        api.linkTodoAgent(todoId, machineId, sessionId)
        reloadTodos()
    }

    suspend fun unlinkTodoAgent(todoId: String, machineId: String, sessionId: String) {
        api.unlinkTodoAgent(todoId, machineId, sessionId)
        reloadTodos()
    }

    fun load(conversation: String) {
        scope.launch {
            val since = _feeds.value[conversation]?.lastOrNull()?.at
            runCatching { api.feed(conversation, since) }.onSuccess { res ->
                if (res.plans.isNotEmpty()) _plans.update { it + res.plans }
                received(res.items, conversation)
            }
        }
    }

    private var main: String? = null

    /** The 今天 card, for the main conversation only (as iOS). */
    fun loadTodayIfMain(conversation: String) {
        scope.launch {
            val id = main ?: runCatching { api.mainConversation() }.getOrNull()?.also { main = it }
            if (id == conversation) loadToday(conversation)
        }
    }

    fun loadToday(conversation: String) {
        val today = LocalDate.now()
        if (todayAsked[conversation] == today) return
        todayAsked[conversation] = today
        scope.launch { runCatching { api.today(conversation) }.onSuccess { received(listOf(it)) }.onFailure { todayAsked.remove(conversation) } }
    }

    private fun received(items: List<FeedItem>, conversation: String? = null) {
        _feeds.update { feeds ->
            var next = conversation?.let { if (it in feeds) feeds else feeds + (it to emptyList()) } ?: feeds
            for ((c, list) in items.groupBy { it.conversation }) next = next + (c to AssistantLines.merge(next[c].orEmpty(), list))
            next
        }
        if (items.any { it.kind == "task" || it.kind == "receipt" }) reloadTodos()
    }

    fun tasksOf(conversation: String): List<Todo> = _todos.value.filter { it.origin == conversation }

    /** The task's agents among [sessions], and its state. */
    fun agentsOf(todo: Todo, sessions: List<Session>): List<Session> =
        todo.activeAgents.mapNotNull { a -> sessions.firstOrNull { it.machineId == a.machineId && it.sessionId == a.sessionId } }

    fun stateOf(todo: Todo, sessions: List<Session>): TaskState =
        agentsOf(todo, sessions).let { a -> TaskState.of(todo, a.map { it.agentState }, a.any { it.interrupted }) }

    /** The agent to open first: the one waiting for the user, then one cut off (#1516), then working, then the latest. */
    fun lead(todo: Todo, sessions: List<Session>): Session? =
        agentsOf(todo, sessions).minWithOrNull(compareBy<Session>({ it.agentState != AgentState.NeedsYou }, { !it.interrupted },
            { it.agentState.priority }).thenByDescending { it.updatedAt.orEmpty() })

    fun startPlan(plan: TaskPlan, remember: Boolean, onError: (Throwable) -> Unit) = act(plan.id, onError) {
        val p = api.startPlan(plan.id, remember); _plans.update { it + (p.id to p) }; reloadTodos()
    }

    fun dismissPlan(plan: TaskPlan, onError: (Throwable) -> Unit) = act(plan.id, onError) {
        val p = api.dismissPlan(plan.id); _plans.update { it + (p.id to p) }
    }

    /**
     * 重试 on a task whose agent did not start (#1396): the hub starts the same task again in the same todo, which it
     * answers reopened — put in at once, so its card shows it starting where it was. Taps while one is on its way do
     * nothing ([busy] under [retryKey]); the hub starts one agent however many arrive. On a task the model service cut
     * off (#1516) it is 继续: the hub tells its agent to go on, once.
     */
    fun retryTask(todo: Todo, onError: (Throwable) -> Unit) = act(retryKey(todo.id), onError) {
        val t = api.retryTask(todo.id)
        _todos.update { list -> list.map { if (it.id == t.id) t else it } }
        reloadTodos()
    }

    /** Removes a task that does not run (one whose agent never started): its todo goes, and any route to it. */
    fun removeTask(todo: Todo, onError: (Throwable) -> Unit) = act("remove:${todo.id}", onError) {
        api.removeTask(todo.id)
        _todos.update { list -> list.filterNot { it.id == todo.id } }
        routes.update { r -> r.filterValues { it != todo.id } }
        reloadTodos()
    }

    private fun act(id: String, onError: (Throwable) -> Unit, block: suspend () -> Unit) {
        if (id in busy.value) return
        busy.update { it + id }
        scope.launch {
            runCatching { block() }.onFailure(onError)
            busy.update { it - id }
        }
    }

    fun setRoute(conversation: String, todoId: String?) {
        routes.update { if (todoId == null) it - conversation else it + (conversation to todoId) }
    }

    /** Sends text straight to the task the conversation is routed to; false when there is none. */
    fun sendRouted(conversation: String, text: String, onError: (Throwable) -> Unit): Boolean {
        val todoId = routes.value[conversation] ?: return false
        setRoute(conversation, null)
        scope.launch { runCatching { api.route(conversation, todoId, text) }.onSuccess { received(listOf(it)) }.onFailure(onError) }
        return true
    }

    /**
     * 发送 on a message card: the (edited) draft goes out through the inbox; the hub records it on the card, whose
     * `assistant_feed` event draws it sent (put in at once here too). Taps while one is on its way, and on a card
     * already sent, do nothing ([busy] under the card's id); a draft with a blank to fill in is not sent.
     */
    fun sendMessage(card: FeedItem, text: String, onError: (Throwable) -> Unit) {
        val itemId = card.ref ?: return
        val latest = { _feeds.value[card.conversation]?.firstOrNull { it.id == card.id } ?: card }
        if (latest().sent != null) return
        draftPlaceholder(text)?.let { onError(IllegalStateException("草稿里还有没填的「$it」")); return }
        act(card.id, onError) {
            api.inboxReply(itemId, text, card.id)
            val now = latest()
            if (now.sent == null) {
                val body = JsonObject(((now.body as? JsonObject) ?: JsonObject(emptyMap())) +
                    mapOf("sent_at" to JsonPrimitive(Instant.now().toString()), "sent" to JsonPrimitive(text)))
                received(listOf(now.copy(body = body)))
            }
        }
    }

    fun putOff(itemId: String) = later.update { it + itemId }

    companion object {
        /** The [busy] key of a task's 重试. */
        fun retryKey(todoId: String) = "retry:$todoId"
    }
}
