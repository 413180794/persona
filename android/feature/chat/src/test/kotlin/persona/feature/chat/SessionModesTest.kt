package persona.feature.chat

import persona.core.contracts.Session
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The old App's SessionModesTest and ModelPickerTest (81dc6928), as iOS names the modes. */
class SessionModesTest {
    @Test fun runningSessionsCannotPickBypassButShowItWhenOn() {
        assertEquals(listOf("default", "acceptEdits", "plan", "auto"), SessionModes.switchable("default").map { it.id })
        assertEquals(listOf("default", "acceptEdits", "plan", "auto"), SessionModes.switchable(null).map { it.id })
        assertEquals("bypassPermissions", SessionModes.switchable("bypassPermissions").last().id)
        assertEquals("全部放行", SessionModes.switchable("bypassPermissions").last().title)
        // A newer Claude Code's mode is listed by its raw name, so the current one still shows as picked.
        assertEquals("future", SessionModes.switchable("future").last().title)
        assertEquals("手动确认", SessionModes.modeLabel("default"))
        assertEquals("很高", SessionModes.effortLabel("xhigh"))
        assertEquals("future", SessionModes.modeLabel("future"))
        assertNull(SessionModes.modeLabel(null))
    }

    @Test fun modelAliasesAsIos() {
        val alias = SessionModes::claudeModelId
        assertEquals("opus", alias("claude-opus-5-5[1m]"))
        assertEquals("sonnet", alias("us.anthropic.claude-sonnet-5-5"))
        assertEquals("haiku", alias("claude-haiku-5-5"))
        assertEquals("default", alias(null))
        assertEquals("default", alias("deepseek/v4.1"))
        assertEquals(listOf("default", "opus", "sonnet", "haiku"), SessionModes.claudeModels.map { it.id })
    }

    @Test fun theHintNamesWhatIsNotTheDefault() {
        val session = Session(sessionId = "s", permissionMode = "default", effort = "high")
        assertNull(chatModeHint(session))
        assertEquals("自动改文件，思考很高", chatModeHint(session.copy(permissionMode = "acceptEdits", effort = "xhigh")))
        assertEquals("全部放行", chatModeHint(session.copy(permissionMode = "bypassPermissions")))
        assertEquals("future", chatModeHint(session.copy(permissionMode = "future")))
    }
}
