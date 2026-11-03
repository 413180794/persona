package persona.core.data

import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class OpenWindowReconciliationTest {
    @Test fun waitsForAllWindowsTimesOutAndDiscardsClosedPairing() = runTest {
        val registry = OpenWindowReconciliation()
        val owner = CoroutineScope(SupervisorJob() + StandardTestDispatcher(testScheduler))
        val chat = CompletableDeferred<Unit>()
        val lark = CompletableDeferred<Unit>()
        registry.register(owner) { chat.await() }
        registry.register(owner) { lark.await() }
        val ready = async { registry.reconcile(100) }
        runCurrent()
        chat.complete(Unit)
        runCurrent()
        assertFalse(ready.isCompleted)
        lark.complete(Unit)
        assertTrue(ready.await())
        registry.register(owner) { awaitCancellation() }
        val timeout = async { registry.reconcile(100) }
        advanceTimeBy(100)
        runCurrent()
        assertFalse(timeout.await())
        owner.cancel()
        assertTrue(registry.reconcile(100))
    }
}
