package persona.feature.assistant.proactive

import androidx.annotation.StringRes
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.contracts.api.AssistantManagement
import persona.core.contracts.api.Followup
import persona.core.contracts.api.Routine
import persona.core.contracts.api.Run
import persona.feature.assistant.R
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.time.Instant
import java.util.UUID

/**
 * What 主动做事 needs from the hub (docs/protocol.md, the assistant's routines, follow-ups and runs). A seam of its
 * own, so the page and its tests do not follow every change of the wider [AssistantManagement].
 */
internal interface ProactiveApi {
    suspend fun routines(): List<Routine>
    /** Replaces the hub's whole table; the hub keeps its own fields (last run, next run, who made it) by id. */
    suspend fun setRoutines(list: List<Routine>): List<Routine>
    suspend fun runRoutine(id: String): SessionKey
    suspend fun followups(): List<Followup>
    suspend fun cancelFollowup(id: String)
    suspend fun runFollowup(id: String): SessionKey
    /** Newest first, every routine and follow-up; one routine's history is filtered by `ref_id`. */
    suspend fun runs(): List<Run>
    /** Where assistant conversations run, for a routine's last run (routines do not say which machine). */
    suspend fun assistantMachine(): String
}

internal fun AssistantManagement.proactive(): ProactiveApi = object : ProactiveApi {
    val api = this@proactive
    override suspend fun routines() = api.routines()
    override suspend fun setRoutines(list: List<Routine>) = api.setRoutines(list)
    override suspend fun runRoutine(id: String) = api.runRoutine(id)
    override suspend fun followups() = api.followups()
    override suspend fun cancelFollowup(id: String) = api.cancelFollowup(id)
    override suspend fun runFollowup(id: String) = api.runFollowup(id)
    override suspend fun runs() = api.runs()
    override suspend fun assistantMachine() = api.notes().machineId
}


internal val Workdays = listOf(1, 2, 3, 4, 5)
private val TimePattern = Regex("^([01]\\d|2[0-3]):[0-5]\\d$")
private val ShortHour = Regex("^\\d:[0-5]\\d$")

/** "HH:MM" as the editor writes it. */
internal fun validTime(time: String) = TimePattern.matches(time)

/** The hub also takes "9:00" (Persona writes those); the editor shows and writes "09:00". */
internal fun normalizeTime(time: String) = time.trim().let { if (ShortHour.matches(it)) "0$it" else it }

/** Days 1 = Monday … 7 = Sunday in order; all seven are written as none, which the hub reads as every day. */
internal fun normalizeDays(days: List<Int>): List<Int> =
    days.filter { it in 1..7 }.distinct().sorted().takeUnless { it.size == 7 }.orEmpty()

/** The fields a person edits. Everything else on a routine belongs to the hub and is never changed here. */
internal data class RoutineDraft(
    val title: String = "",
    val prompt: String = "",
    val time: String = "09:00",
    val weekdays: List<Int> = Workdays,
    val enabled: Boolean = true,
) {
    val valid: Boolean get() = title.isNotBlank() && prompt.isNotBlank() && validTime(normalizeTime(time))
    fun normalized() = copy(title = title.trim(), prompt = prompt.trim(), time = normalizeTime(time), weekdays = normalizeDays(weekdays))
    /** Same schedule and words, ignoring surrounding spaces, "9:00" against "09:00" and how "every day" is spelled. */
    fun sameAs(other: RoutineDraft) = normalized() == other.normalized()
}

internal fun Routine.draft() = RoutineDraft(title, prompt, normalizeTime(time), weekdays, enabled)

private fun Routine.with(draft: RoutineDraft) = draft.normalized().let {
    copy(title = it.title, prompt = it.prompt, time = it.time, weekdays = it.weekdays, enabled = it.enabled)
}

/** Only what the person changed between [from] and [to]; fields changed on the hub meanwhile (by Persona) stay. */
private fun Routine.patched(from: RoutineDraft, to: RoutineDraft): Routine {
    val before = from.normalized()
    val after = to.normalized()
    return copy(
        title = if (before.title != after.title) after.title else title,
        prompt = if (before.prompt != after.prompt) after.prompt else prompt,
        time = if (before.time != after.time) after.time else time,
        weekdays = if (before.weekdays != after.weekdays) after.weekdays else weekdays,
        enabled = if (before.enabled != after.enabled) after.enabled else enabled,
    )
}

/** A new routine's id, made here: a retry after a lost answer then overwrites it instead of adding a second one. */
internal fun newRoutineId(): String = "rt_" + UUID.randomUUID().toString().replace("-", "").take(8)

/**
 * One edit to the routine table. The hub replaces the whole table on PUT, so every change is applied to a fresh read
 * right before writing: routines Persona added meanwhile survive, and nothing but the edited fields changes.
 */
internal sealed interface RoutineChange {
    /** The routine it edits (a new one's id is made in the app, see [newRoutineId]). */
    val id: String
    /** The table with this change, or null when the routine it edits is gone (deleted elsewhere meanwhile). */
    fun applyTo(routines: List<Routine>): List<Routine>?
    /** Whether [routines] already shows this change, so a lost response is not reported as a failure. */
    fun satisfiedBy(routines: List<Routine>): Boolean

    data class Toggle(override val id: String, val enabled: Boolean) : RoutineChange {
        override fun applyTo(routines: List<Routine>) =
            if (routines.none { it.id == id }) null else routines.map { if (it.id == id) it.copy(enabled = enabled) else it }
        override fun satisfiedBy(routines: List<Routine>) = routines.firstOrNull { it.id == id }?.enabled == enabled
    }

    /** [from] is the routine as the editor opened it, [to] what is on screen now. */
    data class Edit(override val id: String, val from: RoutineDraft, val to: RoutineDraft) : RoutineChange {
        override fun applyTo(routines: List<Routine>) =
            if (routines.none { it.id == id }) null else routines.map { if (it.id == id) it.patched(from, to) else it }
        override fun satisfiedBy(routines: List<Routine>) =
            routines.firstOrNull { it.id == id }?.let { it.patched(from, to).draft().sameAs(it.draft()) } == true
    }

    /** Appended at the end, which is where the hub keeps it; added again (a retry) it only updates itself. */
    data class Add(override val id: String, val draft: RoutineDraft) : RoutineChange {
        override fun applyTo(routines: List<Routine>) =
            if (routines.any { it.id == id }) routines.map { if (it.id == id) it.with(draft) else it }
            else routines + Routine(id = id).with(draft)
        override fun satisfiedBy(routines: List<Routine>) = routines.any { it.id == id }
    }

    data class Delete(override val id: String) : RoutineChange {
        override fun applyTo(routines: List<Routine>) = routines.filterNot { it.id == id }
        override fun satisfiedBy(routines: List<Routine>) = routines.none { it.id == id }
    }
}

/** A list read from the hub: the last good value stays on screen when a refresh fails. */
internal data class Loadable<T>(val value: T? = null, val error: String? = null) {
    fun failed(message: String) = copy(error = message)
}

internal data class PendingChange(val token: Long, val change: RoutineChange)

/** A routine's last run, from the history when there is one (the hub's 立即运行 does not move `last_run`). */
internal data class LastRun(
    val at: Instant?, val outcome: RunOutcome, val error: String?, val conversation: SessionKey?, val manual: Boolean = false,
)

internal data class ProactiveState(
    val routines: Loadable<List<Routine>> = Loadable(),
    val followups: Loadable<List<Followup>> = Loadable(),
    val runs: Loadable<List<Run>> = Loadable(),
    /** Switches flipped on screen whose write has not come back yet. */
    val pending: List<PendingChange> = emptyList(),
    /** Actions in flight: `routine:<id>` (a write), `run:<id>` (立即运行), `followup:<id>`. */
    val busy: Set<String> = emptySet(),
    val refreshing: Boolean = false,
    val assistantMachine: String? = null,
    /** When routines or follow-ups were last read: what is due after this is worth reading again for. */
    val readAt: Instant? = null,
) {
    /** The hub's table with the switches still in flight already flipped. */
    val shownRoutines: List<Routine>? get() = routines.value?.let { table ->
        pending.fold(table) { list, pending -> pending.change.applyTo(list) ?: list }
    }
    val followupList: List<Followup>? get() = followups.value?.sortedWith(compareBy<Followup> { parseTime(it.at) }.thenBy { it.id })
    val runList: List<Run>? get() = runs.value?.sortedWith(compareByDescending<Run> { parseTime(it.at) })
    fun runsOf(ref: String): List<Run>? = runList?.filter { it.refId == ref }
    fun routineRunning(id: String) = "run:$id" in busy
    fun routineBusy(id: String) = "routine:$id" in busy || routineRunning(id)
    fun followupBusy(id: String) = "followup:$id" in busy
    /** True once something has been read, so the page can tell "nothing scheduled" from "not loaded yet". */
    val loaded: Boolean get() = routines.value != null || followups.value != null

    fun lastRunOf(routine: Routine): LastRun? {
        runsOf(routine.id)?.firstOrNull()?.let { run ->
            return LastRun(parseTime(run.at), run.outcome, run.error, run.conversation(assistantMachine), run.manual)
        }
        val at = parseTime(routine.lastRun) ?: return null
        val session = routine.lastSessionId?.takeIf { it.isNotBlank() }
        return LastRun(at, outcomeOf(routine.lastStatus), routine.lastError,
            session?.let { id -> assistantMachine?.let { SessionKey(it, id) } })
    }
}

/** The routine was deleted meanwhile; [failureReason] says so in the page's language. */
internal class RoutineGoneException : Exception("routine is gone")

/**
 * The page's state and every hub call it makes. Reads and writes of the routine table go one at a time, and a write
 * that has started finishes even when the page goes away. A failed write is checked against a fresh read and only
 * reported when the hub really does not show it. [scope] should outlive the page (the pairing's).
 */
internal class ProactiveStore(
    private val api: ProactiveApi,
    private val scope: CoroutineScope,
    /** Says failures in the page's language. */
    private val say: ProactiveText,
    private val clock: () -> Instant = Instant::now,
) {
    private val mutable = MutableStateFlow(ProactiveState())
    val state: StateFlow<ProactiveState> = mutable.asStateFlow()
    private val notices = MutableSharedFlow<String>(extraBufferCapacity = 4, onBufferOverflow = BufferOverflow.DROP_OLDEST)
    /** Failures worth telling the user about, in words for this page. */
    val messages: SharedFlow<String> = notices.asSharedFlow()
    private val table = Mutex()
    private var tokens = 0L

    /** Work that should not stop when the page does (a run the user asked for, a cancel, a toggle). */
    fun launch(block: suspend ProactiveStore.() -> Unit): Job = scope.launch { block() }

    suspend fun refresh() {
        mutable.update { it.copy(refreshing = true) }
        try {
            coroutineScope {
                launch { loadRoutines() }
                launch { loadFollowups() }
                launch { loadRuns() }
                launch { loadMachine() }
            }
        } finally {
            mutable.update { it.copy(refreshing = false) }
        }
    }

    /** Waits for a write in flight, so a slow read cannot put back what that write replaced. */
    suspend fun loadRoutines() = table.withLock { readRoutines() }

    private suspend fun readRoutines(): List<Routine>? {
        val result = attempt { api.routines() }
        result.onSuccess { list -> mutable.update { it.copy(routines = Loadable(list), readAt = clock()) } }
            .onFailure { error -> mutable.update { it.copy(routines = it.routines.failed(loadFailure(say, error))) } }
        return result.getOrNull()
    }

    suspend fun loadFollowups() {
        attempt { api.followups() }
            .onSuccess { list -> mutable.update { it.copy(followups = Loadable(list), readAt = clock()) } }
            .onFailure { error -> mutable.update { it.copy(followups = it.followups.failed(loadFailure(say, error))) } }
    }

    suspend fun loadRuns() {
        attempt { api.runs() }
            .onSuccess { list -> mutable.update { it.copy(runs = Loadable(list)) } }
            .onFailure { error -> mutable.update { it.copy(runs = it.runs.failed(loadFailure(say, error))) } }
    }

    private suspend fun loadMachine() {
        attempt { api.assistantMachine() }.onSuccess { id -> mutable.update { it.copy(assistantMachine = id.ifBlank { null }) } }
    }

    /** The switch moves at once; the write follows, and a failure puts back what the hub has. */
    fun toggle(id: String, enabled: Boolean) {
        val pending = PendingChange(++tokens, RoutineChange.Toggle(id, enabled))
        mutable.update { it.copy(pending = it.pending + pending) }
        scope.launch {
            try {
                commit(pending.change).onFailure { error -> report(R.string.assistant_proactive_not_saved, error) }
            } finally {
                mutable.update { it.copy(pending = it.pending - pending) }
            }
        }
    }

    /** For the editor, which stays open until the hub has the change. */
    suspend fun save(change: RoutineChange): Result<List<Routine>> {
        val busy = "routine:${change.id}"
        mutable.update { it.copy(busy = it.busy + busy) }
        try {
            return commit(change).onFailure { error -> report(if (change is RoutineChange.Delete) R.string.assistant_proactive_not_deleted else R.string.assistant_proactive_not_saved, error) }
        } finally {
            mutable.update { it.copy(busy = it.busy - busy) }
        }
    }

    /** Read, change, write, under the table lock; once started it is not cancelled halfway. */
    private suspend fun commit(change: RoutineChange): Result<List<Routine>> = withContext(NonCancellable) {
        table.withLock {
            val written = attempt {
                val fresh = api.routines()
                val next = change.applyTo(fresh) ?: throw RoutineGoneException()
                if (next == fresh) fresh else api.setRoutines(next)
            }
            written.onSuccess { list -> mutable.update { it.copy(routines = Loadable(list), readAt = clock()) } }
            if (written.isSuccess) return@withLock written
            // The answer may have been lost on the way back: believe a fresh read over the error.
            val fresh = readRoutines()
            if (fresh != null && written.exceptionOrNull() !is RoutineGoneException && change.satisfiedBy(fresh)) Result.success(fresh)
            else written
        }
    }

    suspend fun runRoutine(id: String): Result<SessionKey> {
        mutable.update { it.copy(busy = it.busy + "run:$id") }
        val result = try { attempt { api.runRoutine(id) } } finally { mutable.update { it.copy(busy = it.busy - "run:$id") } }
        result.onFailure { report(R.string.assistant_proactive_run_failed, it) }
        // A run, failed or not, changes the routine's last run and the history.
        scope.launch { loadRoutines(); loadRuns() }
        return result
    }

    suspend fun runFollowup(id: String): Result<SessionKey> {
        mutable.update { it.copy(busy = it.busy + "followup:$id") }
        val result = try { attempt { api.runFollowup(id) } } finally { mutable.update { it.copy(busy = it.busy - "followup:$id") } }
        if (result.isSuccess) mutable.update { state ->
            state.copy(followups = state.followups.copy(value = state.followups.value?.filterNot { it.id == id }))
        }
        result.onFailure { report(R.string.assistant_proactive_followup_start_failed, it) }
        // The hub takes a follow-up off its list before running it, so a failed run may still have removed it.
        scope.launch { loadFollowups(); loadRuns() }
        return result
    }

    suspend fun cancelFollowup(id: String): Result<Unit> {
        mutable.update { it.copy(busy = it.busy + "followup:$id") }
        val result = try {
            val first = attempt { api.cancelFollowup(id) }
            if (first.isSuccess) first else {
                // Gone already (it ran, or was cancelled elsewhere): what the user asked for is true.
                val fresh = attempt { api.followups() }.getOrNull()
                if (fresh != null) mutable.update { it.copy(followups = Loadable(fresh)) }
                if (fresh != null && fresh.none { it.id == id }) Result.success(Unit) else first
            }
        } finally {
            mutable.update { it.copy(busy = it.busy - "followup:$id") }
        }
        if (result.isSuccess) mutable.update { state ->
            state.copy(followups = state.followups.copy(value = state.followups.value?.filterNot { it.id == id }))
        }
        result.onFailure { report(R.string.assistant_proactive_followup_cancel_failed, it) }
        return result
    }

    /** [what] is a sentence with the reason as `%1$s` ("没保存上：%1$s"). */
    private fun report(@StringRes what: Int, error: Throwable) {
        notices.tryEmit(say.string(what, failureReason(say, error)))
    }
}

/**
 * Failures become results. Cancellation of the caller still propagates; the transport's own cancellation (the pairing
 * or route changed under a request) is a failure of this call, so the caller can clear its spinner and say so.
 */
internal suspend inline fun <T> attempt(block: () -> T): Result<T> = try {
    Result.success(block())
} catch (cancelled: CancellationException) {
    currentCoroutineContext().ensureActive()
    Result.failure(cancelled)
} catch (error: Exception) {
    Result.failure(error)
}
