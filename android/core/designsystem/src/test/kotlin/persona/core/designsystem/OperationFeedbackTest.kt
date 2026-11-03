package persona.core.designsystem

import persona.core.contracts.api.ConnectionState
import persona.core.domain.FailureAction
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class OperationFeedbackTest {
    @Test fun codeOverridesWordingAndOfflineNoticesAreLimitedPerKind() = runTest {
        val input = HubFailure(HubError.Server, "超时失败", code = "input_draft", delivered = "none", retryable = true)
        val changed = HubFailure(HubError.Server, "完全不同的文字", code = "input_draft", delivered = "none", retryable = true)
        assertEquals(operationFeedback(input), operationFeedback(changed))
        assertEquals(FailureAction.OpenTerminal, operationFeedback(input).action)
        assertEquals(FailureAction.None, operationFeedback(HubFailure(HubError.Server, retryable = false)).action)
        var now = 0L
        val gate = FeedbackGate({ ConnectionState.WaitingForNetwork }, { now })
        val observed = mutableListOf<String>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { gate.messages.toList(observed) }
        val offline = HubFailure(HubError.NoNetwork)
        gate.failure(offline, true)
        runCurrent()
        gate.failure(input, true)
        runCurrent()
        gate.failure(offline, true)
        gate.failure(changed, true)
        runCurrent()
        assertEquals(2, observed.size)
        now = 4_000
        gate.failure(offline, true)
        runCurrent()
        assertEquals(3, observed.size)
        gate.failure(CancellationException(), true)
        gate.failure(input, false)
        runCurrent()
        assertEquals(3, observed.size)
    }
}
