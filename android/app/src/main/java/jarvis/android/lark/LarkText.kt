package jarvis.android.lark

import kotlinx.serialization.json.JsonElement
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

// Inline content of Feishu messages and cards, resolved to colors / fonts only when drawn (ui/inbox/lark).
// Port of iOS LarkInline.swift / LarkMessageContent.swift (`LarkPartRenderer`) / LarkCard.swift (`LarkCardRuns`).

enum class TintRole { LABEL, SECONDARY, TERTIARY, BLUE, WHITE }

/** A color that still has to pick its light / dark value (Feishu's named palette, custom hex, or a semantic role). */
sealed interface LarkTint {
    val alpha: Float
    fun alpha(a: Float): LarkTint

    data class Rgb(val light: Long, val dark: Long, override val alpha: Float = 1f) : LarkTint {
        override fun alpha(a: Float) = copy(alpha = alpha * a)
    }

    data class Role(val role: TintRole, override val alpha: Float = 1f) : LarkTint {
        override fun alpha(a: Float) = copy(alpha = alpha * a)
    }

    companion object {
        val label: LarkTint = Role(TintRole.LABEL)
        val secondary: LarkTint = Role(TintRole.SECONDARY)
        val blue: LarkTint = Role(TintRole.BLUE)
    }
}

/** Feishu's named palette (card text colors, tags, header templates), light / dark (iOS `LarkColor`). */
object LarkPalette {
    private val named: Map<String, Pair<Long, Long>> = mapOf(
        "blue" to (0x3370FFL to 0x4C88FFL), "wathet" to (0x049FD7L to 0x26B5E8L), "turquoise" to (0x04B49CL to 0x23C4AEL),
        "green" to (0x2EA121L to 0x51BA43L), "lime" to (0x8FAC02L to 0xA3C00EL), "yellow" to (0xDC9B04L to 0xF5BF2DL),
        "orange" to (0xF77234L to 0xFF8F4DL), "red" to (0xF54A45L to 0xF76964L), "carmine" to (0xE0457EL to 0xF06A97L),
        "violet" to (0xC34ACFL to 0xD772E0L), "purple" to (0x7F3BF5L to 0x9F6FF8L), "indigo" to (0x4954E6L to 0x6B74F0L),
        "grey" to (0x8F959EL to 0x8C8F96L), "gray" to (0x8F959EL to 0x8C8F96L), "neutral" to (0x8F959EL to 0x8C8F96L),
    )

    private fun base(name: String) = name.split('-').firstOrNull() ?: ""

    /** A text / foreground color: `red`, `grey-950`, `blue-500`; null = inherit (`default`, unknown). */
    fun text(name: String?): LarkTint? {
        if (name.isNullOrEmpty() || name == "default") return null
        if (name == "white") return LarkTint.Role(TintRole.WHITE)
        if (name == "black") return LarkTint.label
        val parts = name.split('-')
        val b = parts[0]
        val shade = parts.getOrNull(1)?.toIntOrNull() ?: 500
        if (b == "grey" || b == "gray" || b == "neutral") {
            if (shade >= 800) return LarkTint.label
            if (shade >= 600) return LarkTint.secondary
        }
        val c = named[b] ?: return null
        return LarkTint.Rgb(c.first, c.second)
    }

    /** The template's base hue (card header / tag / summary accent); default the chrome blue. */
    fun hue(name: String?): LarkTint {
        val c = name?.let { named[base(it)] } ?: return LarkTint.blue
        return LarkTint.Rgb(c.first, c.second)
    }

    /** Feishu red / orange / … always from the palette (not the style's accent). */
    fun fixed(name: String): LarkTint = named[name]?.let { LarkTint.Rgb(it.first, it.second) } ?: LarkTint.blue

    /** A surface tint: `grey`, `blue-50`, `red-100`, `bg-white`; null = none. [cardBackground] stands for `bg-white`. */
    fun background(name: String?, cardBackground: LarkTint? = null): LarkTint? {
        if (name.isNullOrEmpty() || name == "default" || name == "transparent") return null
        if (name == "bg-white") return cardBackground ?: LarkTint.Rgb(0xFFFFFF, 0x1E1F23)
        val b = base(name)
        if (named[b] == null) return null
        val shade = name.split('-').getOrNull(1)?.toIntOrNull() ?: 100
        return hue(b).alpha(if (b == "grey" || b == "gray") 0.12f else if (shade <= 50) 0.08f else 0.13f)
    }

    /** `#rrggbb[aa]` / `#rgb[a]`, or `{lightMode, darkMode}`. */
    fun custom(v: JsonElement?): LarkTint? {
        v ?: return null
        v.str?.let { s -> return hex(s) }
        val light = hex(v["lightMode"].str) as? LarkTint.Rgb ?: return null
        val dark = hex(v["darkMode"].str) as? LarkTint.Rgb ?: light
        return LarkTint.Rgb(light.light, dark.light, light.alpha)
    }

    fun hex(s: String?): LarkTint? {
        var h = s?.trim() ?: return null
        if (!h.startsWith("#")) return null
        h = h.drop(1)
        if (h.length == 3 || h.length == 4) h = h.map { "$it$it" }.joinToString("")
        if (h.length != 6 && h.length != 8) return null
        val v = h.toLongOrNull(16) ?: return null
        val rgb = if (h.length == 6) v else v shr 8
        val a = if (h.length == 6) 1f else (v and 0xff).toFloat() / 255f
        return LarkTint.Rgb(rgb, rgb, a)
    }
}

/** How a run of text looks; null fields inherit. Sizes are in sp. */
data class LarkRunStyle(
    val bold: Boolean = false,
    val italic: Boolean = false,
    val underline: Boolean = false,
    val dotted: Boolean = false,
    val strike: Boolean = false,
    val code: Boolean = false,
    val color: LarkTint? = null,
    val background: LarkTint? = null,
    val size: Float? = null,
    val link: String? = null,
)

/** One piece of inline content: styled text, a Feishu emoji, an icon glyph ([LarkIcons] name), or a small image. */
sealed interface LarkInline {
    data class Text(val text: String, val style: LarkRunStyle = LarkRunStyle()) : LarkInline
    data class Emoji(val key: String, val fallback: String) : LarkInline
    data class Symbol(val name: String, val tint: LarkTint? = null) : LarkInline
    data class Image(val url: String) : LarkInline
}

/** Base text appearance for a block of inline runs. */
data class LarkTextBase(val size: Float = 16f, val bold: Boolean = false, val color: LarkTint? = null)

object LarkFormat {
    fun safeUrl(s: String?): String? {
        s ?: return null
        val scheme = s.substringBefore(':', "").lowercase()
        return if (scheme == "http" || scheme == "https" || scheme == "mailto") s else null
    }

    /** `[微笑]`-style text for an emoji whose picture is not available. */
    fun labelFallback(key: String, text: String?): String {
        if (!text.isNullOrEmpty()) return text
        var k = key.removePrefix("Lark_Emoji_")
        val us = k.lastIndexOf('_')
        if (us > 0) k = k.substring(0, us)
        return "[$k]"
    }

    fun fileSize(raw: String?): String {
        val n = raw?.toLongOrNull() ?: return raw ?: ""
        if (n < 1000) return "$n 字节"
        val units = listOf("KB", "MB", "GB", "TB")
        var v = n.toDouble()
        var i = -1
        while (v >= 1000 && i < units.size - 1) { v /= 1000; i++ }
        val s = if (v >= 100) "%.0f".format(v) else if (v >= 10) "%.1f".format(v) else "%.1f".format(v)
        return "${s.removeSuffix(".0")} ${units[i]}"
    }

    fun duration(ms: Double?): String {
        if (ms == null || ms <= 0) return ""
        val s = Math.round(ms / 1000).toInt()
        return if (s >= 3600) "%d:%02d:%02d".format(s / 3600, s / 60 % 60, s % 60) else "%d:%02d".format(s / 60, s % 60)
    }

    private val fieldTime = DateTimeFormatter.ofPattern("M月d日 HH:mm")

    /** ISO timestamps in summary-card fields read as local times; anything else is shown as is. */
    fun fieldValue(s: String, zone: ZoneId = ZoneId.systemDefault()): String {
        if (s.length < 20 || !s.contains('T')) return s
        val t = parseLarkTime(s) ?: return s
        return t.atZone(zone).format(fieldTime)
    }

    private val entityTable = listOf("&nbsp;" to "\u00A0", "&ensp;" to "\u2002", "&emsp;" to "\u2003", "&lt;" to "<", "&gt;" to ">",
        "&quot;" to "\"", "&#39;" to "'", "&apos;" to "'", "&sim;" to "~", "&amp;" to "&")
    private val numericEntity = Regex("&#(x?)([0-9a-fA-F]{1,7});")

    /** Decodes the HTML character references Feishu leaves in card markdown text. */
    fun entities(s: String): String {
        if (!s.contains('&')) return s
        var out = s
        for ((k, v) in entityTable) out = out.replace(k, v, ignoreCase = true)
        if (!out.contains("&#")) return out
        return numericEntity.replace(out) { m ->
            val hex = m.groupValues[1] == "x"
            val n = m.groupValues[2].toIntOrNull(if (hex) 16 else 10)
            if (n != null && Character.isValidCodePoint(n)) String(Character.toChars(n)) else m.value
        }
    }

    private val markup = Regex("""<br\s*/?>|<a\s([^<>]*)>([^<]*)</a\s*>|<font\s([^<>]*)>([^<]*)</font\s*>""", RegexOption.IGNORE_CASE)

    private fun attr(attrs: String, name: String): String? {
        val r = Regex("""(?:^|\s)$name\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))""", RegexOption.IGNORE_CASE)
        val m = r.find(attrs) ?: return null
        return (1..3).firstNotNullOfOrNull { m.groups[it]?.value }
    }

    /** Splits card markdown text into runs: `<br>`, `<a href>`, `<font color>` (the markup Feishu leaves in place). */
    fun cardMarkdown(text: String, style: LarkRunStyle = LarkRunStyle()): List<LarkInline> {
        if (!text.contains('<')) return listOf(LarkInline.Text(entities(text), style))
        val out = ArrayList<LarkInline>()
        var last = 0
        for (m in markup.findAll(text)) {
            if (m.range.first > last) out += LarkInline.Text(entities(text.substring(last, m.range.first)), style)
            when {
                m.groups[1] != null -> {
                    val href = attr(m.groupValues[1], "href") ?: ""
                    val link = if (href.startsWith("http://") || href.startsWith("https://")) href else null
                    out += LarkInline.Text(entities(m.groupValues[2]), if (link != null) style.copy(link = link) else style)
                }
                m.groups[3] != null -> {
                    val color = attr(m.groupValues[3], "color") ?: ""
                    out += LarkInline.Text(entities(m.groupValues[4]), style.copy(color = LarkPalette.text(color) ?: LarkPalette.hex(color) ?: style.color))
                }
                else -> out += LarkInline.Text("\n", style)
            }
            last = m.range.last + 1
        }
        if (last < text.length) out += LarkInline.Text(entities(text.substring(last)), style)
        return out
    }

    /** Summary-card descriptions: drop tags, keep line breaks. */
    fun stripHtml(s: String): String {
        if (!s.contains('<')) return entities(s)
        val noTags = s.replace(Regex("""<br\s*/?>|</p>|</div>"""), "\n").replace(Regex("<[^>]+>"), "")
        return entities(noTags).trim()
    }

    /** One line of plain text from (possibly Markdown) text: reply quotes, forward previews. */
    fun plain(s: String): String = s.replace(Regex("""[*_`~#>]+"""), "").replace(Regex("""\s+"""), " ").trim()

    /** A removed / comment time (ms since epoch). */
    fun momentsTime(ms: Double?, zone: ZoneId = ZoneId.systemDefault()): String =
        ms?.let { LarkChatRules.dayLabel(Instant.ofEpochMilli(it.toLong()), zone = zone) } ?: ""
}

/** How message parts become inline runs (iOS `LarkPartRenderer`). */
object LarkPartRuns {
    fun isInline(p: LarkPart): Boolean = when (p.kind) {
        "text" -> !p.metaBool("markdown") && !p.metaBool("system")
        "link" -> LarkMoments.postId(p.url) == null
        "at", "docs", "table" -> true
        "emotion" -> p.url == null
        "interactive" -> p.meta("richTag").int != 32
        else -> false
    }

    fun marks(p: LarkPart): LarkRunStyle {
        val marks = (p.meta("marks").arr ?: emptyList()).mapNotNull { it.str }.toSet()
        val legacy = p.meta("style").int
        var s = LarkRunStyle(
            bold = "bold" in marks || legacy == 7,
            italic = "italic" in marks || legacy == 8,
            underline = "underline" in marks || legacy == 9,
            strike = "strikethrough" in marks,
            code = p.metaBool("inlineCode"),
        )
        when (p.metaString("diff")) {
            "added" -> s = s.copy(background = LarkPalette.fixed("green").alpha(0.15f))
            "removed" -> s = s.copy(strike = true, color = LarkTint.secondary)
        }
        return s
    }

    fun runs(p: LarkPart, structured: Boolean): List<LarkInline> {
        val style = marks(p)
        return when (p.kind) {
            "text" -> if (structured && p.metaBool("emptyLine")) listOf(LarkInline.Text("\n", style))
                else lingoRuns(p.text ?: "", p.meta("lingo").arr ?: emptyList(), style)
            "at" -> listOf(LarkInline.Text(p.text ?: "@", style.copy(color = LarkTint.blue)))
            "link" -> listOf(LarkInline.Text(p.text ?: p.url ?: "", style.copy(link = LarkFormat.safeUrl(p.url))))
            "docs" -> listOf(LarkInline.Symbol("doc", LarkTint.blue), LarkInline.Text(" " + (p.text ?: "文档"), style.copy(link = LarkFormat.safeUrl(p.url))))
            "emotion" -> {
                val key = p.refId ?: p.text ?: ""
                listOf(LarkInline.Emoji(key, LarkFormat.labelFallback(key, p.text)))
            }
            else -> listOf(LarkInline.Text(p.text ?: "", style))
        }
    }

    /** Enterprise glossary terms (`lingo`, UTF-16 offsets) get a dotted underline, as in Feishu. */
    fun lingoRuns(text: String, lingo: List<JsonElement>, style: LarkRunStyle): List<LarkInline> {
        val ranges = lingo.mapNotNull { l ->
            val a = l["start"].int ?: return@mapNotNull null
            val b = l["end"].int ?: return@mapNotNull null
            if (a >= 0 && b > a && b <= text.length) a until b else null
        }.sortedBy { it.first }
        if (ranges.isEmpty()) return listOf(LarkInline.Text(text, style))
        val out = ArrayList<LarkInline>()
        var last = 0
        val term = style.copy(dotted = true)
        for (r in ranges) {
            if (r.first < last) continue
            if (r.first > last) out += LarkInline.Text(text.substring(last, r.first), style)
            out += LarkInline.Text(text.substring(r.first, r.last + 1), term)
            last = r.last + 1
        }
        if (last < text.length) out += LarkInline.Text(text.substring(last), style)
        return out
    }

    /** Group events ("刘森 创建了话题群"): the people named in the template stand out. */
    fun systemRuns(part: LarkPart): List<LarkInline> {
        val template = part.metaString("systemTemplate")
        if (template == null || !template.contains('{')) return listOf(LarkInline.Text(part.text ?: ""))
        val holders = HashMap<String, String>()
        for (h in part.meta("systemHolders").arr ?: emptyList()) {
            h["key"].str?.let { holders[it] = h["text"].str ?: "" }
        }
        val out = ArrayList<LarkInline>()
        var rest: String = template
        val strong = LarkRunStyle(color = LarkTint.label.alpha(0.75f))
        while (true) {
            val open = rest.indexOf('{')
            val close = if (open >= 0) rest.indexOf('}', open) else -1
            if (open < 0 || close < 0) break
            if (open > 0) out += LarkInline.Text(rest.substring(0, open))
            out += LarkInline.Text(holders[rest.substring(open + 1, close)] ?: "", strong)
            rest = rest.substring(close + 1)
        }
        if (rest.isNotEmpty()) out += LarkInline.Text(rest)
        return out
    }

    /**
     * Inline leaves of an unstructured message, with the list bullets and line breaks lark-connector marks with
     * `blockStart` / `blockEnd`; trailing newlines are dropped.
     */
    fun flowRuns(parts: List<LarkPart>, structured: Boolean): List<LarkInline> {
        val runs = ArrayList<LarkInline>()
        for (p in parts) {
            if (!structured && p.metaBool("blockStart") && p.metaString("block") == "list-item") runs += LarkInline.Text("•  ")
            runs += runs(p, structured)
            if (!structured && p.metaBool("blockEnd")) runs += LarkInline.Text("\n")
        }
        while ((runs.lastOrNull() as? LarkInline.Text)?.text == "\n") runs.removeAt(runs.size - 1)
        return runs
    }
}

/** removed post / comment text: plain runs and Feishu emoji. */
fun LarkMomentsContent?.runs(): List<LarkInline> {
    val segs = this?.segments
    if (segs.isNullOrEmpty()) return listOf(LarkInline.Text(this?.text ?: ""))
    return segs.map { s ->
        val key = s.key ?: s.emojiKey
        if (s.kind == "emoji" && key != null) LarkInline.Emoji(key, s.text ?: "[$key]") else LarkInline.Text(s.text ?: "")
    }
}
