package persona.core.domain

import persona.core.contracts.Item
import persona.core.contracts.JsonCodec
import org.junit.Assert.assertEquals
import org.junit.Test

class RowsTest {
    private fun item(id: String, seq: Int, kind: String, role: String = "assistant", text: String? = null, tool: String? = null, use: String? = null) =
        Item(id = id, seq = seq, kind = kind, role = role, text = text, toolName = tool, toolUseId = use)

    @Test fun toolUseAndResultMergeIntoOneRow() {
        val rows = buildRows(photo = "[图片]", items = listOf(
            item("a#0", 1, "text", "user", "hi"),
            item("b#0", 2, "tool_use", tool = "Bash", use = "t1"),
            item("c#0", 3, "tool_result", "user", "ok", use = "t1"),
            item("d#0", 4, "text", text = "done"),
        ))
        assertEquals(3, rows.size)
        val tool = rows[1] as Row.Tool
        assertEquals("ok", (tool.result?.text).zhAny())
    }

    @Test fun toolSubjectPicksTheUsefulField() {
        val use = JsonCodec.decodeFromString<Item>(
            """{"id":"x","kind":"tool_use","tool_name":"Read","tool_input":{"file_path":"/a/b/c.kt"}}""",
        )
        assertEquals("c.kt", (toolSubject(use)).zhAny())
    }

    @Test fun tagsAreStripped() {
        assertEquals("[图片]\nlook", (cleanText("<attached_images>\n/tmp/a.png\n</attached_images>\nlook", "[图片]")).zhAny())
        assertEquals("abc", (cleanText("<pasted_content id=\"1\">abc</pasted_content>", "[图片]")).zhAny())
    }

    @Test fun personaOwnMcpServerShowsAsPersona() {
        assertEquals("Persona · agent read", toolDisplayName("mcp__jarvis__agent_read"))
        assertEquals("Persona · agent send", toolDisplayName("mcp__plugin_jarvis__agent_send"))
        assertEquals("Persona", toolDisplayName("mcp__jarvis"))
        assertEquals("github · create issue", toolDisplayName("mcp__github__create_issue"))
        assertEquals("jarvis_extra · x", toolDisplayName("mcp__jarvis_extra__x"))
        assertEquals("Bash", toolDisplayName("Bash"))
        assertEquals(null, toolDisplayName(null))
        assertEquals("Persona", mcpServerLabel("jarvis"))
        assertEquals("example-service", mcpServerLabel("example-service"))
    }
}
