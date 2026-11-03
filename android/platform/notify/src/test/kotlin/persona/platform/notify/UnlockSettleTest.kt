package persona.platform.notify

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class UnlockSettleTest {
    @Test fun anUnlockedPhoneIsUnlockedAtOnce() = runTest {
        var reads = 0
        assertFalse(lockedAfterUnlockSettles({ reads++; false }))
        assertEquals(1, reads)
        assertEquals(0L, currentTime)
    }

    /** SystemUI sends the button as soon as the PIN is accepted; the keyguard reports unlocked a moment later. */
    @Test fun anUnlockThatLetTheButtonThroughIsWaitedFor() = runTest {
        var reads = 0
        assertFalse(lockedAfterUnlockSettles({ ++reads <= 4 }))
        assertEquals(5, reads)
        assertTrue(currentTime < UNLOCK_SETTLE_MILLIS)
    }

    @Test fun aPhoneThatStaysLockedIsLocked() = runTest {
        assertTrue(lockedAfterUnlockSettles({ true }))
        assertEquals(UNLOCK_SETTLE_MILLIS, currentTime)
    }
}
