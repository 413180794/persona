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
