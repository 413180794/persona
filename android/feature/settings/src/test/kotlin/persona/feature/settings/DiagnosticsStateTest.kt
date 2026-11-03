package persona.feature.settings

import persona.core.contracts.ServerEvent
import persona.core.contracts.api.*
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import org.junit.Test

class DiagnosticsStateTest {
    @Test fun observesLiveChangesAndReprobesTheRunningConnection() = runBlocking {
        val connection = LiveConnection()
        val pairing = object : SettingsDiagnostics {
            override suspend fun read(): ConnectionDiagnostics = error("Pairing snapshots must not be read")
            override suspend fun reprobe(): ConnectionDiagnostics = error("Pairing must not be probed")
            override suspend fun scheduleLockScreenTest(delaySeconds: Int) = Unit
        }
        val controller = DiagnosticsState(connection, connection, pairing)
        connection.diagnostics.value = ConnectionDiagnostics(currentPath = "域名 · https://example.com")
        controller.refresh()
        assertEquals(connection.diagnostics.value, controller.data.value)
        connection.diagnostics.value = connection.diagnostics.value.copy(
            lastReceivedAt = "2026-10-31T00:00:00Z", heartbeatSeconds = 20,
        )
        assertEquals("2026-10-31T00:00:00Z", controller.data.value.lastReceivedAt)
        assertEquals(20, controller.data.value.heartbeatSeconds)
        controller.refresh(reprobe = true)
        assertEquals(1, connection.reconnections)
        assertNull(controller.data.value.currentPath)
        connection.diagnostics.value = ConnectionDiagnostics(lastDisconnectReason = "连接中断")
        assertEquals("连接中断", controller.data.value.lastDisconnectReason)
        connection.failHistory = true
        controller.refresh()
        assertEquals("连接中断", controller.data.value.lastDisconnectReason)
        assertNotNull(controller.message.value)
        assertFalse(controller.busy.value)
    }

    @Test fun missingSourceAndUnmeasuredSamplesStayUnknown() = runBlocking {
        val connection = LiveConnection()
        connection.diagnostics.value = ConnectionDiagnostics(currentPath = "旧快照")
        val controller = DiagnosticsState(connection, null, null)
        controller.refresh()
        assertNull(controller.data.value.currentPath)
        assertNotNull(controller.message.value)
        controller.refresh(reprobe = true)
        assertEquals(1, connection.reconnections)
        val candidate = ConnectionCandidate("https://example.com", "name", "未实测")
        assertEquals("未实测", candidate.probeMeasurement())
        assertEquals("未实测", candidate.copy(probeDurationMillis = 12).probeMeasurement())
        assertEquals("未实测", candidate.copy(probeDurationMillis = -1, sampleCount = 1).probeMeasurement())
        assertEquals("12 毫秒（样本数 1），不作为性能基线", candidate.copy(probeDurationMillis = 12, sampleCount = 1).probeMeasurement())
    }

    private class LiveConnection : HubConnection, ConnectionDiagnosticsSource {
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override val diagnostics = MutableStateFlow(ConnectionDiagnostics())
        var reconnections = 0
        var failHistory = false
        override fun reconnect() { reconnections++; diagnostics.value = ConnectionDiagnostics() }
        override fun send(message: JsonObject) = false
        override suspend fun readDiagnostics(): ConnectionDiagnostics {
            if (failHistory) error("History unavailable")
            return diagnostics.value
        }
    }
}

class DiagnosticsTimeTest {
    @Test fun wireTimesShowInLocalTimeWithTheDateOnlyBeforeToday() {
        val zone = java.time.ZoneId.of("Asia/Shanghai")
        val today = java.time.LocalDate.of(2026, 10, 31)
        assertEquals("06:03:57", localTime("2026-10-30T22:03:57.566254Z", zone, today))
        assertEquals("10-30 08:00:00", localTime("2026-10-30T08:00:00+08:00", zone, today))
        assertEquals("时间未提供", localTime("时间未提供", zone, today))
        assertNull(localTime(null, zone, today))
    }
}
