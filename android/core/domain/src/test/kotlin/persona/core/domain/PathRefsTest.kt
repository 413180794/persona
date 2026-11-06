package persona.core.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The web's path-refs.test.ts, case for case: the two clients link the same paths. */
class PathRefsTest {
    private fun paths(t: String) = PathRefs.find(t).map { r -> r.line?.let { "${r.path}:$it" + (r.col?.let { c -> ":$c" } ?: "") } ?: r.path }

    @Test fun findsAbsoluteHomeAndRelativePathsWithAFolder() {
        assertEquals(listOf("/tmp/persona-tasks/1664-lark-test.md"), (paths("读 /tmp/persona-tasks/1664-lark-test.md 并完整执行这个任务")).zhAny())
        assertEquals(listOf("~/code/app/README.md", "web/src/lib/api.ts"), (paths("看 ~/code/app/README.md 和 web/src/lib/api.ts")).zhAny())
        assertEquals(listOf("/var/log/"), (paths("目录 /var/log/ 里")).zhAny())
    }

    @Test fun takesALine() {
        assertEquals(listOf("server/internal/node/files.go:63", "web/src/ui/Markdown.tsx:322:7"),
            (paths("见 server/internal/node/files.go:63 和 web/src/ui/Markdown.tsx:322:7")).zhAny())
        assertEquals(listOf("src/a.ts:12", "/x/y/b.go:8"), (paths("改了 src/a.ts:12-20，还有 /x/y/b.go#L8")).zhAny())
        val t = "at src/a.ts:12."
        val r = PathRefs.find(t).single()
        assertEquals(PathRef(3, 14, "src/a.ts", 12), r)
        assertEquals("src/a.ts:12", (t.substring(r.start, r.end)).zhAny())
    }

    @Test fun endsAnAsciiPathAtChineseTextAndAFullStop() {
        assertEquals(listOf("/tmp/a.md"), (paths("写到 /tmp/a.md里了")).zhAny())
        assertEquals(listOf("/tmp/out/report.md"), (paths("已写入 /tmp/out/report.md。")).zhAny())
        assertEquals(listOf("/tmp/out/report.md"), (paths("See /tmp/out/report.md.")).zhAny())
        assertEquals(listOf("/tmp/a/b.txt"), (paths("（/tmp/a/b.txt）")).zhAny())
    }

    @Test fun leavesUrlsWordsVersionsAndLoneRootsAlone() {
        assertEquals(emptyList<String>(), (paths("https://example.com/a/b.md and //cdn/x/y.js")).zhAny())
        assertEquals(emptyList<String>(), (paths("model deepseek/v4.1-flash, and/or, /tmp alone, 1/2")).zhAny())
        assertEquals(emptyList<String>(), (paths("a ~ b / c")).zhAny())
    }

    @Test fun takesAPathInBackticksWhole() {
        assertEquals(listOf("~/报告/第四季度.docx"), (paths("生成了 `~/报告/第四季度.docx`")).zhAny())
        assertEquals("~/My Notes/todo.md:3", (paths("在 `~/My Notes/todo.md:3`").first()).zhAny())
        assertEquals(listOf("/tmp/a.md 和 /tmp/b.md", "/tmp/a.md", "/tmp/b.md"), (paths("`/tmp/a.md 和 /tmp/b.md`")).zhAny())
        assertEquals(listOf("src/a.ts"), (PathRefs.unique(PathRefs.find("src/a.ts:1 src/a.ts:2 `src/a.ts`"))).zhAny())
    }

    @Test fun aCodeSpanThatIsAWholePath() {
        assertEquals(PathRef(0, 11, "src/a.ts", 12), PathRefs.codePath("src/a.ts:12"))
        assertEquals(PathRef(1, 12, "/tmp/x/y.md"), PathRefs.codePath(" /tmp/x/y.md "))
        assertEquals("~/报告/第四季度.docx", (PathRefs.codePath("~/报告/第四季度.docx")?.path).zhAny())
        assertNull(PathRefs.codePath("ls /tmp/x/y.md"))
        assertNull(PathRefs.codePath("/tmp"))
        assertNull(PathRefs.codePath("https://example.com/a/b"))
    }

    @Test fun splitTakesTheRefsThereTheLongerFirstNoneOverlapping() {
        val t = "`/tmp/a.md 和 /tmp/b.md`"
        val there = setOf("/tmp/a.md", "/tmp/b.md")
        assertEquals(listOf("/tmp/a.md", "/tmp/b.md"), (PathRefs.split(PathRefs.find(t)) { it in there }.map { it.path }).zhAny())
        assertEquals(listOf("/tmp/a.md 和 /tmp/b.md"), (PathRefs.split(PathRefs.find(t)) { true }.map { it.path }).zhAny())
        assertEquals(emptyList<PathRef>(), (PathRefs.split(PathRefs.find("x /tmp/a/b.md")) { false }).zhAny())
    }
}
