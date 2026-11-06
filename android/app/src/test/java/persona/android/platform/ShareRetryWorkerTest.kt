package persona.android.platform

import androidx.work.ListenableWorker
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class ShareRetryWorkerTest {
    private val open = listOf("groups/a", "groups/b")

    // #4818: a share staged in group A is delivered while B is selected, from A's own folder.
    @Test fun mismatchedScopeDrainsItsOwnOpenGroup() = runTest {
        val drained = mutableListOf<String>()
        assertEquals(ListenableWorker.Result.success(),
            shareRetryResult("groups/a", "groups/b", open, { it }) { drained += it; false })
        assertEquals(listOf("groups/a"), drained)
        assertEquals(ListenableWorker.Result.retry(), shareRetryResult("groups/a", "groups/b", open, { it }) { true })
    }

    // A deleted or closed group's work ends instead of retrying up to the backoff cap forever.
    @Test fun scopeWithoutOpenGroupSucceedsWithoutDraining() = runTest {
        assertEquals(ListenableWorker.Result.success(),
            shareRetryResult("groups/c", "groups/b", open, { it }) { error("No group to drain") })
    }

    @Test fun selectedScopeRetriesOnlyWhileSharesRemain() = runTest {
        val drained = mutableListOf<String>()
        assertEquals(ListenableWorker.Result.retry(), shareRetryResult("groups/a", "groups/a", emptyList(), { it }) { drained += it; true })
        assertEquals(ListenableWorker.Result.success(), shareRetryResult("groups/a", "groups/a", emptyList(), { it }) { false })
        assertEquals(listOf("groups/a"), drained)
    }
}
