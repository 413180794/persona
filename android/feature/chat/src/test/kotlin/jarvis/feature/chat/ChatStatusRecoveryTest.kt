package jarvis.feature.chat

import jarvis.core.contracts.Session
import jarvis.core.contracts.api.*
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

class ChatStatusRecoveryTest {
    @Test fun recoveryGuardsConcurrentActionsAndKeepsAcknowledgementWhenRefreshFails() = runTest {
        val session = Session(machineId = "machine", sessionId = "session", status = "offline")
        var resumes = 0
        var reconnects = 0
        val gate = CompletableDeferred<Unit>()
        val service = object : ChatRecoveryService {
            override suspend fun resume(session: Session): ChatRecoveryResult {
                resumes++
                if (resumes == 1) { gate.await(); return ChatRecoveryResult.Unknown }
                return ChatRecoveryResult.Restored(session.key)
            }
            override suspend fun reconnect(machineId: String): ChatActionResult {
                assertEquals(session.machineId, machineId)
                reconnects++
                return ChatActionResult.Done
            }
        }
        val state = ChatStatusRecovery(service, backgroundScope) { error("Refresh unavailable") }
        state.resume(session)
        state.resume(session)
        state.reconnect(session.machineId)
        runCurrent()
        assertEquals(1, resumes)
        assertEquals(0, reconnects)
        assertTrue(state.busy)
        gate.complete(Unit)
        runCurrent()
        assertTrue(state.resumeUnknown)
        assertFalse(state.busy)
        state.resume(session)
        runCurrent()
        assertEquals(session.key, state.restored)
        assertFalse(state.resumeUnknown)
        assertTrue(state.notice!!.contains("列表暂未更新"))
        state.resume(session)
        runCurrent()
        assertEquals(2, resumes)
        state.reconnect(session.machineId)
        runCurrent()
        assertTrue(state.reconnectRequested)
        state.reconnect(session.machineId)
        runCurrent()
        assertEquals(1, reconnects)
    }

    @Test fun statusSheetDescribesLoopsAndBackgroundTasksInWords() {
        val zone = java.time.ZoneId.of("Asia/Shanghai")
        assertEquals("循环 · 每 30 分钟 · 下次 14:05",
            loopSummary(jarvis.core.contracts.LoopInfo(intervalSeconds = 1800, nextAt = "2026-10-31T06:05:00Z"), zone))
        assertEquals("循环 · 按计划 0 9 * * *", loopSummary(jarvis.core.contracts.LoopInfo(cron = "0 9 * * *"), zone))
        assertEquals("跑小组件快照测试", backgroundTitle(jarvis.core.contracts.BackgroundTask("b1", "shell", "跑小组件快照测试", "xcodebuild test")))
        assertEquals("xcodebuild test", backgroundTitle(jarvis.core.contracts.BackgroundTask("b2", "shell", null, "\nxcodebuild test\n-quiet")))
    }
}
