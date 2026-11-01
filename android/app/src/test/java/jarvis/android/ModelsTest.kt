package jarvis.android

import jarvis.android.data.AgentState
import jarvis.android.data.ApiClient
import jarvis.android.data.Interaction
import jarvis.android.data.Item
import jarvis.android.data.JsonCodec
import jarvis.android.data.Resolution
import jarvis.android.data.Session
import jarvis.android.data.parseTime
import jarvis.android.ui.Block
import jarvis.android.ui.ChatRow
import jarvis.android.ui.ToolStep
import jarvis.android.ui.buildRows
import jarvis.android.ui.cleanText
import jarvis.android.ui.parseBlocks
import jarvis.android.ui.toolSubject
import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ModelsTest {
    @Test fun sessionDecodesWithMissingFieldsAndUnknownState() {
        val s = JsonCodec.decodeFromString<Session>(
            """{"machine_id":"server","session_id":"u1","cwd":"/home/x/code/foo","status":"done","state":"later","extra":1}""",
        )
        assertEquals("foo", s.displayTitle)
        // An unknown state falls back to the status, as the iOS app does for older hubs.
        assertEquals(AgentState.Unread, s.agentState)
        assertEquals(0, s.pendingInteractions)
    }

    @Test fun zeroTimeIsUnset() {
        assertNull(parseTime("0001-01-01T00:00:00Z"))
        assertTrue(parseTime("2026-10-29T15:05:32.42Z") != null)
    }

    @Test fun resolutionOmitsUnsetFields() {
        assertEquals("""{"allow":true}""", JsonCodec.encodeToString(Resolution(allow = true)))
        assertEquals("""{"approve":false,"feedback":"改"}""", JsonCodec.encodeToString(Resolution(approve = false, feedback = "改")))
        assertEquals("""{"option":"2"}""", JsonCodec.encodeToString(Resolution(option = "2")))
    }

    @Test fun interactionHeadline() {
        val ix = JsonCodec.decodeFromString<Interaction>(
            """{"id":"ix_1","kind":"permission","tool_name":"Bash","summary":"rm -rf build"}""",
        )
        assertEquals("请求使用 Bash：rm -rf build", ix.headline)
    }

    @Test fun apiUrlsKeepEncodedItemIds() {
        val api = ApiClient("http://hub.example.com:18660/", "t")
        assertEquals("ws://hub.example.com:18660/api/v1/ws", api.wsUrl())
        val u = api.url("/api/v1/sessions/server/u1/items").newBuilder().addPathSegment("e1#2").addPathSegment("image").build()
        assertEquals("/api/v1/sessions/server/u1/items/e1%232/image", u.encodedPath)
    }
}

class RowsTest {
    private fun item(id: String, seq: Int, kind: String, role: String = "assistant", text: String? = null, tool: String? = null, use: String? = null) =
        Item(id = id, seq = seq, kind = kind, role = role, text = text, toolName = tool, toolUseId = use)

    @Test fun toolUseAndResultMergeIntoOneRow() {
        val rows = buildRows(listOf(
            item("a#0", 1, "text", "user", "hi"),
            item("b#0", 2, "tool_use", tool = "Bash", use = "t1"),
            item("c#0", 3, "tool_result", "user", "ok", use = "t1"),
            item("d#0", 4, "text", text = "done"),
        ))
        assertEquals(3, rows.size)
        val tool = (rows[1] as ChatRow.Tools).steps.single() as ToolStep.Call
        assertEquals("ok", tool.call.result?.text)
    }

    @Test fun toolSubjectPicksTheUsefulField() {
        val use = JsonCodec.decodeFromString<Item>(
            """{"id":"x","kind":"tool_use","tool_name":"Read","tool_input":{"file_path":"/a/b/c.kt"}}""",
        )
        assertEquals("c.kt", toolSubject(use))
    }

    @Test fun tagsAreStripped() {
        assertEquals("[图片]\nlook", cleanText("<attached_images>\n/tmp/a.png\n</attached_images>\nlook"))
        assertEquals("abc", cleanText("<pasted_content id=\"1\">abc</pasted_content>"))
    }
}

class MarkdownTest {
    @Test fun blocks() {
        val b = parseBlocks("# T\n\npara one\nline two\n\n- a\n- b\n\n```\ncode\n```\n> q")
        assertEquals(Block.Heading(1, "T"), b[0])
        assertEquals(Block.Para("para one\nline two"), b[1])
        assertEquals(Block.Para("• a\n• b"), b[2])
        assertEquals(Block.Code("code"), b[3])
        assertTrue(b[4] is Block.Quote)
    }

    @Test fun hashWithoutSpaceIsNotAHeading() {
        assertFalse(parseBlocks("#tag")[0] is Block.Heading)
    }
}

class AsyncStartTest {
    @Test fun startingAndStartErrorDecode() {
        val s = jarvis.android.data.JsonCodec.decodeFromString<jarvis.android.data.Session>(
            """{"machine_id":"mac","session_id":"s1","status":"idle","starting":true}""")
        org.junit.Assert.assertEquals(true, s.starting)
        val f = jarvis.android.data.JsonCodec.decodeFromString<jarvis.android.data.Session>(
            """{"machine_id":"mac","session_id":"s1","status":"offline","start_error":"claude did not become ready"}""")
        org.junit.Assert.assertEquals("claude did not become ready", f.startError)
        org.junit.Assert.assertEquals(null, f.starting)
    }
}

class SessionModesTest {
    @Test fun runningSessionsCannotPickBypassButShowItWhenOn() {
        val ids = jarvis.android.ui.SessionModes.switchable("default").map { it.id }
        org.junit.Assert.assertEquals(listOf("default", "acceptEdits", "plan", "auto"), ids)
        org.junit.Assert.assertEquals("bypassPermissions", jarvis.android.ui.SessionModes.switchable("bypassPermissions").last().id)
        org.junit.Assert.assertEquals("future", jarvis.android.ui.SessionModes.switchable("future").last().title) // a newer mode shows raw
        org.junit.Assert.assertEquals("手动确认", jarvis.android.ui.SessionModes.modeLabel("default"))
        org.junit.Assert.assertEquals("很高", jarvis.android.ui.SessionModes.effortLabel("xhigh"))
    }

    @Test fun modesGoOutAsTheHubReadsThem() {
        val s = jarvis.android.data.JsonCodec.decodeFromString<jarvis.android.data.Session>(
            """{"machine_id":"m","session_id":"s","permission_mode":"plan","effort":"max"}""")
        org.junit.Assert.assertEquals("plan", s.permissionMode); org.junit.Assert.assertEquals("max", s.effort)
    }
}

class SlashCommandsTest {
    private fun words(d: String, clear: Boolean = true) = jarvis.android.ui.chat.SlashCommand.matching(d, clear).map { it.word }

    @Test fun aSlashListsThemAndMoreLettersNarrow() {
        org.junit.Assert.assertEquals(listOf("/继续", "/compact", "/context", "/提交", "/汇报", "/clear"), words("/"))
        org.junit.Assert.assertEquals(listOf("/compact", "/context", "/clear"), words("/c"))
        org.junit.Assert.assertEquals(listOf("/compact", "/context"), words("/co"))
        org.junit.Assert.assertEquals(listOf("/汇报"), words("/汇"))
    }

    @Test fun onlyWhileTheDraftIsOneSlashWord() {
        org.junit.Assert.assertEquals(emptyList<String>(), words("帮我看下"))
        org.junit.Assert.assertEquals(emptyList<String>(), words("/compact 一下"))
        org.junit.Assert.assertEquals(emptyList<String>(), words("/tmp/a\n"))
        org.junit.Assert.assertEquals(listOf("/继续", "/compact", "/context", "/提交", "/汇报"), words("/", clear = false)) // Jarvis, Codex
    }
}

class ModelPickerTest {
    @Test fun modelIdsAsIos() {
        val f = jarvis.android.ui.SessionModes::claudeModelId
        org.junit.Assert.assertEquals("opus", f("claude-opus-5-5[1m]"))
        org.junit.Assert.assertEquals("sonnet", f("us.anthropic.claude-sonnet-5-5"))
        org.junit.Assert.assertEquals("haiku", f("claude-haiku-5-5"))
        org.junit.Assert.assertEquals("default", f(null))
        org.junit.Assert.assertEquals("default", f("deepseek/v4.1"))
        org.junit.Assert.assertEquals(listOf("default", "opus", "sonnet", "haiku"), jarvis.android.ui.SessionModes.claudeModels.map { it.id })
    }
}

class SessionInfoTest {
    @Test fun resumeCommandAndTokens() {
        val claude = jarvis.android.data.Session(sessionId = "s")
        org.junit.Assert.assertEquals("cd /tmp/a && claude --resume abc", jarvis.android.ui.resumeCommand(claude, "/tmp/a", "abc"))
        org.junit.Assert.assertEquals("cd '/tmp/my dir' && codex resume abc",
            jarvis.android.ui.resumeCommand(claude.copy(agent = "codex"), "/tmp/my dir", "abc"))
        org.junit.Assert.assertEquals("950", jarvis.android.ui.tokens(950))
        org.junit.Assert.assertEquals("12.3k", jarvis.android.ui.tokens(12_345))
        org.junit.Assert.assertEquals("1.20M", jarvis.android.ui.tokens(1_200_000))
    }

    @Test fun infoDecodes() {
        val i = jarvis.android.data.JsonCodec.decodeFromString<jarvis.android.data.ApiClient.SessionInfo>(
            """{"session_id":"s1","cwd":"/x","model":"claude-opus","context_tokens":51234,"output_tokens":9,"turns":3,"compactions":1,"git_branch":"main","started_at":"2026-11-01T02:00:00Z"}""")
        org.junit.Assert.assertEquals(51234L, i.contextTokens); org.junit.Assert.assertEquals("main", i.gitBranch); org.junit.Assert.assertEquals(3, i.turns)
    }
}
