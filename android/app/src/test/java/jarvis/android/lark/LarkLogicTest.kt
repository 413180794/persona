package jarvis.android.lark

import jarvis.android.data.JsonCodec
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant
import java.time.ZoneId
import java.time.ZonedDateTime

class LarkLogicTest {
    private val zone = ZoneId.of("Asia/Shanghai")

    private fun msg(pos: Int, sender: String = "A", mine: Boolean = false, at: Instant? = null, type: String = "TEXT", isRead: Boolean? = null) =
        LarkMessage(id = "m$pos", position = "$pos", senderName = sender, isMine = mine, type = type, createdAt = at, isRead = isRead)

    @Test fun decodesMessageWithPartsReplyAndReactions() {
        val json = """
        {"id":"m1","position":"9","sender_name":"A","is_mine":false,"type":"POST","text":"hi","recalled":false,"reply_count":0,
         "created_at":"2026-10-30T08:00:00Z","unknown_field":1,
         "parts":[{"kind":"image","url":"/api/v1/lark/media/k","metadata":{"width":10,"height":20}}],
         "reply_to":{"id":"m0","sender_name":"B","text":"q"},
         "reactions":[{"key":"THUMBSUP","count":2,"mine":true,"users":["A","B"]}]}
        """
        val m = JsonCodec.decodeFromString(LarkMessage.serializer(), json)
        assertEquals(10.0, m.parts!!.first().meta("width").dbl!!, 0.0)
        assertEquals("B", m.replyTo?.senderName)
        assertTrue(m.reactions!!.first().mine)
        assertTrue(LarkParts.isBare(m.parts!!))
        assertNotNull(m.createdAt)
        val page = JsonCodec.decodeFromString(LarkMessagesPage.serializer(), """{"messages":[$json],"has_older":true}""")
        assertTrue(page.hasOlder)
        assertNull(page.hasNewer)
    }

    @Test fun forwardedMessagesDecodeCamelCase() {
        val list = JsonCodec.decodeFromString(ListSerializer(LarkForwardedMessage.serializer()),
            """[{"id":"f","senderName":"李雷","senderAvatarUrl":"/api/media/a","createdAt":"2026-10-29T08:00:00Z","parts":[{"kind":"text","text":"x"}]}]""")
        assertEquals("李雷", list[0].senderName)
        assertNotNull(list[0].createdAt)
    }

    @Test fun positionsCompareAsNumbers() {
        assertTrue(LarkPosition.less("99", "100"))
        assertFalse(LarkPosition.less("100", "99"))
        assertFalse(LarkPosition.less("7", "7"))
        assertTrue(LarkPosition.less("-3", "0"))
        assertTrue(LarkPosition.less("-10", "-3"))
        assertTrue(LarkPosition.less("99999999999999999999", "100000000000000000000"))
        assertEquals("68308", LarkPosition.next("68307"))
        assertEquals("199", LarkPosition.previous("200"))
    }

    @Test fun runsOfTheSameSender() {
        val t = Instant.parse("2026-10-30T08:00:00Z")
        val a = msg(1, at = t)
        assertTrue(LarkChatRules.continues(msg(2, at = t.plusSeconds(120)), a))
        assertFalse(LarkChatRules.continues(msg(2, at = t.plusSeconds(400)), a))
        assertFalse(LarkChatRules.continues(msg(2, sender = "B", at = t.plusSeconds(10)), a))
        assertFalse(LarkChatRules.continues(msg(2, at = t.plusSeconds(10), type = "SYSTEM"), a))
        assertFalse(LarkChatRules.continues(msg(2, at = t), null))
    }

    @Test fun dayLinesAndLabels() {
        val now = ZonedDateTime.of(2026, 10, 30, 15, 0, 0, 0, zone).toInstant()
        val today = ZonedDateTime.of(2026, 10, 30, 9, 5, 0, 0, zone).toInstant()
        assertEquals("09:05", LarkChatRules.dayLabel(today, now, zone))
        assertEquals("昨天 09:05", LarkChatRules.dayLabel(today.minusSeconds(86400), now, zone))
        assertEquals("10月1日 09:05", LarkChatRules.dayLabel(ZonedDateTime.of(2026, 10, 1, 9, 5, 0, 0, zone).toInstant(), now, zone))
        assertEquals("2025年10月1日 09:05", LarkChatRules.dayLabel(ZonedDateTime.of(2025, 10, 1, 9, 5, 0, 0, zone).toInstant(), now, zone))
        assertTrue(LarkChatRules.startsDay(msg(1, at = today), null, zone))
        assertFalse(LarkChatRules.startsDay(msg(2, at = today.plusSeconds(3600)), msg(1, at = today), zone))
        assertTrue(LarkChatRules.startsDay(msg(2, at = today.plusSeconds(4 * 3600)), msg(1, at = today), zone))
    }

    @Test fun newMessagesLineSitsAboveTheFirstUnread() {
        val ms = listOf(msg(198), msg(199), msg(200), msg(201))
        assertFalse(LarkChatRules.isFirstUnread(ms[1], ms[0], "199"))
        assertTrue(LarkChatRules.isFirstUnread(ms[2], ms[1], "199"))
        assertFalse(LarkChatRules.isFirstUnread(ms[3], ms[2], "199"))
        assertTrue(LarkChatRules.isFirstUnread(ms[2], null, "199"))
        assertFalse(LarkChatRules.isFirstUnread(ms[2], ms[1], null))
    }

    @Test fun unseenCountsOnlyOthersOnScreen() {
        val ms = listOf(msg(200), msg(201, mine = true), msg(202), msg(203, type = "SYSTEM"))
        assertEquals(4, LarkChatRules.unseenNew(ms, emptySet(), "199", 4))
        assertEquals(2, LarkChatRules.unseenNew(ms, setOf("200", "201", "202", "203"), "199", 4))
        assertEquals(0, LarkChatRules.unseenNew(ms, emptySet(), null, 4))
    }

    @Test fun readReportOnlyCoversWhatWasOnScreen() {
        val ms = listOf(msg(199), msg(200), msg(201, mine = true), msg(202, isRead = true), msg(203), msg(204))
        assertNull(LarkChatRules.readReport(ms, emptySet(), "199"))
        assertNull(LarkChatRules.readReport(ms, setOf("199"), "199"))
        val r = LarkChatRules.readReport(ms, setOf("200", "201", "202", "203"), "199")!!
        assertEquals("203", r.position)
        // 201 is mine, 202 already read: only the others' unread ones up to the newest seen.
        assertEquals(listOf("m200", "m203"), r.messageIds)
        val none = LarkChatRules.readReport(listOf(msg(205, mine = true)), setOf("205"), "204")!!
        assertNull(none.messageIds)
    }

    @Test fun latestPageMergesInPlaceAndAppends() {
        val cur = listOf(msg(1), msg(2), msg(3))
        val latest = listOf(msg(2).copy(text = "edited"), msg(3), msg(4), msg(5))
        val (merged, fresh) = LarkChatRules.mergeLatest(cur, latest)
        assertEquals(2, fresh)
        assertEquals(listOf("1", "2", "3", "4", "5"), merged.map { it.position })
        assertEquals("edited", merged[1].text)
        assertEquals(listOf("0", "1", "2", "3"), LarkChatRules.prependOlder(cur, listOf(msg(0), msg(1))).map { it.position })
        assertEquals(listOf("1", "2", "3", "4"), LarkChatRules.appendNewer(cur, listOf(msg(3), msg(4))).map { it.position })
    }

    @Test fun reactionToggle() {
        val m = msg(1).copy(reactions = listOf(LarkReaction("OK", 2, true, listOf("我", "B"))))
        val (a, removeA) = m.togglingReaction("ok", "我")
        assertTrue(removeA)
        assertEquals(1, a.reactions!![0].count)
        assertFalse(a.reactions!![0].mine)
        assertEquals(listOf("B"), a.reactions!![0].users)
        val (b, removeB) = a.togglingReaction("THUMBSUP", "我")
        assertFalse(removeB)
        assertEquals(2, b.reactions!!.size)
        assertTrue(b.reactions!!.last().mine)
        val (c, _) = msg(1).copy(reactions = listOf(LarkReaction("OK", 1, true, listOf("我")))).togglingReaction("OK", "我")
        assertTrue(c.reactions!!.isEmpty())
        val (d, removeD) = a.togglingReaction("OK", "我")
        assertFalse(removeD)
        assertEquals(2, d.reactions!![0].count)
    }

    @Test fun momentsLinks() {
        assertEquals("7000000000000000001", LarkMoments.postId("https://applink.example.com/client/moments/detail?postId=7000000000000000001&source=share"))
        assertNull(LarkMoments.postId("https://applink.example.com/client/moments/detail?postId=abc"))
        assertNull(LarkMoments.postId("https://example.com/a?postId=1"))
        assertNull(LarkMoments.postId(null))
    }

    @Test fun rebuildsRichBlockTreeAndAdoptsNestedLists() {
        val ps = JsonCodec.decodeFromString(ListSerializer(LarkPart.serializer()), """
        [{"kind":"text","text":"a","metadata":{"blocks":[{"id":"l","type":"ordered-list"},{"id":"i1","type":"list-item"}]}},
         {"kind":"text","text":"a1","metadata":{"blocks":[{"id":"l","type":"ordered-list"},{"id":"l2","type":"unordered-list"},{"id":"i3","type":"list-item"}]}},
         {"kind":"text","text":"b","metadata":{"blocks":[{"id":"l","type":"ordered-list"},{"id":"i2","type":"list-item"}]}},
         {"kind":"text","text":"p","metadata":{"blocks":[{"id":"p","type":"paragraph"}]}}]
        """)
        val tree = LarkRichNode.tree(ps)
        assertEquals(2, tree.size)
        val list = tree[0] as LarkRichNode.Block
        assertEquals("ordered-list", list.block.type)
        assertEquals(2, list.children.size)
        assertEquals(2, (list.children[0] as LarkRichNode.Block).children.size) // "a" and the adopted nested list
    }

    @Test fun listMarkers() {
        assertEquals("4.", LarkRichNode.marker(LarkRichBlock("", "ordered-list", 0, 3), 1))
        assertEquals("b.", LarkRichNode.marker(LarkRichBlock("", "ordered-list", 1, 1), 1))
        assertEquals("iv.", LarkRichNode.marker(LarkRichBlock("", "ordered-list", 3, 4), 0))
        assertEquals("•", LarkRichNode.marker(LarkRichBlock("", "unordered-list", 0, 1), 0))
        val items = listOf(0, 1, 1, 0).map { lvl -> buildJsonObject { put("type", "ol"); put("level", lvl) } }
        assertEquals(listOf("1.", "1.", "2.", "2."), larkCardListMarkers(items))
    }

    @Test fun bareParts() {
        fun p(kind: String, text: String? = null, url: String? = null) = LarkPart(kind, text, url)
        assertFalse(LarkParts.isBare(listOf(p("text", "hi"))))
        assertTrue(LarkParts.isBare(listOf(p("card"), p("text", " "))))
        assertTrue(LarkParts.isBare(listOf(p("emotion", url = "/s"))))
        assertFalse(LarkParts.isBare(listOf(p("emotion", text = "[OK]"))))
        assertTrue(LarkParts.isBare(listOf(p("link", url = "https://a.example.com/client/moments/detail?postId=1"))))
        assertFalse(LarkParts.isBare(listOf(p("image", url = "/a"), p("text", "caption"))))
        assertFalse(LarkParts.isBare(emptyList()))
    }

    @Test fun sizes() {
        assertEquals(120, larkThumbEdge(102))
        assertNull(larkThumbEdge(1200))
        assertEquals(200f to 150f, LarkParts.displaySize(0.0, 0.0))
        val (w, h) = LarkParts.displaySize(1280.0, 720.0)
        assertEquals(240f, w, 0.01f)
        assertEquals(135f, h, 0.01f)
        val (sw, sh) = LarkParts.displaySize(20.0, 20.0)
        assertEquals(60f, sw, 0.01f)
        assertEquals(40f, sh, 0.01f)
    }

    @Test fun zeroAndOddTimesAreNull() {
        assertNull(parseLarkTime("0001-01-01T00:00:00Z"))
        assertNull(parseLarkTime("soon"))
        assertNotNull(parseLarkTime("2026-10-30T08:00:00+08:00"))
        val m = JsonCodec.decodeFromString(LarkMessage.serializer(), """{"id":"m","created_at":"0001-01-01T00:00:00Z"}""")
        assertNull(m.createdAt)
    }
}
