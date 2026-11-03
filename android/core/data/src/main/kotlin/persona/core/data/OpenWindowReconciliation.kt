package persona.core.data

import kotlinx.coroutines.*

/** Registrations live only as long as their pairing-owned repositories. */
class OpenWindowReconciliation {
    private val participants = mutableMapOf<Any, suspend () -> Unit>()

    fun register(scope: CoroutineScope, reconcile: suspend () -> Unit) {
        val key = Any()
        synchronized(participants) { participants[key] = reconcile }
        requireNotNull(scope.coroutineContext[Job]).invokeOnCompletion {
            synchronized(participants) { participants.remove(key) }
        }
    }

    suspend fun reconcile(timeoutMillis: Long, prerequisites: List<suspend () -> Unit> = emptyList()): Boolean {
        require(timeoutMillis > 0)
        return withTimeoutOrNull(timeoutMillis) {
            supervisorScope {
                val work = prerequisites + synchronized(participants) { participants.values.toList() }
                work.map { reconcile -> async {
                    try { reconcile() }
                    catch (cancelled: CancellationException) { throw cancelled }
                    catch (_: Exception) { awaitCancellation() }
                } }.awaitAll()
                true
            }
        } ?: false
    }
}

interface OpenWindowReconciliationOwner {
    val openWindowReconciliation: OpenWindowReconciliation?
}
