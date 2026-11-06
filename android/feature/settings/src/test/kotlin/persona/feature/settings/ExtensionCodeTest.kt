package persona.feature.settings

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import persona.core.designsystem.Tone
import java.io.File

// UI version 4's code hosting components (#2951): what the app reads from the component showcase pages, the logic they are drawn
// with, and what pressing them sends.
class ExtensionCodeTest {
    private fun page(name: String): ExtensionView =
        JsonCodec.decodeFromString(ExtensionView.serializer(), File("../../../schema/extension/examples/showcase/$name.json").readText())

    @Test fun readsTheCodePages() {
        val review = page("code-review")
        assertEquals("split", review.layout)
        assertEquals(listOf("overview", "files", "commits", "checks"), review.tabs!!.items.map { it.value })
        assertEquals("side", review.sections[1].pane)
        val diff = review.sections[2].items.first()
        assertEquals("diff", diff.type)
        assertEquals("t1", diff.threads!!.single().id)
        assertEquals(mapOf("thread" to "t1"), diff.threads!!.single().reply!!.values)
        assertTrue(review.sections.flatMap { it.items }.all(ExtensionViewText::drawable))
        val overview = page("code-overview")
        assertTrue(overview.sections.flatMap { it.items }.all(ExtensionViewText::drawable))
        assertEquals("", ExtensionViewText.fields(overview)["body"])
        assertEquals("overview", ExtensionViewText.fields(overview)["tab"])
    }

    @Test fun buildsTheTree() {
        val tree = ExtensionCodeText.tree(listOf(ExtensionTreeEntry("src/upload/retry.go"), ExtensionTreeEntry("src/upload/client.go"),
            ExtensionTreeEntry("README.md"), ExtensionTreeEntry("docs", kind = "dir")))
        assertEquals(listOf("docs", "src/upload", "README.md"), tree.map { it.name })
        assertTrue(tree[0].unloaded)
        assertEquals(listOf("client.go", "retry.go"), tree[1].children.map { it.name })
        val lines = ExtensionCodeText.lines(tree, closed = setOf("src/upload"))
        assertEquals(listOf("docs", "src/upload", "README.md"), lines.map { it.node.name })
        assertEquals("A" to Tone.Positive, ExtensionCodeText.status("added"))
    }

    @Test fun numbersDiffLinesAndPlacesThreads() {
        val h = ExtensionDiffHunk(oldStart = 20, newStart = 20, lines = listOf(" a", "-b", "+B", " c", "\\ No newline at end of file"))
        val rows = ExtensionCodeText.rows(h)
        assertEquals(listOf(DiffKind.Context, DiffKind.Del, DiffKind.Add, DiffKind.Context, DiffKind.Note), rows.map { it.kind })
        assertEquals(listOf(20, 21, null, 22, null), rows.map { it.old })
        assertEquals(listOf(20, null, 21, 22, null), rows.map { it.new })
        assertEquals(listOf("a", "b", "c") to listOf("a", "B", "c"), ExtensionCodeText.sides(h))
        val (at, rest) = ExtensionCodeText.placeThreads(listOf(ExtensionDiffThread(side = "old", line = 21), ExtensionDiffThread(line = 99)), listOf(h))
        assertEquals(setOf("old:21"), at.keys)
        assertEquals(99, rest.single().line)
        assertEquals(19, ExtensionCodeText.gapBefore(listOf(h), 0))
    }

    @Test fun highlights() {
        assertEquals("go", ExtensionCodeText.language(null, "a/b.go"))
        assertEquals("typescript", ExtensionCodeText.language("TS", null))
        val lines = ExtensionCodeText.highlight(listOf("func f() { // hi", "  return \"x\" /* a", "b */ 42"), "go")
        assertEquals(CodeToken("func", TokenKind.Keyword), lines[0][0])
        assertEquals(TokenKind.Comment, lines[0].last().kind)
        assertTrue(lines[1].any { it.kind == TokenKind.Text && it.text == "\"x\"" })
        assertEquals(CodeToken("b */", TokenKind.Comment), lines[2][0])
        assertTrue(lines[2].any { it.kind == TokenKind.Number && it.text == "42" })
        assertEquals(listOf(listOf(CodeToken("x = 1", TokenKind.Plain))), ExtensionCodeText.highlight(listOf("x = 1"), ""))
    }

    @Test fun sendsTheActionsValues() {
        val a = ExtensionViewAction("open", values = mapOf("path" to "fixed"))
        assertEquals(mapOf("path" to "fixed"), ExtensionViewText.sent(a, mapOf("q" to "x"), mapOf("path" to "typed")))
        assertEquals(mapOf("q" to "x", "body" to "hi"), ExtensionViewText.sent(ExtensionViewAction("c", submit = true), mapOf("q" to "x"), mapOf("body" to "hi")))
        assertNull(ExtensionViewText.sent(ExtensionViewAction("plain"), mapOf("q" to "x"), null))
    }
}
