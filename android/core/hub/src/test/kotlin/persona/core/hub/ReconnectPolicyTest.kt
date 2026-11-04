package persona.core.hub

import org.junit.Assert.*
import org.junit.Test

class ReconnectPolicyTest {
    private val policy = ReconnectPolicy()

    @Test fun aRestartIsRetriedSoonThenSteadilyWithoutGrowing() {
        assertEquals(150, policy.restartDelay(0) { 0.0 })
        assertEquals(400, policy.restartDelay(0) { 1.0 })
        for (attempt in 1..40) {
            assertEquals(500, policy.restartDelay(attempt) { 0.0 })
            assertEquals(1000, policy.restartDelay(attempt) { 1.0 })
        }
        // Randomness out of range never leaves the bounds.
        assertEquals(1000, policy.restartDelay(3) { 7.0 })
        assertEquals(150, policy.restartDelay(0) { -1.0 })
    }

    @Test fun theRestartWindowLastsThirtySeconds() {
        assertFalse(policy.restarting(null, 5_000))
        assertTrue(policy.restarting(1_000, 1_000))
        assertTrue(policy.restarting(1_000, 30_999))
        assertFalse(policy.restarting(1_000, 31_000))
        // A clock that went backwards is not a restart in progress.
        assertFalse(policy.restarting(1_000, 999))
    }

    @Test fun onlyConnectingStatesGetTheLongGrace() {
        assertEquals(8_000, policy.graceMillis(connecting = true, restarting = false))
        assertEquals(30_000, policy.graceMillis(connecting = true, restarting = true))
        assertEquals(1_500, policy.graceMillis(connecting = false, restarting = false))
        assertEquals(1_500, policy.graceMillis(connecting = false, restarting = true))
    }

    @Test fun aFreshStartSurfacesOnlyAfterADropUsersWouldHaveSeen() {
        assertFalse(policy.surfaceFreshStart(null))
        assertFalse(policy.surfaceFreshStart(1_499))
        assertTrue(policy.surfaceFreshStart(1_500))
    }
}
