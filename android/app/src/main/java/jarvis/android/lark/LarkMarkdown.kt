package jarvis.android.lark

/** Markdown text parts (bot messages): enough Markdown for a chat bubble — headings, lists, quotes, bold / italic / code / links. */
object LarkMarkdown {
    data class Line(val runs: List<LarkInline>, val heading: Int = 0, val marker: String? = null, val quote: Boolean = false, val indent: Int = 0)

    private val inline = Regex("""\*\*(.+?)\*\*|__(.+?)__|`([^`]+)`|\[([^\]]+)\]\(([^)\s]+)\)|(?<![\w*])\*(?!\s)(.+?)(?<!\s)\*(?!\w)|~~(.+?)~~""")
    private val headingRe = Regex("^(#{1,6})\\s+")
    private val ulRe = Regex("^[-*+]\\s+")
    private val olRe = Regex("^(\\d+)[.)]\\s+")

    fun lines(src: String): List<Line> {
        val out = ArrayList<Line>()
        for (raw in src.replace("\r\n", "\n").split('\n')) {
            val indent = raw.length - raw.trimStart().length
            var t = raw.trim()
            if (t.isEmpty()) {
                if (out.isNotEmpty() && out.last().runs.isNotEmpty()) out += Line(emptyList())
                continue
            }
            var heading = 0
            var marker: String? = null
            var quote = false
            if (t.startsWith(">")) { quote = true; t = t.removePrefix(">").trimStart() }
            headingRe.find(t)?.let { heading = it.groupValues[1].length; t = t.substring(it.range.last + 1) }
            if (heading == 0) {
                val ul = ulRe.find(t)
                val ol = olRe.find(t)
                when {
                    ul != null -> { marker = "•"; t = t.substring(ul.range.last + 1) }
                    ol != null -> { marker = ol.groupValues[1] + "."; t = t.substring(ol.range.last + 1) }
                }
            }
            out += Line(runs(t), heading, marker, quote, indent / 2)
        }
        while (out.isNotEmpty() && out.last().runs.isEmpty()) out.removeAt(out.size - 1)
        return out
    }

    fun runs(t: String, style: LarkRunStyle = LarkRunStyle()): List<LarkInline> {
        val out = ArrayList<LarkInline>()
        var last = 0
        for (m in inline.findAll(t)) {
            if (m.range.first > last) out += LarkInline.Text(t.substring(last, m.range.first), style)
            val g = m.groups
            when {
                g[1] != null -> out += runs(g[1]!!.value, style.copy(bold = true))
                g[2] != null -> out += runs(g[2]!!.value, style.copy(bold = true))
                g[3] != null -> out += LarkInline.Text(g[3]!!.value, style.copy(code = true))
                g[4] != null -> out += LarkInline.Text(g[4]!!.value, style.copy(link = LarkFormat.safeUrl(g[5]!!.value)))
                g[6] != null -> out += runs(g[6]!!.value, style.copy(italic = true))
                g[7] != null -> out += runs(g[7]!!.value, style.copy(strike = true))
            }
            last = m.range.last + 1
        }
        if (last < t.length) out += LarkInline.Text(t.substring(last), style)
        return out
    }
}
