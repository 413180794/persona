package persona.core.domain.sort

import persona.core.domain.zhAny

import persona.core.domain.zh
import persona.core.contracts.Session
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** One confirmation for the list's long press and swipe and the chat's menu: which agent, where, what happens. */
class AgentCloseTest {
    @Test fun itNamesTheAgentTheMachineAndTheTab() {
        val session = Session(machineId = "macbook", sessionId = "s", title = "补齐网络层的单元测试", spaceName = "weather", slotName = "tests")
        val confirmation = session.closeConfirmation("MacBook")
        assertEquals("关闭这个 Agent？", (confirmation.title).zhAny())
        assertEquals("「补齐网络层的单元测试」\nMacBook / weather › tests\n\n会退出 MacBook 上的 Claude，并关掉它在终端里的标签页「tests」" +
            "（分屏的标签页只关它这一格）。正在做的事会中断；对话记录会保留，之后可以在 Agents 的「历史」里继续。", (confirmation.message).zhAny())
    }

    @Test fun withoutATabOrAMachineNameItStillReads() {
        val codex = Session(machineId = "server", sessionId = "s", cwd = "/srv/shop", agent = "codex")
        val message = codex.closeConfirmation(null).message.zh()
        assertTrue(message.startsWith("「shop」\nserver\n\n会退出 server 上的 Codex，并关掉它所在的终端窗格。"))
    }
}
