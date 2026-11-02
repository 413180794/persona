package jarvis.core.domain

import org.junit.Assert.*
import org.junit.Test

class OperationFailureTest {
    @Test fun deliveryEvidenceAndTerminalCodesDetermineRecovery() {
        assertEquals(FailureAction.Verify, mapOperationFailure("input_draft", "unknown", true).action)
        assertEquals(FailureAction.Verify, mapOperationFailure(null, "yes", true).action)
        assertEquals(FailureAction.OpenTerminal, mapOperationFailure("input_draft", "none", true).action)
        assertEquals(FailureAction.Verify, mapOperationFailure("stale_session", "none", true).action)
        assertEquals(FailureAction.PairAgain, mapOperationFailure("device_revoked", "unknown", true).action)
        assertEquals(FailureAction.UpdateHub, mapOperationFailure("incompatible", "none", true).action)
        assertEquals(FailureAction.None, mapOperationFailure("future_code", "none", false).action)
        assertEquals(FailureAction.Retry, mapOperationFailure("future_code", "none", true).action)
        assertEquals(FailureReason.NotDelivered, mapOperationFailure("no_network", null, true, FailureCategory.Network).reason)
        assertEquals(FailureAction.Verify, mapOperationFailure(null, null, true, FailureCategory.Network).action)
    }
}
