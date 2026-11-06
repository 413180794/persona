package persona.platform.phonetools

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class UnlockSettleTest {
    /** "Allow" on the lock screen comes as soon as the PIN is accepted; the keyguard reports unlocked a moment later. */
    @Test fun anUnlockThatLetAllowThroughIsWaitedFor() = runTest {
        var reads = 0
        assertFalse(lockedAfterUnlockSettles({ ++reads <= 3 }))
        assertTrue(currentTime < UNLOCK_SETTLE_MILLIS)
    }

    @Test fun anUnlockedPhoneAnswersAtOnceAndALockedOneNever() = runTest {
        assertFalse(lockedAfterUnlockSettles({ false }))
        assertEquals(0L, currentTime)
        assertTrue(lockedAfterUnlockSettles({ true }))
        assertEquals(UNLOCK_SETTLE_MILLIS, currentTime)
    }
}
