package persona.core.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MessageContextsTest {
    @Test fun attributesInAnyOrderOrMissing() {
        val (contexts, rest) = parseContexts(
            "<jarvis-context url=\"https://example.com/a\" title=\"页面\" kind=\"edit\">\n选中的字\n</jarvis-context>\n\n改一下",
        )
        assertEquals(listOf(MessageContext("edit", "页面", "https://example.com/a", "选中的字")), contexts)
        assertEquals("改一下", rest)

        val bare = parseContexts("<jarvis-context title='只有标题'>正文</jarvis-context>看看").first.single()
        assertEquals("context", bare.kind)
        assertEquals("只有标题", bare.title)
        assertNull(bare.url)
        assertNull(parseContexts("<jarvis-context kind=\"context\" title=\"t\" url=\"\">x</jarvis-context>").first.single().url)
    }

    @Test fun escapedAttributesAndBodyAreDecoded() {
        val context = parseContexts(
            "<jarvis-context kind=\"context\" title=\"A &quot;b&quot; &lt;c&gt; &amp;amp;\" url=\"https://example.com/?a=1&amp;b=2\">" +
                "&lt;b&gt; &amp; c</jarvis-context>",
        ).first.single()
        assertEquals("A \"b\" <c> &amp;", context.title)
        assertEquals("https://example.com/?a=1&b=2", context.url)
        assertEquals("<b> & c", context.body)
    }

    @Test fun emptyBodyAndLabels() {
        val (contexts, rest) = parseContexts("<jarvis-context kind=\"context\" title=\"项目 · 发布\">\n</jarvis-context>")
        assertEquals("", contexts.single().body)
        assertEquals("", rest)
        assertEquals("引用", MessageContext("context", "", null, "").label)
        assertEquals("https://example.com", MessageContext("context", " ", "https://example.com", "").label)
    }

    @Test fun plainTextHasNoContexts() {
        assertEquals(emptyList<MessageContext>() to "就一句话", parseContexts("  就一句话\n"))
        // An unclosed tag (a message still streaming) is left as it is.
        assertTrue(parseContexts("<jarvis-context kind=\"context\">没写完").first.isEmpty())
    }

    @Test fun summaries() {
        val one = MessageContext("context", "文档 · 手册", null, "")
        assertEquals("", contextSummary(emptyList()))
        assertEquals("上下文 · 文档 · 手册", contextSummary(listOf(one)))
        assertEquals("要改的内容 · 文档 · 手册", contextSummary(listOf(one.copy(kind = "edit"))))
        assertEquals("上下文 · 2 项", contextSummary(listOf(one, one.copy(kind = "edit"))))
    }

    @Test fun feishuAgentRequest() {
        val text = """
            <jarvis-context kind="instructions" title="给 Agent 的说明">
            你在飞书里被 /agent 叫到。
            ## 怎么做
            先读上下文，再回答。
            </jarvis-context>
            <jarvis-context kind="context" title="请求的位置">
            位置：群聊「发布群」
            </jarvis-context>
            <jarvis-context kind="context" title="前面的聊天">
            10-31 09:00 张三：构建挂了
            10-31 09:01 李四：我看看
            …（已截断，共 20480 字节）
            </jarvis-context>
            <jarvis-context kind="doc" title="文档 · 手册" url="https://example.com/d">
            手册正文
            </jarvis-context>
            <jarvis-context kind="context" title="项目 · 发布">
            </jarvis-context>
            看下构建为什么挂了
        """.trimIndent()
        val (contexts, rest) = parseContexts(text)
        assertEquals("看下构建为什么挂了", rest)
        assertEquals(listOf("instructions", "context", "context", "doc", "context"), contexts.map { it.kind })
        assertEquals(listOf("给 Agent 的说明", "请求的位置", "前面的聊天", "文档 · 手册", "项目 · 发布"), contexts.map { it.title })
        assertEquals("位置：群聊「发布群」", contexts[1].body)
        assertTrue(contexts[2].body.endsWith("…（已截断，共 20480 字节）"))
        assertEquals("https://example.com/d", contexts[3].url)
        assertEquals("", contexts[4].body)
        assertEquals("上下文 · 5 项", contextSummary(contexts))
        assertEquals("看下构建为什么挂了", cleanText(text))
    }
}
