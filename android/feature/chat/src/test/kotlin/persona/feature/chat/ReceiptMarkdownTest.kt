package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test
import persona.feature.chat.ReceiptMarkdown.Mark

class ReceiptMarkdownTest {
    private fun ReceiptMarkdown.Styled.marked(mark: Mark) = spans.filter { it.mark == mark }.map { text.substring(it.start, it.end) }

    @Test fun headingsListsBoldAndCodeLoseTheirMarks() {
        val s = ReceiptMarkdown.parse("## 根因\nhub 和 node 不同步\n\n**网页的改动**\n- **底部的「收起」**：改成 `Collapse`\n  - 调 `POST /api/v1/sessions/{mid}/{sid}/interrupt`\n1. 第二条")
        assertEquals("根因\nhub 和 node 不同步\n网页的改动\n• 底部的「收起」：改成 Collapse\n  • 调 POST /api/v1/sessions/{mid}/{sid}/interrupt\n1. 第二条", s.text)
        assertEquals(listOf("根因", "网页的改动", "底部的「收起」"), s.marked(Mark.Strong))
        assertEquals(listOf("Collapse", "POST /api/v1/sessions/{mid}/{sid}/interrupt"), s.marked(Mark.Code))
    }

    @Test fun linksEmphasisAndCodeBlocks() {
        val s = ReceiptMarkdown.parse("> 见 [PR](https://github.com/o/r/pull/7) 和 <https://example.com>，~~旧的~~ *新的*\n```go\nx := 1\n```\n---\n| a | b |\n| - | - |\n| 1 | 2 |")
        assertEquals("见 PR 和 https://example.com，旧的 新的\nx := 1\na · b\n1 · 2", s.text)
        assertEquals(listOf("https://github.com/o/r/pull/7", "https://example.com"), s.spans.filter { it.mark == Mark.Link }.map { it.url })
        assertEquals(listOf("旧的"), s.marked(Mark.Strike))
        assertEquals(listOf("新的"), s.marked(Mark.Emphasis))
        assertEquals(listOf("x := 1"), s.marked(Mark.Code))
    }

    @Test fun marksAClipLeftUnpairedAreDropped() {
        assertEquals("剪在 加粗中间…", ReceiptMarkdown.parse("剪在 **加粗中间…").text)
        assertEquals("剪在 代码中间…", ReceiptMarkdown.parse("剪在 `代码中间…").text)
        assertEquals("snake_case_name 和 2 * 3", ReceiptMarkdown.parse("snake_case_name 和 2 * 3").text)
    }
}
