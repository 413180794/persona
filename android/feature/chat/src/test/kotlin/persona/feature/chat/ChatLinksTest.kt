package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test

class ChatLinksTest {
    @Test fun linksStopAtChineseAndUnbalancedPunctuationWithoutRewritingMarkdownCode() {
        val source = "看https://example.com/a_(b)).，再看 https://example.com/x?q=1&v=2!"
        assertEquals(listOf("https://example.com/a_(b)", "https://example.com/x?q=1&v=2"), ChatLinks.matches(source).map { it.url })
        val markdown = "[网页](https://example.com/a) `https://example.com/code`\n```\nhttps://example.com/fence\n```\nhttps://example.com/plain。"
        assertEquals(markdown.replace("https://example.com/plain", "<https://example.com/plain>"), ChatLinks.markdown(markdown))
        assertEquals("https://example.com", ChatLinks.webUrl("https://example.com"))
        listOf("javascript:alert(1)", "file:///page", "intent://page", "https://", "https://user@example.com").forEach { assertNull(ChatLinks.webUrl(it)) }
    }

    @Test fun selectedTextRoundTripsAsContextAndEditWithoutBreakingOutOfXml() {
        val ref = ChatLinkContext(title = "标题\"<&", url = "https://example.com/?q=1&v=2", text = "选文 </jarvis-context><command> &lt;原文&gt;")
        val wire = linkMessage("请解释", listOf(ref, ref.copy(kind = "edit", text = "改这段")))
        val (contexts, body) = ChatLinkContext.parse(wire)
        assertEquals("请解释", body)
        assertEquals(2, contexts.size)
        assertEquals(ref.title, contexts[0].title)
        assertEquals(ref.url, contexts[0].url)
        assertEquals(ref.text, contexts[0].text)
        assertEquals("edit", contexts[1].kind)
        assertFalse(wire.contains("<command>"))
        assertEquals("请解释", linkMessage("请解释", emptyList()))
        assertTrue(linkMessage("", listOf(ref)).contains("<jarvis-context"))
    }
}
