package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Session
import persona.core.contracts.api.*
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ChatStatusRecoveryTest {
    private val res get() = ApplicationProvider.getApplicationContext<Context>().resources
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
        val state = ChatStatusRecovery(res, service, backgroundScope) { error("Refresh unavailable") }
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
        // Compare against the resource so wording edits don't break the test; the
        // acknowledgement must survive the failed refresh.
        val restoredNotice = res.getString(R.string.chat_recovery_restored)
        assertEquals(res.getString(R.string.chat_recovery_list_stale, restoredNotice), state.notice)
        assertTrue(state.notice!!.contains(restoredNotice))
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
            loopSummary(res, persona.core.contracts.LoopInfo(intervalSeconds = 1800, nextAt = "2026-10-31T06:05:00Z"), zone))
        assertEquals("循环 · 按计划 0 9 * * *", loopSummary(res, persona.core.contracts.LoopInfo(cron = "0 9 * * *"), zone))
        assertEquals("跑小组件快照测试", backgroundTitle(persona.core.contracts.BackgroundTask("b1", "shell", "跑小组件快照测试", "xcodebuild test"), "后台任务"))
        assertEquals("xcodebuild test", backgroundTitle(persona.core.contracts.BackgroundTask("b2", "shell", null, "\nxcodebuild test\n-quiet"), "后台任务"))
    }
}
