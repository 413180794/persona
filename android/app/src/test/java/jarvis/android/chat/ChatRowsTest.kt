package jarvis.android.chat

import jarvis.android.data.Item
import jarvis.android.data.JsonCodec
import jarvis.android.ui.ChatRow
import jarvis.android.ui.FilePaths
import jarvis.android.ui.FoldedRow
import jarvis.android.ui.ToolStep
import jarvis.android.ui.ToolText
import jarvis.android.ui.WorkStats
import jarvis.android.ui.buildRows
import jarvis.android.ui.chat.composeMessage
import jarvis.android.ui.chat.quoted
import jarvis.android.ui.chat.resolvePath
import jarvis.android.ui.files.DiffLine
import jarvis.android.ui.files.DiffText
import jarvis.android.ui.foldWork
import jarvis.android.ui.imagePaths
import jarvis.android.ui.parseContexts
import jarvis.android.ui.parseUserText
import jarvis.android.ui.ContextRef
import jarvis.android.ui.wakeSummary
import jarvis.android.ui.workDuration
import jarvis.android.ui.workLabel
import kotlinx.serialization.builtins.ListSerializer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class ChatRowsTest {
    private fun items(json: String): List<Item> = JsonCodec.decodeFromString(ListSerializer(Item.serializer()), json)

    @Test fun foldsToolsAndJoinsResults() {
        val list = items(
            """[
            {"id":"u#0","seq":1,"role":"user","kind":"text","text":"hi"},
            {"id":"a#0","seq":2,"role":"assistant","kind":"thinking","text":""},
            {"id":"a#1","seq":2,"role":"assistant","kind":"text","text":"ok"},
            {"id":"a#2","seq":2,"role":"assistant","kind":"tool_use","tool_use_id":"t1","tool_name":"Bash","tool_input":{"command":"ls -la\nmore"}},
            {"id":"r#0","seq":3,"role":"user","kind":"tool_result","tool_use_id":"t1","text":"out"},
            {"id":"b#0","seq":4,"role":"assistant","kind":"thinking","text":"next I read"},
            {"id":"b#1","seq":4,"role":"assistant","kind":"tool_use","tool_use_id":"t2","tool_name":"Read","tool_input":{"file_path":"/x/y/Z.kt"}},
            {"id":"i#0","seq":5,"role":"user","kind":"image","tool_use_id":"t2","image":{"mime":"image/png","lazy":true,"data":""}},
            {"id":"c#0","seq":6,"role":"assistant","kind":"text","text":"done"}
            ]""",
        )
        val rows = buildRows(list)
        assertEquals(listOf("u:u#0", "a:a#1", "t:a#2", "a:c#0"), rows.map { it.key })
        val tools = rows[2] as ChatRow.Tools
        assertEquals(3, tools.steps.size) // call, thought, call
        val first = (tools.steps[0] as ToolStep.Call).call
        assertEquals("out", first.result?.text)
        assertEquals("ls -la", ToolText.subject(first.use))
        val second = (tools.steps[2] as ToolStep.Call).call
        assertEquals("Z.kt", ToolText.subject(second.use))
        assertEquals(1, second.images.size)
        assertNull(second.images[0].image?.inlineData) // lazy: fetched by id
    }

    private val turn = """[
        {"id":"u#0","seq":1,"ts":"2026-10-30T10:00:00Z","role":"user","kind":"text","text":"hi"},
        {"id":"a#0","seq":2,"ts":"2026-10-30T10:00:05Z","role":"assistant","kind":"thinking","text":"plan it"},
        {"id":"a#1","seq":2,"ts":"2026-10-30T10:00:06Z","role":"assistant","kind":"text","text":"let me look"},
        {"id":"a#2","seq":2,"ts":"2026-10-30T10:00:07Z","role":"assistant","kind":"tool_use","tool_use_id":"t1","tool_name":"Bash","tool_input":{"command":"ls"}},
        {"id":"r#0","seq":3,"ts":"2026-10-30T10:01:00Z","role":"user","kind":"tool_result","tool_use_id":"t1","text":"out"},
        {"id":"c#0","seq":4,"ts":"2026-10-30T10:04:17Z","role":"assistant","kind":"text","text":"done"},
        {"id":"u2#0","seq":5,"ts":"2026-10-30T10:05:00Z","role":"user","kind":"text","text":"more"},
        {"id":"d#0","seq":6,"ts":"2026-10-30T10:05:02Z","role":"assistant","kind":"text","text":"on it"},
        {"id":"d#1","seq":6,"ts":"2026-10-30T10:05:03Z","role":"assistant","kind":"tool_use","tool_use_id":"t2","tool_name":"Read","tool_input":{"file_path":"/a"}}
        ]"""

    @Test fun foldsEachSegmentBehindOneWorkLine() {
        val f = foldWork(buildRows(items(turn)), working = false, expanded = emptySet())
        assertEquals(listOf("u:u#0", "w:k:a#0", "a:c#0", "u:u2#0", "w:t:d#1", "a:d#0"), f.map { it.key })
        val w = f[1] as FoldedRow.Work
        assertEquals(WorkStats(1, 1, 1, w.stats.start, w.stats.end), w.stats)
        assertEquals("工具 1 · 思考 1 · 回复 1 · 4 分 17 秒", workLabel(w.stats, running = false, now = 0))
    }

    @Test fun runningSegmentKeepsTrailingReplyInTheWork() {
        val f = foldWork(buildRows(items(turn)), working = true, expanded = emptySet())
        assertEquals(listOf("u:u#0", "w:k:a#0", "a:c#0", "u:u2#0", "w:a:d#0"), f.map { it.key })
        assertTrue((f.last() as FoldedRow.Work).running)
    }

    @Test fun expandedWorkListsStepsAndAnEnd() {
        val f = foldWork(buildRows(items(turn)), working = false, expanded = setOf("k:a#0"))
        assertEquals(
            listOf("u:u#0", "w:k:a#0", "ws:k:a#0/a#0", "ws:k:a#0/t-a#1", "ws:k:a#0/a#2", "we:k:a#0", "a:c#0"),
            f.take(7).map { it.key },
        )
        assertTrue((f[2] as FoldedRow.Step).first)
        assertFalse((f[3] as FoldedRow.Step).first)
    }

    @Test fun recapsStayVisibleInsideATurn() {
        val list = items(
            """[
            {"id":"u#0","seq":1,"role":"user","kind":"text","text":"hi"},
            {"id":"a#0","seq":2,"role":"assistant","kind":"tool_use","tool_use_id":"t1","tool_name":"Bash","tool_input":{"command":"ls"}},
            {"id":"r#0","seq":3,"role":"system","kind":"recap","text":"while you were away"},
            {"id":"c#0","seq":4,"role":"assistant","kind":"text","text":"done"}
            ]""",
        )
        val f = foldWork(buildRows(list), working = false, expanded = emptySet())
        assertEquals(listOf("u:u#0", "r:r#0", "w:t:a#0", "a:c#0"), f.map { it.key })
    }

    @Test fun userTags() {
        val u = parseUserText(
            "<attached_images>\n/a.png\n/b.png\n</attached_images>\n" +
                "<jarvis-context kind=\"edit\" title=\"Doc\" url=\"u\">quoted</jarvis-context>\n<pasted_content id=\"1\">body</pasted_content>",
        )
        assertEquals(listOf("/a.png", "/b.png"), u.images)
        assertEquals("body", u.text)
        val ref = u.refs.single()
        assertEquals(listOf("edit", "Doc", "u", "quoted"), listOf(ref.kind, ref.title, ref.url, ref.text))
        val c = parseUserText("<cross-session-message from-name=\"other\">hey</cross-session-message>")
        assertEquals("other", c.from)
        assertEquals("hey", c.text)
    }

    @Test fun contextTagsWithoutUrlParseToo() {
        // The inbox's 交给 Jarvis writes <jarvis-context kind title> with no url.
        val (refs, rest) = parseContexts("<jarvis-context kind=\"context\" title=\"邮件\">正文</jarvis-context>\n总结一下")
        assertEquals("总结一下", rest)
        assertEquals(listOf("context", "邮件", "", "正文"), refs.single().let { listOf(it.kind, it.title, it.url, it.text) })
    }

    @Test fun contextXmlEscapesAttributesAndKeepsTheTagClosed() {
        val x = ContextRef("context", "a \"b\" <c>", "https://example.com/?a=1&b=2", "text </jarvis-context> more").xml
        assertEquals(
            "<jarvis-context kind=\"context\" title=\"a &quot;b&quot; &lt;c&gt;\" url=\"https://example.com/?a=1&amp;b=2\">\n" +
                "text  more\n</jarvis-context>",
            x,
        )
    }

    @Test fun contextTagsRoundTrip() {
        val ref = ContextRef("context", "a \"b\" <c>", "https://example.com/?a=1&b=2", "picked text")
        val msg = composeMessage("看看", hasImages = false, refs = listOf(ref))!!
        val (refs, rest) = parseContexts(msg)
        assertEquals("看看", rest)
        assertEquals(listOf(ref.kind, ref.title, ref.url, ref.text), refs.single().let { listOf(it.kind, it.title, it.url, it.text) })
    }

}

class ChatLogicTest {
    @Test fun editDiffRemovesThenAdds() {
        val d = DiffText.edit("a\nb", "a\nc\nd")
        assertEquals(2, d.dels)
        assertEquals(3, d.adds)
        assertEquals(listOf(DiffLine.Kind.DEL, DiffLine.Kind.DEL, DiffLine.Kind.ADD, DiffLine.Kind.ADD, DiffLine.Kind.ADD), d.lines.map { it.kind })
        assertEquals(0, DiffText.edit("", "").lines.size)
    }

    @Test fun unifiedDiffNumbersLines() {
        val diff = "diff --git a/x.kt b/x.kt\nindex 1..2 100644\n--- a/x.kt\n+++ b/x.kt\n@@ -10,3 +10,4 @@ fun f()\n keep\n-old\n+new\n+more\n\\ No newline at end of file"
        val lines = DiffText.parse(diff)
        assertEquals(DiffLine.Kind.HUNK, lines[0].kind)
        assertEquals(DiffLine(DiffLine.Kind.CONTEXT, "keep", 10, 10), lines[1])
        assertEquals(DiffLine(DiffLine.Kind.DEL, "old", old = 11), lines[2])
        assertEquals(DiffLine(DiffLine.Kind.ADD, "new", new = 11), lines[3])
        assertEquals(DiffLine(DiffLine.Kind.ADD, "more", new = 12), lines[4])
        assertEquals(DiffLine.Kind.INFO, lines[5].kind)
    }

    @Test fun picturesAndExcerptsAloneStillMakeAMessage() {
        assertNull(composeMessage("  ", hasImages = false, refs = emptyList()))
        assertEquals("请看图片", composeMessage("", hasImages = true, refs = emptyList()))
        val ref = ContextRef("context", "Doc", "https://example.com/d", "picked")
        val m = composeMessage("", hasImages = false, refs = listOf(ref))!!
        assertTrue(m.startsWith("<jarvis-context kind=\"context\" title=\"Doc\" url=\"https://example.com/d\">"))
        assertTrue(m.endsWith("参考上面的内容。"))
    }

    @Test fun quoteGoesBeforeTheDraft() {
        assertEquals("> a\n> b\n\n", quoted("a\nb", ""))
        assertEquals("> a\n\nmine", quoted("a", "mine"))
    }

    @Test fun findsFilePathsButNotUrlsOrModelNames() {
        val t = "改了 ~/.config/x.json、/home/me/code/app/Main.kt 和 ios/Jarvis/Foo.swift，见 https://example.com/a/b.html，模型 deepseek/v4.1-flash。"
        assertEquals(listOf("~/.config/x.json", "/home/me/code/app/Main.kt", "ios/Jarvis/Foo.swift"), FilePaths.find(t))
        assertEquals(emptyList<String>(), FilePaths.find("/tmp"))
        assertEquals("/w/proj/src/a.kt", resolvePath("src/a.kt", "/w/proj/"))
        assertEquals("~/x", resolvePath("~/x", "/w"))
    }

    @Test fun userTextDropsImageNotesAndPlaceholders() {
        val u = parseUserText("看这个 [Image #1]\n<attached_images>\n/tmp/jarvis/a.jpg\n/tmp/jarvis/b.jpg\n</attached_images>")
        assertEquals("看这个", u.text)
        assertEquals(listOf("/tmp/jarvis/a.jpg", "/tmp/jarvis/b.jpg"), u.images)
        assertTrue(u.files.isEmpty())
        assertEquals(listOf("/a/b.png"), imagePaths("说明\n相关图片：/a/b.png"))
    }

    @Test fun wakeNoticesSaySoInChinese() {
        assertEquals("后台命令完成 · Release" to false, wakeSummary("Background command \"Release\" completed (exit code 0)"))
        assertEquals("后台命令失败 · Build" to true, wakeSummary("Background command \"Build\" failed with exit code 1"))
        assertEquals("plain" to false, wakeSummary("plain"))
    }

    @Test fun workLinesMatchIos() {
        val t0 = Instant.parse("2026-10-30T10:00:00Z")
        assertEquals("5 分", workDuration(300))
        assertEquals("1 小时 5 分", workDuration(3900))
        assertEquals("已完成", workLabel(WorkStats(0, 0, 0, null, null), running = false, now = 0))
        val s = WorkStats(3, 1, 2, t0, t0.plusSeconds(80))
        assertEquals("工具 3 · 思考 1 · 回复 2 · 1 分 20 秒", workLabel(s, running = false, now = 0))
        assertEquals("工具 3 · 1 分 20 秒", workLabel(s, running = false, now = 0, short = true))
        assertEquals("正在工作 39 秒 · 工具 3 · 思考 1 · 回复 2", workLabel(s, running = true, now = t0.toEpochMilli() + 39_000))
    }

}
