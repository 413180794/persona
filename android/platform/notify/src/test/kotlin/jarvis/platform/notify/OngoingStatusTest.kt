package jarvis.platform.notify

import jarvis.core.contracts.FleetItem
import jarvis.core.contracts.FleetState
import jarvis.core.contracts.FleetStats
import jarvis.core.contracts.api.ConnectionState
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class OngoingStatusTest {
    @Test fun summaryIgnoresMetricsAndPromotionFallsBackWhenUnavailableOrIdle() {
        val fleet = FleetState(needsYou = 2, working = 3, unread = 1, items = listOf(
            FleetItem("machine/session", title = "示例任务", detail = "允许执行？", interaction = "request"),
        ))
        val status = OngoingStatus.from(ConnectionState.Online, fleet)
        assertEquals("需要你 2 · 工作中 3 · 完成未看 1", status.counts)
        assertEquals("示例任务：允许执行？", status.detail)
        assertEquals("💬2", status.criticalText)
        assertFalse(status.counts.contains("示例任务"))
        assertEquals(status, OngoingStatus.from(ConnectionState.Online, fleet.copy(
            stats = FleetStats(tpm = 1234), workingSince = 100.0, assistant = true,
        )))
        assertFalse(status.shouldPromote(35, true))
        assertFalse(status.shouldPromote(36, false))
        assertTrue(status.shouldPromote(36, true))
        assertFalse(status.shouldPromote(36, true, enabled = false))
        assertFalse(status.shouldPromote(37, true, enabled = false))
        assertTrue(status.shouldPromote(37, true, enabled = true))
        assertEquals(status.counts, status.lockScreenText(false))
        assertEquals("${status.counts}\n示例任务：允许执行？", status.lockScreenText(true))
        assertEquals(listOf(2, 3, 1), listOf(status.needsYou, status.working, status.unread))
        assertFalse(OngoingStatus.from(ConnectionState.Online, FleetState()).shouldPromote(36, true))
        assertEquals("⚙999+", OngoingStatus.from(ConnectionState.Online, FleetState(working = 1000)).criticalText)
        assertEquals("●1", OngoingStatus.from(ConnectionState.Online, FleetState(unread = 1)).criticalText)
        val offline = OngoingStatus.from(ConnectionState.WaitingForNetwork, fleet)
        assertEquals("等待网络", offline.title)
        assertEquals("", offline.counts)
        assertEquals("", offline.detail)
        assertEquals("", offline.lockScreenText(true))
        assertEquals(listOf(0, 0, 0), listOf(offline.needsYou, offline.working, offline.unread))
        assertFalse(offline.shouldPromote(36, true))
    }

    @Test fun continuousChangesAreConflatedAndFinalValueIsDeliveredWithoutDuplicates() = runTest {
        val source = MutableStateFlow(0)
        val delivered = mutableListOf<Int>()
        val job = backgroundScope.launch { source.throttleOngoing(100).collect { delivered += it } }
        runCurrent()
        assertEquals(listOf(0), delivered)
        repeat(9) {
            advanceTimeBy(10)
            source.value = it + 1
            runCurrent()
        }
        assertEquals(listOf(0), delivered)
        advanceTimeBy(10)
        runCurrent()
        assertEquals(listOf(0, 9), delivered)
        source.value = 10
        runCurrent()
        advanceTimeBy(100)
        runCurrent()
        assertEquals(listOf(0, 9, 10), delivered)
        source.value = 11
        runCurrent()
        source.value = 10
        runCurrent()
        advanceTimeBy(100)
        runCurrent()
        assertEquals(listOf(0, 9, 10), delivered)
        source.value = 12
        runCurrent()
        assertEquals(listOf(0, 9, 10, 12), delivered)
        job.cancel()
        source.value = 13
        advanceTimeBy(100)
        runCurrent()
        assertEquals(listOf(0, 9, 10, 12), delivered)
    }
}
