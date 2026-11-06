package persona.feature.chat

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.SessionPathStat
import persona.core.contracts.api.SessionPathStats
import persona.core.domain.PathRefs

@OptIn(ExperimentalCoroutinesApi::class)
class PathLinksTest {
    private val there = mapOf("/tmp/persona-tasks/1664-lark-test.md" to "file", "src/a.ts" to "file", "~/notes/" to "dir", "/Users/me/Desktop/x.md" to "denied")

    @Test fun rewritesOnlyThePathsThereAndLeavesCodeBlocksAndLinksAlone() {
        val md = "改了 `src/a.ts:12`，见 src/a.ts:40 和 ~/notes/ 和 /tmp/other/x.md。\n\n```\ncat src/a.ts\n```\n\n" +
            "[链接](https://example.com/src/a.ts) <https://example.com/src/a.ts>"
        val out = PathLinks.markdown(md, setOf("src/a.ts", "~/notes/"))
        assertEquals(
            "改了 [`src/a.ts:12`](persona-path:src%2Fa.ts#L12)，见 [src/a.ts:40](persona-path:src%2Fa.ts#L40) 和 [~/notes/](persona-path:%7E%2Fnotes%2F) 和 /tmp/other/x.md。\n\n" +
                "```\ncat src/a.ts\n```\n\n[链接](https://example.com/src/a.ts) <https://example.com/src/a.ts>",
            out,
        )
        assertEquals(md, PathLinks.markdown(md, emptySet()))
    }

    @Test fun escapesMarkdownInALinksTextAndKeepsUnclosedFencesAndIndentedCode() {
        assertEquals("见 [src/my\\_file.py](persona-path:src%2Fmy_file.py)", PathLinks.markdown("见 src/my_file.py", setOf("src/my_file.py")))
        assertEquals("```sh\ncat src/a.ts", PathLinks.markdown("```sh\ncat src/a.ts", setOf("src/a.ts"))) // still streaming
        assertEquals("说明\n\n    cat src/a.ts\n", PathLinks.markdown("说明\n\n    cat src/a.ts\n", setOf("src/a.ts")))
    }

    @Test fun aPathLinkCarriesThePathBackAndOtherLinksGoOn() {
        val path = "~/报告/第 4 季度.docx"
        assertEquals(path, PathLinks.pathOf(PathLinks.uri(path)))
        assertNull(PathLinks.pathOf("https://example.com"))
        val opened = mutableListOf<Pair<String, Int?>>()
        val outer = mutableListOf<String>()
        val handler = PathLinks.handler(object : androidx.compose.ui.platform.UriHandler { override fun openUri(uri: String) { outer += uri } }) { path, line -> opened += path to line }
        handler.openUri(PathLinks.uri("src/a.ts"))
        handler.openUri(PathLinks.uri("src/a.ts", 12))
        handler.openUri("https://example.com")
        assertEquals(listOf("src/a.ts" to null, "src/a.ts" to 12), opened)
        assertEquals(listOf("https://example.com"), outer)
    }

    @Test fun aPathsLineTravelsInItsLink() {
        assertEquals("persona-path:src%2Fa.kt#L12", PathLinks.uri("src/a.kt", 12))
        assertEquals("src/a.kt", PathLinks.pathOf(PathLinks.uri("src/a.kt", 12)))
        assertEquals(12, PathLinks.lineOf(PathLinks.uri("src/a.kt", 12)))
        assertNull(PathLinks.lineOf(PathLinks.uri("src/a.kt")))
        assertNull(PathLinks.lineOf("https://example.com/#L3"))
        // Each way of writing a line: the link opens the file at it; the text stays as written.
        val linkable = setOf("src/a.kt")
        assertEquals("见 [src/a.kt:12](persona-path:src%2Fa.kt#L12)", PathLinks.markdown("见 src/a.kt:12", linkable))
        assertEquals("见 [src/a.kt#L12](persona-path:src%2Fa.kt#L12)", PathLinks.markdown("见 src/a.kt#L12", linkable))
        assertEquals("见 [src/a.kt:12:3](persona-path:src%2Fa.kt#L12)", PathLinks.markdown("见 src/a.kt:12:3", linkable))
        assertEquals("见 [`src/a.kt:7`](persona-path:src%2Fa.kt#L7)", PathLinks.markdown("见 `src/a.kt:7`", linkable))
        val t = "改了 src/a.kt:40"
        assertEquals(listOf(40), PathLinks.plainLinks(t, PathRefs.find(t)) { it in linkable }.map { it.line })
    }

    @Test fun plainTextLinksThePathsThereNotInsideAWebLink() {
        val t = "读 /tmp/persona-tasks/1664-lark-test.md 并完整执行，别动 /tmp/other/x.md，见 https://example.com/tmp/a/b.md"
        val links = PathLinks.plainLinks(t, PathRefs.find(t)) { there[it] != null }
        assertEquals(listOf("/tmp/persona-tasks/1664-lark-test.md"), links.map { t.substring(it.start, it.end) })
    }

    @Test fun asksOnceForThePathsShownTogetherAndRemembers() = runTest {
        val asked = mutableListOf<List<String>>()
        val presence = PathPresence(this, { paths -> asked += paths; stats(paths) }, now = { testScheduler.currentTime })
        presence.ask(listOf("src/a.ts", "/nope/x.md"))
        presence.ask(listOf("src/a.ts", "~/notes/", "/Users/me/Desktop/x.md"))
        assertNull(presence.kind("src/a.ts")) // plain text until the answer
        advanceTimeBy(PathPresence.WAIT_MS + 1); runCurrent()
        assertEquals(listOf(listOf("src/a.ts", "/nope/x.md", "~/notes/", "/Users/me/Desktop/x.md")), asked)
        assertTrue(presence.linkable("src/a.ts") && presence.linkable("~/notes/") && presence.linkable("/Users/me/Desktop/x.md"))
        assertFalse(presence.linkable("/nope/x.md"))
        presence.ask(listOf("src/a.ts", "/nope/x.md"))
        advanceTimeBy(PathPresence.WAIT_MS + 1); runCurrent()
        assertEquals(1, asked.size)
        // a missing one is asked again after two minutes (the agent may have written it), one there after thirty
        advanceTimeBy(2 * 60_000L)
        presence.ask(listOf("src/a.ts", "/nope/x.md"))
        advanceTimeBy(PathPresence.WAIT_MS + 1); runCurrent()
        assertEquals(listOf("/nope/x.md"), asked.last())
    }

    @Test fun noAnswerLeavesPathsUnknownAndAsksAgainInAMinute() = runTest {
        var fail = true
        var calls = 0
        val presence = PathPresence(this, { paths -> calls++; if (fail) error("offline") else stats(paths) }, now = { testScheduler.currentTime })
        presence.ask(listOf("src/a.ts"))
        advanceTimeBy(PathPresence.WAIT_MS + 1); runCurrent()
        assertNull(presence.kind("src/a.ts"))
        assertFalse(presence.linkable("src/a.ts"))
        presence.ask(listOf("src/a.ts"))
        advanceTimeBy(PathPresence.WAIT_MS + 1); runCurrent()
        assertEquals(1, calls)
        fail = false
        advanceTimeBy(60_000L)
        presence.ask(listOf("src/a.ts"))
        advanceTimeBy(PathPresence.WAIT_MS + 1); runCurrent()
        assertEquals(2, calls)
        assertTrue(presence.linkable("src/a.ts"))
    }

    @Test fun pathsAskedWhileAnAnswerIsOnItsWayAreAskedToo() = runTest {
        val gate = kotlinx.coroutines.CompletableDeferred<Unit>()
        val asked = mutableListOf<List<String>>()
        val presence = PathPresence(this, { paths -> asked += paths; if (asked.size == 1) gate.await(); stats(paths) }, now = { testScheduler.currentTime })
        presence.ask(listOf("src/a.ts"))
        advanceTimeBy(PathPresence.WAIT_MS + 1); runCurrent()
        presence.ask(listOf("~/notes/")) // the first answer has not come
        advanceTimeBy(PathPresence.WAIT_MS + 1); runCurrent()
        assertEquals(listOf(listOf("src/a.ts"), listOf("~/notes/")), asked)
        gate.complete(Unit); runCurrent()
        assertTrue(presence.linkable("src/a.ts") && presence.linkable("~/notes/"))
    }

    private fun stats(paths: List<String>) = SessionPathStats(paths.map { SessionPathStat(it, there[it] ?: "missing") })

}
