package persona.feature.chat

import persona.core.contracts.Item
import persona.core.contracts.JsonCodec
import persona.core.domain.lines.ChatLine
import org.junit.Assert.*
import org.junit.Test

class ContentPresentationTest {
    @Test fun rendersEditWriteMultiEditAndTodoInputsWithoutRawJson() {
        fun tool(name: String, input: String) = Item("call", kind = "tool_use", toolName = name,
            toolInput = JsonCodec.parseToJsonElement(input))
        val diff = toolDiff(tool("Edit", """{"old_string":"same\nold\ntail\n","new_string":"same\nnew\ntail\n"}"""))!!
        assertEquals(listOf(' ', '-', '+', ' '), diff.lines.map { it.sign })
        assertEquals(1, diff.added)
        assertEquals(1, diff.removed)
        assertTrue(diff.lines.all { it.old == null && it.new == null })
        val write = toolDiff(tool("Write", """{"content":"first\nsecond\n"}"""))!!
        assertEquals(2, write.added)
        assertEquals(listOf(1, 2), write.lines.map { it.new })
        assertTrue(toolDiff(tool("Write", """{"content":""}"""))!!.lines.isEmpty())
        assertEquals(2, toolDiff(tool("MultiEdit", """{"edits":[{"old_string":"a","new_string":"b"},{"old_string":"","new_string":"c"}]}"""))!!.added)
        assertNull(toolDiff(tool("Edit", """{"old_string":"a"}""")))
        val todos = todoEntries(tool("TodoWrite", """{"todos":[{"content":"检查改动","status":"completed"},{"content":"编译","status":"in_progress","activeForm":"正在编译"},{"content":"交付","status":"pending"},null]}"""))!!
        assertEquals(listOf("completed", "in_progress", "pending"), todos.map { it.status })
        assertEquals("正在编译", todos[1].activeForm)
        assertNull(todoEntries(tool("Read", "{}")))
    }

    @Test fun preservesReferenceMetadataAndMergesQueuedUsersIntoOneLine() {
        val text = """<jarvis-context kind="edit" title="页面 &amp; 说明" url="https://example.com/document?a=1&amp;b=2">待修改内容</jarvis-context>请修改<pasted_content id="sample">这段</pasted_content>"""
        val model = messageModel(text)
        val reference = model.contexts.single()
        assertEquals("页面 & 说明", reference.title)
        assertEquals("edit", reference.kind)
        assertEquals("https://example.com/document?a=1&b=2", reference.url)
        assertEquals("待修改内容", reference.body)
        assertEquals(listOf<MessagePart>(MessagePart.Text("请修改这段")), model.parts)
        val source = messageParts("""<cross-session-message from-name="其他会话">内容</cross-session-message>""").single() as MessagePart.Source
        assertEquals("其他会话", source.name)
        assertEquals("内容", source.text)
        val items = listOf(
            Item("first", 1, role = "user", kind = "text", text = text),
            Item("second", 2, role = "user", kind = "text", text = "再补充", queued = true),
            Item("answer", 3, role = "assistant", kind = "text", text = "收到"),
        )
        val projection = ChatTimeline().lines(items, null, emptySet())
        val merged = projection.lines.filterIsInstance<ChatLine.MergedUsers>().single()
        assertEquals(2, merged.users.size)
        assertFalse(projection.lines.filterIsInstance<ChatLine.Row>().any { it.row.type == "user" })
        assertTrue(ChatRenderStyle.Assistant.collapseThoughts)
        assertTrue(ChatRenderStyle.Assistant.compactToolSummary)
    }
}
