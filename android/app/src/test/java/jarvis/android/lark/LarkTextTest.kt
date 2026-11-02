package jarvis.android.lark

import jarvis.android.data.JsonCodec
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.ZoneId

class LarkTextTest {
    private fun texts(runs: List<LarkInline>) = runs.mapNotNull { (it as? LarkInline.Text)?.text }

    private fun part(json: String) = JsonCodec.decodeFromString(LarkPart.serializer(), json)

    @Test fun cardMarkdownMarkupAndEntities() {
        val runs = LarkFormat.cardMarkdown("a&nbsp;b<br>see <a href='https://x.com'>x</a> <font color='red'>hot</font> &#60;tag&#x3E;")
        assertEquals("a b\nsee x hot <tag>", texts(runs).joinToString(""))
        val link = runs.first { it is LarkInline.Text && it.text == "x" } as LarkInline.Text
        assertEquals("https://x.com", link.style.link)
        val hot = runs.first { it is LarkInline.Text && it.text == "hot" } as LarkInline.Text
        assertEquals(LarkPalette.text("red"), hot.style.color)
        // javascript: links stay text
        val unsafe = LarkFormat.cardMarkdown("<a href=\"javascript:alert(1)\">x</a>")
        assertNull((unsafe[0] as LarkInline.Text).style.link)
    }

    @Test fun lingoTermsUseUtf16Offsets() {
        val runs = LarkPartRuns.lingoRuns("😀 联调好了", listOf(buildJsonObject { put("start", 3); put("end", 5) }), LarkRunStyle())
        val dotted = runs.filterIsInstance<LarkInline.Text>().filter { it.style.dotted }.map { it.text }
        assertEquals(listOf("联调"), dotted)
    }

    @Test fun systemTemplateHolders() {
        val p = part("""{"kind":"text","text":"刘森 创建了话题群","metadata":{"system":true,"systemTemplate":"{from_user} 创建了话题群","systemHolders":[{"key":"from_user","text":"刘森"}]}}""")
        assertEquals(listOf("刘森", " 创建了话题群"), texts(LarkPartRuns.systemRuns(p)))
        assertEquals(listOf("plain"), texts(LarkPartRuns.systemRuns(part("""{"kind":"text","text":"plain","metadata":{"system":true}}"""))))
    }

    @Test fun partRuns() {
        val at = LarkPartRuns.runs(part("""{"kind":"at","text":"@李雷"}"""), false).single() as LarkInline.Text
        assertEquals(LarkTint.blue, at.style.color)
        val link = LarkPartRuns.runs(part("""{"kind":"link","text":"设计稿","url":"https://example.com"}"""), false).single() as LarkInline.Text
        assertEquals("https://example.com", link.style.link)
        val emoji = LarkPartRuns.runs(part("""{"kind":"emotion","refId":"OK","text":"[OK]"}"""), false).single() as LarkInline.Emoji
        assertEquals("OK", emoji.key)
        val bold = LarkPartRuns.marks(part("""{"kind":"text","text":"x","metadata":{"marks":["bold","strikethrough"],"inlineCode":true}}"""))
        assertTrue(bold.bold && bold.strike && bold.code)
        assertTrue(LarkPartRuns.isInline(part("""{"kind":"text","text":"x"}""")))
        assertTrue(!LarkPartRuns.isInline(part("""{"kind":"text","text":"x","metadata":{"markdown":true}}""")))
        assertTrue(!LarkPartRuns.isInline(part("""{"kind":"emotion","url":"/sticker"}""")))
    }

    @Test fun flowRunsAddBulletsAndDropTrailingBreaks() {
        val ps = listOf(
            part("""{"kind":"text","text":"one","metadata":{"blockStart":true,"block":"list-item","blockEnd":true}}"""),
            part("""{"kind":"text","text":"two","metadata":{"blockEnd":true}}"""),
        )
        assertEquals("•  one\ntwo", texts(LarkPartRuns.flowRuns(ps, false)).joinToString(""))
    }

    @Test fun colors() {
        assertNotNull(LarkPalette.hex("#f8fafeff"))
        assertNotNull(LarkPalette.hex("#abc"))
        assertNull(LarkPalette.hex("red"))
        assertEquals(LarkTint.Rgb(0xF54A45, 0xF76964), LarkPalette.text("red"))
        assertEquals(LarkTint.label, LarkPalette.text("grey-900"))
        assertEquals(LarkTint.secondary, LarkPalette.text("grey-600"))
        assertNull(LarkPalette.text("default"))
        assertEquals(LarkTint.blue, LarkPalette.hue(null))
        assertNull(LarkPalette.background("transparent"))
        assertEquals(0.08f, LarkPalette.background("blue-50")!!.alpha, 0.001f)
        val custom = LarkPalette.custom(buildJsonObject { put("lightMode", "#112233"); put("darkMode", "#445566") }) as LarkTint.Rgb
        assertEquals(0x112233L, custom.light)
        assertEquals(0x445566L, custom.dark)
    }

    @Test fun formatting() {
        assertEquals("0:19", LarkFormat.duration(19000.0))
        assertEquals("1:02:05", LarkFormat.duration(3_725_000.0))
        assertEquals("", LarkFormat.duration(null))
        assertEquals("246 KB", LarkFormat.fileSize("245760"))
        assertEquals("12 字节", LarkFormat.fileSize("12"))
        assertEquals("abc", LarkFormat.fileSize("abc"))
        assertEquals("a\nb&c", LarkFormat.stripHtml("<p>a</p><b>b</b>&amp;c"))
        assertEquals("[OK]", LarkFormat.labelFallback("Lark_Emoji_OK_0", null))
        assertEquals("[微笑]", LarkFormat.labelFallback("SMILE", "[微笑]"))
        assertEquals("10月30日 16:00", LarkFormat.fieldValue("2026-10-30T08:00:00.000Z", ZoneId.of("Asia/Shanghai")))
        assertEquals("plain", LarkFormat.fieldValue("plain"))
        assertNull(LarkFormat.safeUrl("javascript:alert(1)"))
        assertEquals("好的，下午", LarkFormat.plain("**好的**，下午"))
    }

    @Test fun markdownLines() {
        val lines = LarkMarkdown.lines("# 标题\n- 一 **粗**\n2. 二 `code`\n> 引用 [链接](https://example.com)\n\n结尾")
        assertEquals(1, lines[0].heading)
        assertEquals("•", lines[1].marker)
        assertTrue(lines[1].runs.any { it is LarkInline.Text && it.style.bold && it.text == "粗" })
        assertEquals("2.", lines[2].marker)
        assertTrue(lines[2].runs.any { it is LarkInline.Text && it.style.code })
        assertTrue(lines[3].quote)
        assertTrue(lines[3].runs.any { it is LarkInline.Text && it.style.link == "https://example.com" })
        assertTrue(lines[4].runs.isEmpty())
        assertEquals("结尾", texts(lines[5].runs).joinToString(""))
    }

    @Test fun syntaxTokens() {
        val t = LarkSyntax.tokens("val x = \"hi\" // note\nreturn 42")
        assertTrue(t.any { it.kind == LarkSyntax.Kind.KEYWORD && it.text == "val" })
        assertTrue(t.any { it.kind == LarkSyntax.Kind.STRING && it.text == "\"hi\"" })
        assertTrue(t.any { it.kind == LarkSyntax.Kind.COMMENT && it.text == "// note" })
        assertTrue(t.any { it.kind == LarkSyntax.Kind.NUMBER && it.text == "42" })
        assertEquals("val x = \"hi\" // note\nreturn 42", t.joinToString("") { it.text })
    }

    @Test fun momentsRuns() {
        val c = JsonCodec.decodeFromString(LarkMomentsContent.serializer(),
            """{"segments":[{"kind":"text","text":"好 "},{"kind":"emoji","key":"SMILE","text":"[微笑]"}]}""")
        val r = c.runs()
        assertEquals("好 ", (r[0] as LarkInline.Text).text)
        assertEquals("SMILE", (r[1] as LarkInline.Emoji).key)
        assertEquals(listOf(""), texts((null as LarkMomentsContent?).runs()))
    }
}
