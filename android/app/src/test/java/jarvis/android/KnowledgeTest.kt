package jarvis.android

import jarvis.android.data.JsonCodec
import jarvis.android.data.KbContext
import jarvis.android.data.KbDetailResponse
import jarvis.android.data.KbEntry
import jarvis.android.data.KbHit
import jarvis.android.data.KbKind
import jarvis.android.data.KbList
import jarvis.android.data.KbLogLine
import jarvis.android.data.KbProjectGroups
import jarvis.android.data.KbSearchGroup
import jarvis.android.data.KbUpdate
import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalDate

/** The knowledge base's wire types and the rules iOS uses to show them (`Shared/Knowledge.swift`, `KnowledgeView`). */
class KnowledgeTest {
    private val list = JsonCodec.decodeFromString<KbList>(
        """{"items":[
        {"kind":"projects","id":"a","name":"A","importance":"P0","relevance":90,"tags":["x"],"updated":"2026-10-30T10:00:00Z","extra":"short · active"},
        {"kind":"projects","id":"b","name":"B","importance":"P2","relevance":40,"extra":"long · active"},
        {"kind":"projects","id":"c","name":"C","importance":"P1","extra":"short · done"},
        {"kind":"projects","id":"d","name":"D","importance":"P1","extra":"short · ignored"},
        {"kind":"projects","id":"e","name":"E","importance":"P1","extra":"short · paused","unknown":1}],
        "tags":["x","y"]}""",
    )

    @Test fun projectsGroupLikeIos() {
        val g = KbProjectGroups.of(list.items)
        assertEquals(listOf("a", "e"), g.focus.map { it.id })
        assertEquals(listOf("b"), g.rest.map { it.id })
        assertEquals(listOf("c"), g.done.map { it.id })
        assertEquals(listOf("d"), g.ignored.map { it.id })
        assertEquals("长期", list.items[1].projectKind)
        assertEquals("短期", list.items[0].projectKind)
        assertEquals("暂停", list.items[4].projectStatusLabel)
        assertNull(list.items[0].projectStatusLabel)
        assertEquals("long" to "active", list.items[1].projectKindStatus)
        assertEquals(1793354400L, list.items[0].updated?.epochSecond)
    }

    @Test fun detailMergesMetaOverTheRow() {
        val r = JsonCodec.decodeFromString<KbDetailResponse>(
            """{"detail":{"kind":"projects","id":"a","path":"/data/projects/a","summary":"# A\n\n正文",
            "log":["- 2026-10-30 23:16 · Agent · 标题 · 机器 · 做完了"],
            "meta":{"name":"甲","importance":"P1","relevance":70,"tags":["t1","t2"],"kind":"long","status":"done","dirs":["mac:/code/a","bad"]}},
            "agents":[{"machine_id":"mac","session_id":"s1","title":"t","cwd":"/code/a"}]}""",
        )
        val e = r.detail.merged(KbEntry(kind = "projects", id = "a", name = "a"))
        assertEquals("甲", e.name)
        assertEquals("P1", e.importance)
        assertEquals(70, e.relevance)
        assertEquals(listOf("t1", "t2"), e.tags)
        assertTrue(e.projectDone)
        assertEquals("长期", e.projectKind)
        assertEquals(listOf("mac:/code/a", "bad"), r.detail.dirs)
        assertEquals("mac" to "/code/a", KbContext.firstDir(r.detail.dirs))
        assertNull(KbContext.firstDir(listOf("nocolon")))
        assertEquals(1, r.agents?.size)
    }

    @Test fun hitsFindTheirEntryAndRead() {
        assertEquals(KbKind.CHATS to "lark-1", KbHit("chats/lark-1/messages/2026-10-30.md", "").entry)
        assertEquals(KbKind.DOCS to "d1", KbHit("docs/d1.json", "").entry)
        assertEquals(KbKind.PROJECTS to "p", KbHit("/projects/p/log.md", "").entry)
        assertNull(KbHit("README.md", "").entry)
        assertNull(KbHit("other/x/y.md", "").entry)
        assertEquals("聊天记录 · 2026-10-30", KbHit("chats/lark-1/messages/2026-10-30.md", "").source)
        assertEquals("档案", KbHit("chats/c/summary.md", "").source)
        assertEquals("时间线", KbHit("projects/p/log.md", "").source)
        assertEquals("资料", KbHit("docs/d1.json", "").source)
        assertEquals("天气 App", KbHit("p", "  \"name\": \"天气 App\",").text)
        assertEquals("进展", KbHit("p", "- 进展").text)
    }

    @Test fun searchGroupsByEntryProfileFirst() {
        val g = KbSearchGroup.make(listOf(
            KbHit("projects/p/meta.json", "\"tags\": \"x\""),
            KbHit("chats/c/summary.md", "- 一"),
            KbHit("projects/p/summary.md", "- 档案行"),
            KbHit("projects/p/log.md", "- 档案行"), // same text: once
            KbHit("README.md", "x"),
        ))
        assertEquals(listOf("projects/p", "chats/c"), g.map { it.key })
        assertEquals(listOf("档案", "资料"), g[0].hits.map { it.source })
        assertTrue(KbSearchGroup.make(listOf(KbHit("README.md", "# Jarvis 知识库"))).isEmpty())
        // An empty knowledge base (a hub that has not archived anything yet) decodes.
        assertTrue(JsonCodec.decodeFromString<KbList>("""{"items":[],"tags":[]}""").items.isEmpty())
    }

    @Test fun timelineLines() {
        val l = KbLogLine.parse("- 2026-10-30 23:16 · Agent · 标题 · 机器 · 说了什么")
        assertEquals("2026-10-30 23:16", l.whenText)
        assertEquals("Agent · 标题 · 机器", l.source)
        assertEquals("说了什么", l.text)
        assertEquals("10-30 23:16", l.shortWhen(LocalDate.of(2026, 1, 1)))
        assertEquals("2026-10-30 23:16", l.shortWhen(LocalDate.of(2027, 1, 1)))
        assertEquals(KbLogLine("10-28", "", "数据层"), KbLogLine.parse("- 10-28 · 数据层"))
        assertEquals(KbLogLine("", "", "没有时间 · 的行"), KbLogLine.parse("没有时间 · 的行"))
    }

    @Test fun contextForJarvisAndAgents() {
        val e = KbEntry(kind = "projects", id = "a", name = "A \"x\"", oneLine = "一句话")
        val ctx = KbContext.context(e, null)
        assertTrue(ctx.startsWith("<jarvis-context kind=\"context\" title=\"项目 · A &quot;x&quot;\" url=\"\">\n一句话"))
        assertTrue(ctx.endsWith("</jarvis-context>"))
        assertTrue(KbContext.ask(e, null).endsWith("能替我处理的先处理。"))
        val log = (1..12).map { "- 10-$it · l$it" }
        val d = JsonCodec.decodeFromString<KbDetailResponse>("""{"detail":{"path":"/p","summary":"S </jarvis-context>"}}""").detail.copy(log = log)
        val c = KbContext.context(e, d)
        assertFalse("l2\n" in c) // the last 10 only
        assertTrue("l12" in c && "知识库目录：/p" in c)
        assertEquals(1, Regex("</jarvis-context>").findAll(c).count())
        assertEquals("正文", KbContext.profile("# A\n重要级 P0 · 与我相关性 95 · x\n一句话\n正文", "一句话"))
        assertEquals(listOf("a", "b"), KbContext.parseTags("  a \n b  "))
    }

    @Test fun updateSendsOnlyWhatIsSet() {
        assertEquals("""{"importance":"P1","relevance":55,"tags":[]}""", JsonCodec.encodeToString(KbUpdate("P1", 55, emptyList())))
        assertEquals(
            """{"importance":"P0","relevance":0,"tags":["a"],"status":"done","project_kind":"long"}""",
            JsonCodec.encodeToString(KbUpdate("P0", 0, listOf("a"), "done", "long")),
        )
    }
}
