package persona.core.domain.tasks

import persona.core.contracts.CoreTextFailure
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import java.time.Clock
import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import persona.core.contracts.AgentState
import persona.core.contracts.AssistantFeedEvent
import persona.core.contracts.AssistantViewActionEvent
import persona.core.contracts.AssistantViewBlockEvent
import persona.core.contracts.AssistantViewEvent
import persona.core.contracts.FeedItem
import persona.core.contracts.JsonCodec
import persona.core.contracts.Session
import persona.core.contracts.PlanTask
import persona.core.contracts.TaskPlan
import persona.core.contracts.TaskPlanEvent
import persona.core.contracts.Todo
import persona.core.contracts.TodoSpace
import persona.core.contracts.TodosEvent
import persona.core.contracts.draftPlaceholder
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.TasksApi
import persona.core.domain.lines.ApiErrorInfo
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ToolCall
import persona.core.domain.lines.WorkItem
import persona.core.domain.views.AssistantCardActions
import persona.core.domain.views.AssistantViewStore

// Tasks dispatched from an assistant conversation (docs/unified-entry.md; iOS TasksStore / Shared/Tasks.swift, web
// lib/feed.ts): the todos that are tasks, the plans, each conversation's feed, and where those go among the chat's lines.

/** A day divider among the chat's lines: [date], and whether it is today, yesterday, or which year it is in. */
data class DayLabel(val date: LocalDate, val kind: Kind) {
    enum class Kind { Today, Yesterday, ThisYear, OtherYear }
}

/** A draft still holding a blank to fill in ([placeholder], a full-width-bracketed recipient name) is not sent. */
class DraftPlaceholderLeft(val placeholder: String) : IllegalStateException("draft still has a blank: $placeholder"), CoreTextFailure {
    override val text: CoreText get() = CoreText.of(Key.DraftPlaceholderLeft, placeholder)
    override val message: String get() = CoreText.words(text)
}

/** Where a task is, from its todo and its agents' states (docs/todos.md "State rules"). */
enum class TaskState(val label: CoreText, val rank: Int) {
    NeedsYou(CoreText.of(Key.TaskNeedsYou), 0),
    /** An agent's turn was cut off by the model service (#1516): not done; "Continue" tells it to go on. */
    Interrupted(CoreText.of(Key.TaskInterrupted), 0),
    Working(CoreText.of(Key.TaskWorking), 1), NotStarted(CoreText.of(Key.TaskStarting), 1), Unread(CoreText.of(Key.TaskUnread), 2),
    Followup(CoreText.of(Key.TaskStopped), 3), Done(CoreText.of(Key.TaskDone), 4),
    /** Its agent did not start (#1077): the hub ended it with why; "Retry" starts it again (#1396). */
    Failed(CoreText.of(Key.TaskFailed), 4);

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

/** Why the model service cut the agent off (#1516), in words, and whether "Continue" can help; null when it was not. */
data class Interruption(val reason: CoreText, val canContinue: Boolean) {
    companion object {
        fun of(s: Session?): Interruption? {
            if (s?.interrupted != true) return null
            val info = ApiErrorInfo.parse(s.turnError)
            return Interruption(info?.reason ?: CoreText.of(Key.ApiFailed), info?.needsAction != true)
        }
    }
}

/** What a task's lead agent says or does now (iOS TaskNow, web taskNow; docs/unified-entry.md "Task card progress", #1583). */
sealed interface TaskNow {
    val text: String

    data class Words(override val text: String) : TaskNow
    /** [since]: when the step started, while the agent works. */
    data class Step(override val text: String, val since: String?) : TaskNow

    companion object {
        /** A working agent's step (the hub sends one only when it is newer than its words) comes before its words; once it
         *  stopped, its words do, and the step only stands in for words it never said. Null when it said and did nothing. */
        fun of(lead: Session?, working: Boolean): TaskNow? {
            val words = lead?.lastText?.trim().orEmpty()
            val step = lead?.activity?.trim().orEmpty()
            return when {
                step.isNotEmpty() && (working || words.isEmpty()) -> Step(step, if (working) lead?.activityAt else null)
                words.isNotEmpty() -> Words(words)
                else -> null
            }
        }

        /** What each of an agent's running subagents is for: shown under its own progress, never instead of it. */
        fun subagents(s: Session?): List<CoreText> = s?.subagents.orEmpty().filter { it.running }.map {
            (it.description?.trim()?.takeIf(String::isNotEmpty) ?: it.type.takeIf(String::isNotEmpty))?.let(CoreText::plain)
                ?: CoreText.of(Key.Subtask)
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
        is ChatLine.UserImages -> line.images.firstOrNull()?.time
        else -> null
    }

    private fun lineTime(line: ChatLine): Instant? = when (line) {
        is ChatLine.Row -> line.row.start
        is ChatLine.Work -> line.start
        is ChatLine.ApiError -> line.item.time
        is ChatLine.Extra -> (line.payload as? AssistantExtra.Feed)?.item?.at?.let(::time)
        else -> null
    }

    fun time(iso: String?): Instant? = iso?.let { runCatching { Instant.parse(it) }.getOrNull() }

    /** "Today · Tue, Nov 3", "Yesterday · …", or the date: which one, and whether the year shows (the app words it). */
    fun dayLabel(date: LocalDate, today: LocalDate = LocalDate.now()): DayLabel = DayLabel(date, when (date) {
        today -> DayLabel.Kind.Today
        today.minusDays(1) -> DayLabel.Kind.Yesterday
        else -> if (date.year == today.year) DayLabel.Kind.ThisYear else DayLabel.Kind.OtherYear
    })
}

/**
 * The conversation's tasks, plans and feed for one hub, kept up to date by its `todos`, `assistant_feed` and
 * `task_plan` events and pulled again on (re)connect.
 */
/**
 * The agents going on that no task stands for, in the main conversation's dock (#1113; iOS `DockAgent.shown`): working
 * or waiting for the user, not Persona's own, not an agent of a task not done; one the hub tags with a to-do only when
 * that to-do was made by hand. Waiting first, then the newest. Elsewhere none.
 */
fun dockAgents(conversation: String, main: String?, sessions: List<Session>, todos: List<Todo>): List<Session> {
    if (main.isNullOrBlank() || conversation != main) return emptyList()
    val taskAgents = todos.filter { !it.origin.isNullOrBlank() && !it.isDone }.flatMap { t -> t.activeAgents.map { it.key } }.toSet()
    val manual = todos.filter { it.origin.isNullOrBlank() }.map { it.id }.toSet()
    return sessions.filter { !it.assistant && (it.agentState == AgentState.NeedsYou || it.agentState == AgentState.Working) }
        .filter { it.key !in taskAgents && (it.todo?.id?.takeIf(String::isNotBlank)?.let { id -> id in manual } ?: true) }
        .sortedWith(compareBy<Session> { it.agentState.priority }.thenByDescending { it.stateSince.orEmpty() }.thenBy { it.key.path })
}

/** Whether a task may be removed from the dock or its detail: not while anything of it runs (iOS `isRemovable`). */
val TaskState.isRemovable: Boolean get() = this != TaskState.Working && this != TaskState.NeedsYou

/**
 * The to-dos last read from this pairing's hub, kept on the phone (iOS TodoStore's StateCache): a cold start or an open
 * without the hub shows them instead of "No to-dos" until the hub answers.
 */
interface TodoCache {
    suspend fun read(): List<Todo>?
    suspend fun write(todos: List<Todo>)
}

/** How long a Today card is held before an open asks the hub again whether its day has moved on (#5194). */
private val todayRecheck = Duration.ofMinutes(10)

class TasksStore(
    private val api: TasksApi,
    connection: HubConnection,
    private val scope: CoroutineScope,
    private val cache: TodoCache? = null,
    private val clock: Clock = Clock.systemDefaultZone(),
) {
    private val _todos = MutableStateFlow<List<Todo>>(emptyList())
    val todos: StateFlow<List<Todo>> = _todos.asStateFlow()
    /** Guards [answered] with the list: the cached list never replaces one the hub has given. */
    private val todosLock = Any()
    /** The hub's last answer (a list, or an older hub's 404 as none); null until it answers, while the cache shows. */
    private var answered: List<Todo>? = null
    private val writing = Mutex()
    private var written: List<Todo>? = null
    private val _todosSupported = MutableStateFlow(true)
    val todosSupported: StateFlow<Boolean> = _todosSupported.asStateFlow()
    private val _plans = MutableStateFlow<Map<String, TaskPlan>>(emptyMap())
    val plans: StateFlow<Map<String, TaskPlan>> = _plans.asStateFlow()
    private val _feeds = MutableStateFlow<Map<String, List<FeedItem>>>(emptyMap())
    val feeds: StateFlow<Map<String, List<FeedItem>>> = _feeds.asStateFlow()
    /** Conversations whose whole feed has been read once: a live item alone does not make a later load incremental (#4689). */
    private val loaded = MutableStateFlow<Set<String>>(emptySet())
    /** Conversation → the task the next message goes straight to ("Say to 'xx'"). */
    val routes = MutableStateFlow<Map<String, String>>(emptyMap())
    val busy = MutableStateFlow<Set<String>>(emptySet())
    /** Message cards (inbox item ids) put off with "Later". */
    val later = MutableStateFlow<Set<String>>(emptySet())
    /** Conversation → when the Today card was last asked for, and the day of the card the hub gave (null while asking). */
    private val todayAsked = HashMap<String, Pair<Instant, LocalDate?>>()
    /** This hub's assistant cards (docs/assistant-views.md): live cards, answers and registered reply blocks. */
    val cards = AssistantViewStore()
    val cardActions = AssistantCardActions(api, cards, reload = { load(it, whole = true) })

    init {
        if (cache != null) scope.launch(start = CoroutineStart.UNDISPATCHED) {
            val cached = try { cache.read() } catch (e: CancellationException) { throw e } catch (_: Exception) { null }
            if (!cached.isNullOrEmpty()) synchronized(todosLock) {
                if (answered == null && _todos.value.isEmpty()) { written = cached; _todos.value = cached }
            }
        }
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.events.collect { e ->
                when (e) {
                    is TodosEvent -> reloadTodos()
                    is AssistantFeedEvent -> received(listOf(e.item))
                    is TaskPlanEvent -> _plans.update { it + (e.plan.id to e.plan) }
                    is AssistantViewEvent -> cards.view(e.conversation, e.view)
                    is AssistantViewActionEvent -> cards.answer(e.conversation, e.action)
                    is AssistantViewBlockEvent -> cards.block(e.conversation, e.key)
                    else -> Unit
                }
            }
        }
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            connection.state.collect { if (it == ConnectionState.Online) { reloadTodos(); loaded.value.forEach(::load) } }
        }
    }

    fun reloadTodos() {
        scope.launch {
            runCatching { api.todos() }
                .onSuccess { list ->
                    _todosSupported.value = true
                    todosLoaded = true
                    synchronized(todosLock) { answered = list; if (list != _todos.value) _todos.value = list }
                    dropStaleRoutes()
                    remember()
                }
                .onFailure { error ->
                    val status = (error as? Exception)?.let { e ->
                        val prop = e::class.java.methods.firstOrNull { it.name == "getStatus" }
                        prop?.invoke(e) as? Int
                    }
                    if (status == 404 || error.message?.contains("404") == true) {
                        _todosSupported.value = false
                        synchronized(todosLock) { answered = emptyList(); _todos.value = emptyList() }
                        remember()
                    }
                }
        }
    }

    /**
     * Keeps the hub's latest answer for the next start, one write at a time (two reads can answer out of order); a failed
     * write only costs that start its first paint.
     */
    private suspend fun remember() {
        if (cache == null) return
        writing.withLock {
            val latest = synchronized(todosLock) { answered } ?: return
            if (latest == written) return
            try { cache.write(latest); written = latest }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { }
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
        dropStaleRoutes()
    }

    /** Routes to to-dos no longer listed (deleted here or on another device) go: their chip is hidden already. */
    private fun dropStaleRoutes() {
        val ids = _todos.value.mapTo(HashSet()) { it.id }
        routes.update { r -> r.filterValues { it in ids }.takeIf { it.size != r.size } ?: r }
    }

    suspend fun linkTodoAgent(todoId: String, machineId: String, sessionId: String) {
        api.linkTodoAgent(todoId, machineId, sessionId)
        reloadTodos()
    }

    suspend fun unlinkTodoAgent(todoId: String, machineId: String, sessionId: String) {
        api.unlinkTodoAgent(todoId, machineId, sessionId)
        reloadTodos()
    }

    /** A read of the to-dos has answered: until then nothing tells whether the hub knows projects. */
    @Volatile private var todosLoaded = false
    /** The hub answered "Attach to to-do" with its router's 404: it has no such route (iOS TodosLogic.isLegacySpacesRoute). */
    @Volatile private var legacySpaces = false

    /**
     * Whether this hub knows projects on todos (#1616): an older one leaves `spaces` out altogether (iOS supportsSpaces).
     * False until the first read answers, and for good once the hub has no route for it.
     */
    val spacesSupported: Boolean
        get() = todosLoaded && !legacySpaces && _todos.value.let { list -> list.isEmpty() || list.any { it.spaces != null } }

    /**
     * "Attach to to-do" / "Remove from to-do" a whole project. A project is on one todo at a time: linking it takes it, and the agents it
     * brought, off any other first. The change shows at once; the hub then supplies the agents it attached.
     */
    suspend fun setTodoSpace(todoId: String, machineId: String, space: String, linked: Boolean) {
        val before = _todos.value
        val via = "$machineId/$space"
        _todos.update { list ->
            list.map { todo ->
                if (!linked && todo.id != todoId) return@map todo
                val others = todo.spaces?.filterNot { it.machineId == machineId && it.space == space }
                val spaces = if (linked && todo.id == todoId) others.orEmpty() + TodoSpace(machineId, space) else others
                val agents = if (!linked || todo.id != todoId) todo.agents.filterNot { it.source == "space" && it.via == via } else todo.agents
                todo.copy(spaces = spaces, agents = agents)
            }
        }
        try {
            if (linked) api.linkTodoSpace(todoId, machineId, space) else api.unlinkTodoSpace(todoId, machineId, space)
        } catch (t: Throwable) {
            _todos.value = before
            if (isLegacySpacesRoute(t)) legacySpaces = true
            throw t
        } finally {
            reloadTodos()
        }
    }

    /**
     * Pulls [conversation]'s feed: the items after the last one held (all of them with [whole] or before its first read; from the
     * oldest unsent message card on while it holds one, whose `filed_at` the hub works out on each read and sends no event for,
     * #5693 — not from the start, as the hub pages oldest first), and every time the full set of its cards, answers and registered blocks.
     */
    fun load(conversation: String, whole: Boolean = false) {
        scope.launch {
            val held = _feeds.value[conversation].orEmpty()
            val unsent = held.indexOfFirst { it.kind == "message" && it.sent == null }
            val since = when {
                whole || conversation !in loaded.value -> null
                unsent >= 0 -> held.take(unsent).lastOrNull { it.at != held[unsent].at }?.at // reads are after `since`: skip ties
                else -> held.lastOrNull()?.at
            }
            runCatching { api.feed(conversation, since) }.onSuccess { res ->
                if (res.plans.isNotEmpty()) _plans.update { it + res.plans }
                cards.snapshot(conversation, res.views, res.viewActions, res.viewBlocks)
                received(res.items, conversation)
                loaded.update { it + conversation }
            }
        }
    }

    private var main: String? = null
    /** The main assistant conversation's id once read: its dock also shows the agents no task stands for (#1113). */
    val mainConversation = MutableStateFlow<String?>(null)

    /**
     * The "Today" card, for the main conversation only (as iOS). A conversation that isn't the one held asks the hub again:
     * "New main chat", a move to another machine or another device replaces the main (#4690).
     */
    fun loadTodayIfMain(conversation: String) {
        scope.launch {
            val id = main.takeIf { it == conversation }
                ?: runCatching { api.mainConversation() }.getOrNull()?.also { main = it; mainConversation.value = it }
            if (id == conversation) loadToday(conversation)
        }
    }

    /** What [conversation]'s dock shows after its tasks (iOS `DockAgent.shown`); see [dockAgents]. */
    fun agentsOf(conversation: String, sessions: List<Session>): List<Session> =
        dockAgents(conversation, mainConversation.value, sessions, _todos.value)

    /**
     * The hub counts the day in the assistant's time zone, which need not be the phone's (#5194): the card is asked
     * again on the phone's new day, and on an open [todayRecheck] after the last ask unless the card's day is already
     * past the phone's (that hub cannot move on before the phone does).
     */
    fun loadToday(conversation: String) {
        val now = clock.instant()
        val today = LocalDate.now(clock)
        todayAsked[conversation]?.let { (at, day) ->
            if (LocalDate.ofInstant(at, clock.zone) == today &&
                (day == null || day > today || now < at + todayRecheck)) return
        }
        todayAsked[conversation] = now to null
        scope.launch {
            runCatching { api.today(conversation) }.onSuccess { card ->
                todayAsked[conversation] = now to (card.ref?.let { runCatching { LocalDate.parse(it) }.getOrNull() } ?: today)
                received(listOf(card))
            }.onFailure { todayAsked.remove(conversation) }
        }
    }

    private fun received(items: List<FeedItem>, conversation: String? = null) {
        // A live card's creation item carries the card as it was then: drawn even if its assistant_view was missed.
        items.filter { it.kind == "view" }.forEach { item ->
            ((item.body as? JsonObject)?.get("view"))?.let { cards.view(item.conversation, it) }
        }
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

    /** [edits] is "Edit": the plan's tasks as the user changed them, by index (null: as planned). */
    fun startPlan(plan: TaskPlan, remember: Boolean, onError: (Throwable) -> Unit, edits: List<PlanTask>? = null) = act(plan.id, onError) {
        val p = if (edits == null) api.startPlan(plan.id, remember) else api.startPlan(plan.id, remember, edits)
        _plans.update { it + (p.id to p) }; reloadTodos()
    }

    fun dismissPlan(plan: TaskPlan, onError: (Throwable) -> Unit) = act(plan.id, onError) {
        val p = api.dismissPlan(plan.id); _plans.update { it + (p.id to p) }
    }

    /**
     * "Retry" on a task whose agent did not start (#1396): the hub starts the same task again in the same todo, which it
     * answers reopened — put in at once, so its card shows it starting where it was. Taps while one is on its way do
     * nothing ([busy] under [retryKey]); the hub starts one agent however many arrive. On a task the model service cut
     * off (#1516) it is "Continue": the hub tells its agent to go on, once.
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

    /** Sends text straight to the task the conversation is routed to; false when there is none or its to-do is gone. */
    fun sendRouted(conversation: String, text: String, onError: (Throwable) -> Unit): Boolean {
        val todoId = routes.value[conversation] ?: return false
        setRoute(conversation, null)
        if (_todos.value.none { it.id == todoId }) return false // the hub would refuse it (409): Persona gets it instead
        scope.launch { runCatching { api.route(conversation, todoId, text) }.onSuccess { received(listOf(it)) }.onFailure(onError) }
        return true
    }

    /**
     * "Send" on a message card: the (edited) draft goes out through the inbox; the hub records it on the card, whose
     * `assistant_feed` event draws it sent (put in at once here too). Taps while one is on its way, and on a card
     * already sent or dealt with elsewhere (`filed_at`, #5095), do nothing ([busy] under the card's id); a draft with a
     * blank to fill in is not sent.
     */
    fun sendMessage(card: FeedItem, text: String, onError: (Throwable) -> Unit) {
        val itemId = card.ref ?: return
        val latest = { _feeds.value[card.conversation]?.firstOrNull { it.id == card.id } ?: card }
        if (!latest().canSend) return
        draftPlaceholder(text)?.let { onError(DraftPlaceholderLeft(it)); return }
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
        /** The [busy] key of a task's "Retry". */
        fun retryKey(todoId: String) = "retry:$todoId"
    }
}

/**
 * A 404 from the hub's router rather than its handler (iOS TodosLogic.isLegacySpacesRoute): no code at all, or the
 * generic `not_found` with "Not Found". A missing to-do is a 404 with its own message, and keeps the action.
 */
internal fun isLegacySpacesRoute(error: Throwable): Boolean {
    fun read(name: String): Any? = runCatching { error::class.java.methods.firstOrNull { it.name == name && it.parameterCount == 0 }?.invoke(error) }.getOrNull()
    if (read("getStatus") as? Int != 404) return false
    val code = (read("getCode") as? String).orEmpty()
    return code.isEmpty() || (code == "not_found" && error.message == "Not Found")
}
