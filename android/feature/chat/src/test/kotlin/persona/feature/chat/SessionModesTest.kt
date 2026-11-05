package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Session
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The old App's SessionModesTest and ModelPickerTest (81dc6928), as iOS names the modes. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class SessionModesTest {
    private val res = ApplicationProvider.getApplicationContext<Context>().resources
    @Test fun runningSessionsCannotPickBypassButShowItWhenOn() {
        assertEquals(listOf("default", "acceptEdits", "plan", "auto"), SessionModes.switchable("default").map { it.id })
        assertEquals(listOf("default", "acceptEdits", "plan", "auto"), SessionModes.switchable(null).map { it.id })
        assertEquals("bypassPermissions", SessionModes.switchable("bypassPermissions").last().id)
        assertEquals("全部放行", SessionModes.switchable("bypassPermissions").last().title(res))
        // A newer Claude Code's mode is listed by its raw name, so the current one still shows as picked.
        assertEquals("future", SessionModes.switchable("future").last().title(res))
        assertEquals("手动确认", SessionModes.modeLabel("default", res))
        assertEquals("很高", SessionModes.effortLabel("xhigh", res))
        assertEquals("future", SessionModes.modeLabel("future", res))
        assertNull(SessionModes.modeLabel(null, res))
    }

    @Test fun aCodexThreadOffersItsOwnLevels() {
        val codex = Session(sessionId = "s", agent = "codex", effort = "high", efforts = listOf("low", "high", "ultra", "turbo"))
        assertEquals(listOf("low", "high", "ultra", "turbo"), SessionModes.effortOptions(codex).map { it.id })
        assertEquals(listOf("低", "高", "极高", "turbo"), SessionModes.effortOptions(codex).map { it.title(res) })
        assertEquals("极低", SessionModes.effortLabel("minimal", res))
        // Claude Code's sessions keep Claude's levels.
        assertEquals(SessionModes.effortLevels, SessionModes.effortOptions(Session(sessionId = "s")))
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

    @Test fun theGptHintKeepsTheModesAndEveryVersionVariant() {
        val session = Session(sessionId = "s", model = "gpt-6.1-sol", permissionMode = "default", effort = "high")
        assertEquals("6.1 Sol", chatModeHint(session, res))
        assertEquals("6.1", chatModeHint(session.copy(model = "gpt-6.1"), res))
        assertEquals("4o Mini", chatModeHint(session.copy(model = "gpt-4o-mini"), res))
        assertEquals("6.1 Codex Max", chatModeHint(session.copy(model = "gpt-6.1-codex-max"), res))
        assertEquals("自动改文件，思考很高，6.1 Sol", chatModeHint(session.copy(permissionMode = "acceptEdits", effort = "xhigh"), res))
        // The current model does not grant a session permission or effort controls.
        assertEquals("6.1 Sol", chatModeHint(session.copy(permissionMode = "acceptEdits", effort = "xhigh"), res, includeModes = false))
        assertEquals("gpt-6.1-sol", session.model)
    }

    @Test fun nonGptAndUnknownModelsDoNotChangeTheExistingHint() {
        val session = Session(sessionId = "s", permissionMode = "acceptEdits", effort = "xhigh")
        listOf(null, "", " \t", "unknown", "claude-opus-5-5", "deepseek/v4.1", "gateway/gpt-6.1", "gpt-", "gpt-6.1--sol").forEach { model ->
            assertEquals(model, "自动改文件，思考很高", chatModeHint(session.copy(model = model), res))
            assertNull(chatModeHint(session.copy(model = model), res, includeModes = false))
            assertNull(chatModeHint(session.copy(model = model, permissionMode = "default", effort = "high"), res))
        }
        assertNull(chatModeHint(null, res))
    }

    @Test fun theHintNamesWhatIsNotTheDefault() {
        val session = Session(sessionId = "s", permissionMode = "default", effort = "high")
        assertNull(chatModeHint(session, res))
        assertEquals("自动改文件，思考很高", chatModeHint(session.copy(permissionMode = "acceptEdits", effort = "xhigh"), res))
        assertEquals("全部放行", chatModeHint(session.copy(permissionMode = "bypassPermissions"), res))
        assertEquals("future", chatModeHint(session.copy(permissionMode = "future"), res))
    }
}
