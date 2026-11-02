package jarvis.android.lark

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * One element of a Feishu interactive card as lark-connector projects it (`CardNode`): a tag, text, style, children and the
 * per-tag extras (image, options, items, persons…). Kept as raw JSON (iOS `LarkCardNode`).
 */
class LarkCardNode(val raw: JsonObject) {
    val tag: String get() = raw["tag"].str ?: ""
    val text: String? get() = raw["text"].str
    val url: String? get() = raw["url"].str
    val children: List<LarkCardNode> by lazy { (raw["children"].arr ?: emptyList()).mapNotNull { of(it) } }
    val style: JsonElement? get() = raw["style"]
    fun style(k: String): JsonElement? = style[k]
    fun styleString(k: String): String? = style[k].str
    val disabled: Boolean get() = raw["disabled"].bool ?: false
    val token: String? get() = raw["token"].str
    val imageUrl: String? get() = raw["image"]["url"].str
    val imageWidth: Double get() = raw["image"]["width"].dbl ?: 0.0
    val imageHeight: Double get() = raw["image"]["height"].dbl ?: 0.0
    val actionId: String? get() = raw["actionId"].str
    val actionMethod: Int? get() = raw["actionMethod"].int
    fun node(k: String): LarkCardNode? = of(raw[k])

    /** All the text this node and its children show. */
    val plainText: String
        get() {
            val t = text
            if (!t.isNullOrEmpty() && children.isEmpty()) return t
            val inner = children.joinToString("") { it.plainText }
            return inner.ifEmpty { t ?: "" }
        }

    val textAttributes: Set<String> get() = (style("textAttributes").arr ?: emptyList()).mapNotNull { it.str }.toSet()

    val isInline: Boolean get() = tag in inlineTags
    val isIcon: Boolean get() = tag == "standard_icon" || tag == "custom_icon" || tag == "ud_icon"

    companion object {
        fun of(e: JsonElement?): LarkCardNode? = (e as? JsonObject)?.let { LarkCardNode(it) }

        val inlineTags = setOf("plain_text", "link", "at", "at_all", "person", "person_v1", "person_list", "code_span", "emoji", "br",
            "local_datetime", "markdown_emphasis", "markdown_font", "markdown_fallback", "text_tag", "number_tag",
            "standard_icon", "custom_icon", "ud_icon")
    }
}

/** Card style units (iOS `LarkCardStyle`). */
object LarkCardStyle {
    fun unit(v: JsonElement?): Float? {
        v ?: return null
        v.dbl?.let { return it.toFloat().coerceIn(0f, 64f) }
        v["value"].dbl?.let { return it.toFloat().coerceIn(0f, 64f) }
        return when (v["value"].str ?: v.str) {
            "small" -> 4f
            "medium", "default" -> 8f
            "large" -> 12f
            "extra_large" -> 16f
            else -> null
        }
    }

    /** (top, left, bottom, right) in dp. */
    fun insets(v: JsonElement?): FloatArray {
        val o = v as? JsonObject ?: return FloatArray(4)
        fun f(k: String) = (o[k].dbl ?: 0.0).toFloat().coerceIn(0f, 48f)
        return floatArrayOf(f("top"), f("left"), f("bottom"), f("right"))
    }

    fun textSize(s: String?): Float? = when (s) {
        "notation", "small" -> 12f
        "normal" -> 15f
        "large" -> 17f
        "heading", "heading-4" -> 18f
        "heading-3" -> 20f
        "heading-2" -> 22f
        "heading-1", "heading-0" -> 24f
        else -> null
    }

    fun spacing(n: LarkCardNode): Float? = unit(n.style("verticalSpacingValue") ?: n.style("horizontalSpacingValue"))

    /** How a `column_set` lays out its columns. */
    sealed interface ColumnSpec {
        data class Fixed(val dp: Float) : ColumnSpec
        data object Auto : ColumnSpec
        data class Weight(val w: Double) : ColumnSpec
    }

    fun columnSpec(c: LarkCardNode): ColumnSpec {
        val w = c.style("widthValue")
        if (w["type"].str == "pixels") w["value"].dbl?.let { return ColumnSpec.Fixed(minOf(it, 720.0).toFloat()) }
        if (w["type"].str == "builtin_column_width" && w["value"].str == "auto") return ColumnSpec.Auto
        if (c.styleString("width") == "auto") return ColumnSpec.Auto
        return ColumnSpec.Weight(maxOf(c.style("weight").dbl ?: w["value"].dbl ?: 1.0, 0.1))
    }

    /**
     * Column widths for one row (port of the CSS flex rules): fixed widths first, content-sized (`auto`) columns get
     * what they need up to a fair share (water-filled), weighted columns split the rest. [ideal] = each column's
     * content width (only asked for auto columns).
     */
    fun columnWidths(specs: List<ColumnSpec>, total: Float, spacing: Float, ideal: (Int) -> Float): FloatArray {
        val n = specs.size
        val w = FloatArray(n)
        if (n == 0) return w
        var avail = total - spacing * (n - 1)
        for (i in 0 until n) {
            val s = specs[i]
            if (s is ColumnSpec.Fixed) {
                w[i] = minOf(s.dp, maxOf(avail, 0f)); avail -= w[i]
            }
        }
        avail = maxOf(avail, 0f)
        val weighted = (0 until n).filter { specs[it] is ColumnSpec.Weight }
        val autos = (0 until n).filter { specs[it] is ColumnSpec.Auto }
        if (weighted.isEmpty() && autos.isEmpty()) return w
        var remaining = avail
        val open = autos.toMutableList()
        val ideals = autos.associateWith { ideal(it) }
        val shareCount = (autos.size + weighted.size).toFloat()
        while (open.isNotEmpty()) {
            val share = remaining / maxOf(open.size + weighted.size, 1)
            val fitting = open.filter { ideals.getValue(it) <= share }
            if (fitting.isEmpty()) {
                for (i in open) w[i] = if (weighted.isEmpty()) share else minOf(ideals.getValue(i), avail / shareCount)
                remaining -= open.sumOf { w[it].toDouble() }.toFloat()
                open.clear()
            } else {
                for (i in fitting) { w[i] = ideals.getValue(i); remaining -= w[i] }
                open.removeAll(fitting)
            }
        }
        val totalWeight = weighted.sumOf { (specs[it] as ColumnSpec.Weight).w }
        for (i in weighted) w[i] = maxOf(remaining, 0f) * ((specs[i] as ColumnSpec.Weight).w / maxOf(totalWeight, 0.1)).toFloat()
        return w
    }

    /** Narrow-screen column modes: at most [perRow] per row, fewer when a column would get under [minWidth]. */
    fun gridColumns(width: Float, perRow: Int, minWidth: Float, spacing: Float): Int =
        if (!width.isFinite()) perRow else maxOf(1, minOf(perRow, ((width + spacing) / (minWidth + spacing)).toInt()))

    /** Two short `div` fields share a row; a long one takes the whole row. */
    fun <T> fieldRows(items: List<Pair<Boolean, T>>): List<List<T>> {
        val out = ArrayList<List<T>>()
        var pending: T? = null
        var hasPending = false
        for ((short, f) in items) {
            if (short) {
                if (hasPending) { @Suppress("UNCHECKED_CAST") out += listOf(pending as T, f); hasPending = false; pending = null }
                else { pending = f; hasPending = true }
            } else {
                if (hasPending) { @Suppress("UNCHECKED_CAST") out += listOf(pending as T); hasPending = false; pending = null }
                out += listOf(f)
            }
        }
        @Suppress("UNCHECKED_CAST")
        if (hasPending) out += listOf(pending as T)
        return out
    }
}

/** UD icon tokens drawn with the closest icon (warning, info, success, time, people…), as a [LarkIcons] name + tint. */
object LarkCardIcons {
    private val table: List<Triple<String, String, String?>> = listOf(
        Triple("warning", "warning", "orange"), Triple("alert", "warning", "orange"),
        Triple("error", "error", "red"), Triple("close", "cancel", "red"), Triple("cancel", "cancel", "red"),
        Triple("info", "info", null), Triple("maybe", "help", null), Triple("help", "help", null), Triple("question", "help", null),
        Triple("succeed", "success", "green"), Triple("success", "success", "green"), Triple("done", "success", "green"),
        Triple("check", "success", "green"), Triple("yes", "success", "green"),
        Triple("time", "clock", null), Triple("clock", "clock", null), Triple("calendar", "calendar", null), Triple("alarm", "alarm", null),
        Triple("member", "group", null), Triple("group", "group", null), Triple("people", "person", null), Triple("user", "person", null),
        Triple("contacts", "person", null),
        Triple("link", "link", null), Triple("external", "external", null), Triple("doc", "doc", null), Triple("file", "file", null),
        Triple("folder", "folder", null),
        Triple("bell", "bell", null), Triple("notice", "bell", null), Triple("fire", "fire", "orange"), Triple("star", "star", "yellow"),
        Triple("like", "like", null),
        Triple("chat", "chat", null), Triple("message", "chat", null), Triple("mail", "mail", null), Triple("search", "search", null),
        Triple("setting", "settings", null), Triple("edit", "edit", null), Triple("delete", "delete", null), Triple("add", "add", null),
        Triple("refresh", "refresh", null),
        Triple("down", "down", null), Triple("up", "up", null), Triple("right", "right", null), Triple("left", "left", null),
        Triple("robot", "sparkles", null), Triple("ai", "sparkles", "purple"), Triple("lock", "lock", null), Triple("location", "location", null),
        Triple("chart", "chart", null), Triple("data", "chart", null), Triple("code", "code", null), Triple("bug", "bug", "red"),
        Triple("video", "video", null), Triple("phone", "phone", null), Triple("image", "image", null), Triple("tag", "tag", null),
        Triple("flag", "flag", null), Triple("pin", "pin", null),
    )

    fun symbol(token: String?): Pair<String, LarkTint?>? {
        val t = token?.lowercase()?.takeIf { it.isNotEmpty() } ?: return null
        for ((k, s, c) in table) if (t.contains(k)) return s to c?.let { LarkPalette.fixed(if (it == "yellow") "yellow" else it) }
        return null
    }
}

/** Card inline runs (iOS `LarkCardRuns`). */
object LarkCardRuns {
    fun style(n: LarkCardNode, inherited: LarkRunStyle = LarkRunStyle()): LarkRunStyle {
        var s = inherited
        val attrs = n.textAttributes
        if ("bold" in attrs) s = s.copy(bold = true)
        if ("italic" in attrs) s = s.copy(italic = true)
        if ("strikethrough" in attrs) s = s.copy(strike = true)
        if ("underline" in attrs) s = s.copy(underline = true)
        (LarkPalette.custom(n.style("customTextColor")) ?: LarkPalette.text(n.styleString("textColor") ?: n.styleString("color")))
            ?.let { s = s.copy(color = it) }
        LarkCardStyle.textSize(n.styleString("textSize"))?.let { s = s.copy(size = it) }
        return s
    }

    fun runs(n: LarkCardNode, inMarkdown: Boolean, inherited: LarkRunStyle = LarkRunStyle()): List<LarkInline> {
        val s = style(n, inherited)
        val kids = n.children
        fun inner(st: LarkRunStyle) = kids.flatMap { runs(it, inMarkdown, st) }
        return when (n.tag) {
            "plain_text" -> {
                val t = n.text ?: ""
                if (inMarkdown) LarkFormat.cardMarkdown(t, s) else listOf(LarkInline.Text(t, s))
            }
            "link" -> {
                val l = s.copy(link = LarkFormat.safeUrl(n.url))
                if (kids.isEmpty()) listOf(LarkInline.Text(n.text ?: n.url ?: "", l)) else inner(l)
            }
            "at", "person", "person_v1" -> {
                val name = n.text ?: ""
                listOf(LarkInline.Text(if (n.tag == "at" && !name.startsWith("@")) "@$name" else name, s.copy(color = LarkTint.blue)))
            }
            "at_all" -> listOf(LarkInline.Text(n.text ?: "@所有人", s.copy(color = LarkTint.blue)))
            "person_list" -> {
                val names = (n.raw["persons"].arr ?: emptyList()).mapNotNull { it["text"].str }
                listOf(LarkInline.Text(if (names.isEmpty()) n.text ?: "" else names.joinToString("、"), s.copy(color = LarkTint.blue)))
            }
            "code_span" -> listOf(LarkInline.Text(if (inMarkdown) LarkFormat.entities(n.text ?: "") else n.text ?: "", s.copy(code = true)))
            "emoji" -> {
                val key = n.token ?: n.text
                if (key.isNullOrEmpty()) emptyList() else listOf(LarkInline.Emoji(key, n.text ?: LarkFormat.labelFallback(key, null)))
            }
            "br" -> listOf(LarkInline.Text("\n", s))
            "local_datetime" -> listOf(LarkInline.Text(datetime(n.raw["milliseconds"].dbl, n.raw["formatType"].str) ?: n.text ?: "", s))
            "markdown_emphasis" -> {
                val e = s.copy(italic = true)
                if (kids.isEmpty()) listOf(LarkInline.Text(n.text ?: "", e)) else inner(e)
            }
            "markdown_font", "markdown_fallback" -> if (kids.isEmpty()) listOf(LarkInline.Text(n.text ?: "", s)) else inner(s)
            "text_tag" -> {
                val hue = LarkPalette.hue(n.styleString("color"))
                listOf(LarkInline.Text("\u2009" + n.plainText + "\u2009", s.copy(color = hue, background = hue.alpha(0.14f), size = 12f)))
            }
            "number_tag" -> listOf(LarkInline.Text("[${n.plainText}]", s.copy(color = LarkTint.blue, size = 11f, link = LarkFormat.safeUrl(n.url))))
            "standard_icon", "custom_icon", "ud_icon" -> {
                val sym = LarkCardIcons.symbol(n.token)
                when {
                    sym != null -> listOf(LarkInline.Symbol(sym.first, LarkPalette.text(n.styleString("color")) ?: sym.second))
                    n.imageUrl != null -> listOf(LarkInline.Image(n.imageUrl!!))
                    else -> emptyList()
                }
            }
            else -> if (kids.isEmpty()) listOf(LarkInline.Text(n.text ?: "", s)) else inner(s)
        }
    }

    /** `local_datetime` in the reader's own time zone. */
    fun datetime(ms: Double?, format: String?, zone: ZoneId = ZoneId.systemDefault()): String? {
        ms ?: return null
        val d = Instant.ofEpochMilli(ms.toLong()).atZone(zone)
        val p = when (format) {
            "date_num" -> "yyyy/MM/dd"
            "date_short" -> "M月d日"
            "date" -> "yyyy年M月d日"
            "time" -> "HH:mm"
            "time_sec" -> "HH:mm:ss"
            else -> "yyyy年M月d日 HH:mm"
        }
        return d.format(DateTimeFormatter.ofPattern(p))
    }
}
