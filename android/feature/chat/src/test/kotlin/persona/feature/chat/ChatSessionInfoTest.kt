package persona.feature.chat

import persona.core.contracts.Session
import persona.core.contracts.SessionCapabilities
import persona.core.contracts.SessionInfo
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.ZoneId

/** The old App's SessionInfoTest (81dc6928): tokens, rows and what can be copied. */
class ChatSessionInfoTest {
    private val zone = ZoneId.of("Asia/Shanghai")
    private fun rows(session: Session, info: SessionInfo) = sessionInfoSections(session, info, zone).associate { section ->
        section.title to section.rows.associate { it.label to it.value }
    }

    @Test fun tokensReadAsIos() {
        assertEquals("950", formatTokens(950))
        assertEquals("12.3k", formatTokens(12_345))
        assertEquals("1.20M", formatTokens(1_200_000))
    }

    @Test fun claudesSessionShowsWhatItRunsHowFarAndWhere() {
        val session = Session(machineId = "m", sessionId = "s1", cwd = "/home/me/code/shop", launcher = "claude", model = "opus",
            permissionMode = "acceptEdits", effort = "xhigh")
        val info = SessionInfo(sessionId = "s1", cwd = "/home/me/code/shop", model = "claude-opus-5", version = "2.1.320",
            contextTokens = 48_200, outputTokens = 12_800, turns = 6, compactions = 0, gitBranch = "main",
            startedAt = "2026-11-01T02:00:00Z")
        val all = rows(session, info)
        assertEquals("claude-opus-5", all.getValue("运行")["模型"])
        assertEquals("2.1.320", all.getValue("运行")["Claude Code"])
        assertEquals("自动改文件", all.getValue("运行")["权限"])
        assertEquals("很高", all.getValue("运行")["思考强度"])
        assertEquals("48.2k tokens", all.getValue("用量")["上下文"])
        assertFalse("压缩" in all.getValue("用量"))
        assertEquals("6 轮", all.getValue("用量")["对话"])
        assertEquals("11月1日 10:00", all.getValue("用量")["开始"])
        assertEquals("main", all.getValue("位置")["分支"])
        assertEquals("cd -- '/home/me/code/shop' && claude --resume 's1'", all.getValue("拷贝")["恢复命令"])
        assertEquals("s1", all.getValue("拷贝")["session id"])
        // Unknown counts and fields read as nothing rather than inventing them.
        val sparse = rows(session.copy(launcher = null), SessionInfo(sessionId = "s1", compactions = 2))
        assertEquals("2 次", sparse.getValue("用量")["压缩"])
        assertNull(sparse.getValue("运行")["启动器"])
        assertFalse("开始" in sparse.getValue("用量"))
    }

    @Test fun aCodexThreadResumesByItsThreadAndARuntimeNotAtAll() {
        val codex = Session(machineId = "m", sessionId = "c0de-opaque", cwd = "/srv/deploy", agent = "codex",
            sourceKind = "codex_app_server", sourceThreadId = "019a-thread",
            capabilities = SessionCapabilities(read = true, send = true, terminal = true))
        val all = rows(codex, SessionInfo(sessionId = "c0de-opaque", version = "0.153.0"))
        assertEquals("0.153.0", all.getValue("运行")["Codex"])
        assertEquals("cd -- '/srv/deploy' && codex resume '019a-thread'", all.getValue("拷贝")["恢复命令"])
        val runtime = Session(machineId = "m", sessionId = "session-1", agent = "dsh", agentLabelHint = "DSH", sourceKind = "runtime",
            capabilities = SessionCapabilities(read = true, send = true))
        val copied = rows(runtime, SessionInfo(sessionId = "session-1")).getValue("拷贝")
        assertFalse("恢复命令" in copied)
        assertTrue("session id" in copied)
    }

    @Test fun readOnlySourcesAndBlockedApprovalsSayWhy() {
        val history = Session(sessionId = "h", sourceKind = "codex_history", readOnlyReason = "仅查看：外部进程正在使用此会话",
            capabilities = SessionCapabilities(read = true))
        assertEquals("仅查看：外部进程正在使用此会话", readOnlyNote(history))
        assertEquals("此会话仅可查看", readOnlyNote(history.copy(sourceKind = "codex_history", readOnlyReason = null,
            capabilities = SessionCapabilities(read = true, send = true))))
        val live = history.copy(sourceKind = "codex_app_server", capabilities = SessionCapabilities(read = true, send = true, attach = true))
        assertTrue("实时接入" in approvalBlockedReason(live))
        assertFalse("实时接入" in approvalBlockedReason(history))
    }
}
