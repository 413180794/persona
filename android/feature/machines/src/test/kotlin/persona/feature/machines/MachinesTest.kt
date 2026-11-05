package persona.feature.machines

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class MachinesTest {
    @Test fun bytesPickTheUnitAfterRounding() {
        val en = java.util.Locale.ENGLISH
        assertEquals("1,023.0 B", MetricFormat.bytes(1023, en))
        assertEquals("1.0 KB", MetricFormat.bytes(1023.96, en))
        assertEquals("1.0 KB", MetricFormat.bytes(1024, en))
        assertEquals("1,023.9 KB", MetricFormat.bytes(1023.94 * 1024, en))
        assertEquals("1.0 MB", MetricFormat.bytes(1024.0 * 1024 - 20, en))
        assertEquals("1.0 GB/s", MetricFormat.rate(1023.97 * 1024 * 1024, en))
        assertEquals("16.0 GB", MetricFormat.bytes(16L shl 30, en))
        assertEquals("2,048.0 TB", MetricFormat.bytes(2048.0 * (1L shl 40), en))
    }

    @Test fun historyPreservesGapsAndPollingStopsWithThePage() = runTest {
        val points = listOf(MetricPoint(100, cpu = 20.0), MetricPoint(110, cpu = 30.0),
            MetricPoint(120, gap = true), MetricPoint(200, cpu = 95.0), MetricPoint(210), MetricPoint(220, cpu = 0.0))
        assertEquals(listOf(listOf(100L, 110L), listOf(200L), listOf(220L)), segments(points, 10) { it.cpu }.map { run -> run.map { it.t } })
        assertNull(selectedPoint(points, 180, 10))
        assertNull(selectedPoint(points, 120, 10))
        assertEquals(200L, selectedPoint(points, 200, 10)?.t)
        val sample = MetricSample(200, cpu = 95.0, memUsed = 80, memTotal = 100, load = 8.0)
        val metric = MachineMetrics("machine", true, latest = sample, points = points)
        assertNull(metric.current(231))
        assertNull(metric.copy(online = false).current(200))
        assertEquals(3, alerts(sample, 4).size)
        assertEquals(80.0, percent(sample.memUsed, sample.memTotal)!!, 0.0)
        assertNull(percent(1, 0))
        assertEquals(listOf("agent"), machineSessions(listOf(
            Session("machine", "assistant", assistant = true), Session("machine", "agent"),
            Session("other", "foreign"), Session("machine", "ended", status = "offline")), "machine").map { it.sessionId })

        val calls = mutableListOf<MetricRange>()
        val api = object : MachineMetricsApi {
            override suspend fun dashboard() = MetricsDashboard(listOf(metric))
            override suspend fun history(machineId: String, range: MetricRange): MetricsHistory {
                calls += range
                return MetricsHistory(metric, range.wire, range.step)
            }
        }
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow(emptyList<Machine>())
            override val sessions = MutableStateFlow(emptyList<Session>())
            override suspend fun refresh() = Unit
        }
        val model = MachinesViewModel(api, sessions, "machine") { 200 }
        val job = backgroundScope.launch { model.poll() }
        runCurrent()
        assertEquals(listOf(MetricRange.Hour), calls)
        model.selectRange(MetricRange.Day)
        runCurrent()
        assertEquals(MetricRange.Day, calls.last())
        assertEquals("24h", model.state.value.history?.range)
        advanceTimeBy(MetricRange.Day.pollMillis)
        runCurrent()
        assertEquals(3, calls.size)
        job.cancelAndJoin()
        advanceTimeBy(MetricRange.Day.pollMillis * 2)
        runCurrent()
        assertEquals(3, calls.size)
    }
}
