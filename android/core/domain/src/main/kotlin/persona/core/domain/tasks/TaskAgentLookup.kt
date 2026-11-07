package persona.core.domain.tasks

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.TasksApi
import java.util.concurrent.atomic.AtomicLong

/** Metadata for the visible conversation's bound agents, outside the filtered Agents window. */
internal class TaskAgentLookup(private val api: TasksApi, scope: CoroutineScope) {
    private val rows = MutableStateFlow<Map<SessionKey, Session>>(emptyMap())
    val sessions = rows.asStateFlow()
    private val failed = MutableStateFlow<Map<SessionKey, Exception>>(emptyMap())
    val failures = failed.asStateFlow()
    private val wanted = MutableStateFlow(emptySet<SessionKey>())
    private val invalidations = Channel<Unit>(Channel.CONFLATED)
    private val generation = AtomicLong()

    init {
        scope.launch {
            for (ignored in invalidations) {
                val epoch = generation.get()
                val keys = wanted.value
                val next = mutableMapOf<SessionKey, Session>()
                val errors = mutableMapOf<SessionKey, Exception>()
                for (key in keys) {
                    try {
                        api.taskAgent(key)?.takeIf { it.key == key }?.let { next[key] = it }
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (error: Exception) {
                        errors[key] = error
                        // A failed metadata read falls back to the durable binding, never to another launch.
                        // Do not retain an old working state when its live lookup failed.
                    }
                }
                if (generation.get() == epoch && wanted.value == keys) { rows.value = next; failed.value = errors }
            }
        }
    }

    fun watch(keys: Set<SessionKey>) {
        if (keys == wanted.value) return
        generation.incrementAndGet()
        wanted.value = keys.toSet()
        rows.value = rows.value.filterKeys { it in keys }
        failed.value = failed.value.filterKeys { it in keys }
        refresh()
    }

    fun refresh() { invalidations.trySend(Unit) }
}
