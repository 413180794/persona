package persona.platform.notify

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ForegroundStartsTest {
    @Test fun aStopWithNothingPendingStopsAtOnce() {
        val starts = ForegroundStarts()
        assertFalse(starts.deferStop())
    }

    // Pairing replaced under load: the runtime asked to start, then (paired went false) to stop, before the service
    // promoted. Stopping then crashed the app; now the start promotes and stops itself.
    @Test fun aStopWhileAStartIsPendingIsLeftToThatStart() {
        val starts = ForegroundStarts()
        starts.requested()
        assertTrue(starts.deferStop())
        assertTrue("the pending start stops itself after promoting", starts.promoted())
        assertFalse("nothing pending any more", starts.deferStop())
    }

    @Test fun aStartAfterTheStopWins() {
        val starts = ForegroundStarts()
        starts.requested()
        assertTrue(starts.deferStop())
        starts.requested()
        assertFalse(starts.promoted())
        assertFalse(starts.promoted())
    }

    @Test fun withTwoStartsPendingBothSeeTheStopUntilTheLastOneHasPromoted() {
        val starts = ForegroundStarts()
        starts.requested()
        starts.requested()
        assertTrue(starts.deferStop())
        // stopSelf(startId) in the first one does nothing while the second start is still pending.
        assertTrue(starts.promoted())
        assertTrue(starts.promoted())
        assertFalse(starts.promoted())
    }

    @Test fun aStartThatCouldNotBeSentLeavesNothingPending() {
        val starts = ForegroundStarts()
        starts.requested()
        starts.failed()
        assertFalse(starts.deferStop())
    }
}
