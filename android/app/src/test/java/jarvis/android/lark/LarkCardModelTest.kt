package jarvis.android.lark

import jarvis.android.data.JsonCodec
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LarkCardModelTest {
    private fun node(json: String) = LarkCardNode(JsonCodec.parseToJsonElement(json) as JsonObject)

    @Test fun columnWidthsFixedAutoAndWeights() {
        val specs = listOf(LarkCardStyle.ColumnSpec.Fixed(100f), LarkCardStyle.ColumnSpec.Weight(1.0), LarkCardStyle.ColumnSpec.Weight(3.0))
        val w = LarkCardStyle.columnWidths(specs, 520f, 10f) { 0f }
        assertArrayEquals(floatArrayOf(100f, 100f, 300f), w, 0.01f)
        // An auto column takes what it needs when that fits its share; the weighted one gets the rest.
        val auto = LarkCardStyle.columnWidths(listOf(LarkCardStyle.ColumnSpec.Auto, LarkCardStyle.ColumnSpec.Weight(1.0)), 300f, 0f) { 80f }
        assertArrayEquals(floatArrayOf(80f, 220f), auto, 0.01f)
        // Autos alone split what is left when none fits.
        val wide = LarkCardStyle.columnWidths(listOf(LarkCardStyle.ColumnSpec.Auto, LarkCardStyle.ColumnSpec.Auto), 200f, 0f) { 500f }
        assertArrayEquals(floatArrayOf(100f, 100f), wide, 0.01f)
        assertEquals(0, LarkCardStyle.columnWidths(emptyList(), 100f, 0f) { 0f }.size)
    }

    @Test fun columnSpecsFromStyles() {
        assertEquals(LarkCardStyle.ColumnSpec.Fixed(120f), LarkCardStyle.columnSpec(node("""{"tag":"column","style":{"widthValue":{"type":"pixels","value":120}}}""")))
        assertEquals(LarkCardStyle.ColumnSpec.Auto, LarkCardStyle.columnSpec(node("""{"tag":"column","style":{"width":"auto"}}""")))
        assertEquals(LarkCardStyle.ColumnSpec.Weight(2.0), LarkCardStyle.columnSpec(node("""{"tag":"column","style":{"weight":2}}""")))
        assertEquals(LarkCardStyle.ColumnSpec.Weight(1.0), LarkCardStyle.columnSpec(node("""{"tag":"column"}""")))
    }

    @Test fun narrowScreenColumns() {
        assertEquals(2, LarkCardStyle.gridColumns(320f, 2, 64f, 8f))
        assertEquals(1, LarkCardStyle.gridColumns(100f, 3, 64f, 8f))
        assertEquals(3, LarkCardStyle.gridColumns(Float.POSITIVE_INFINITY, 3, 64f, 8f))
    }

    @Test fun shortFieldsShareARow() {
        val rows = LarkCardStyle.fieldRows(listOf(true to "a", true to "b", false to "c", true to "d"))
        assertEquals(listOf(listOf("a", "b"), listOf("c"), listOf("d")), rows)
    }

    @Test fun units() {
        assertEquals(8f, LarkCardStyle.unit(JsonCodec.parseToJsonElement("\"medium\""))!!, 0f)
        assertEquals(64f, LarkCardStyle.unit(JsonCodec.parseToJsonElement("100"))!!, 0f)
        assertEquals(12f, LarkCardStyle.unit(JsonCodec.parseToJsonElement("""{"value":"large"}"""))!!, 0f)
        assertNull(LarkCardStyle.unit(null))
        val pad = LarkCardStyle.insets(JsonCodec.parseToJsonElement("""{"top":4,"left":8,"bottom":100}"""))
        assertArrayEquals(floatArrayOf(4f, 8f, 48f, 0f), pad, 0f)
        assertEquals(18f, LarkCardStyle.textSize("heading")!!, 0f)
    }

    @Test fun cardRuns() {
        val n = node("""{"tag":"markdown","children":[{"tag":"plain_text","text":"hi <font color='red'>x</font>","style":{"textAttributes":["bold"]}},
            {"tag":"at","text":"李雷"},{"tag":"code_span","text":"a&lt;b"},{"tag":"text_tag","text":"新","style":{"color":"green"}},
            {"tag":"standard_icon","token":"warning_filled"},{"tag":"emoji","token":"OK"},{"tag":"br"}]}""")
        val runs = n.children.flatMap { LarkCardRuns.runs(it, inMarkdown = true) }
        val texts = runs.filterIsInstance<LarkInline.Text>()
        assertTrue(texts.first().style.bold)
        assertTrue(texts.any { it.text == "@李雷" && it.style.color == LarkTint.blue })
        assertTrue(texts.any { it.text == "a<b" && it.style.code })
        assertTrue(texts.any { it.text.contains("新") && it.style.size == 12f })
        assertTrue(runs.any { it is LarkInline.Symbol && it.name == "warning" })
        assertTrue(runs.any { it is LarkInline.Emoji && it.key == "OK" })
        assertEquals("\n", texts.last().text)
        assertEquals("hi x李雷", node("""{"tag":"div","children":[{"tag":"plain_text","text":"hi "},{"tag":"plain_text","text":"x"},{"tag":"at","text":"李雷"}]}""").plainText)
    }

    @Test fun iconTokens() {
        assertEquals("warning", LarkCardIcons.symbol("warning_filled")!!.first)
        assertEquals("success", LarkCardIcons.symbol("succeed_filled")!!.first)
        assertNull(LarkCardIcons.symbol("zzz"))
        assertNull(LarkCardIcons.symbol(null))
    }

    @Test fun localDatetime() {
        val zone = java.time.ZoneId.of("Asia/Shanghai")
        assertEquals("16:00", LarkCardRuns.datetime(1_793_347_200_000.0, "time", zone))
        assertEquals("2026/10/30", LarkCardRuns.datetime(1_793_347_200_000.0, "date_num", zone))
    }
}
