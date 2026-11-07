package persona.feature.settings

import persona.core.contracts.api.ExtensionDiffHunk
import persona.core.contracts.api.ExtensionView
import persona.core.contracts.api.ExtensionViewItem

// UI version 5's code review (docs/extensions/extensions.md "UI version 5", #3136) without the looks, as the web's lib/code-review.ts:
// which words of a changed line changed, a change that is only whitespace drawn as unchanged, generated files and
// lockfiles, the lines hidden between hunks and those shown of them, an id for a viewed mark kept on the device,
// ```suggestion blocks in a comment, a range of lines picked for a comment, and an action's components put in place.
// Drawn by ExtensionCodeViews.kt.

/** Characters [start, end) of a line's text. */
internal data class CharSpan(val start: Int, val end: Int)

/** A run of text to draw: its highlighting, and whether it is a word that changed. */
internal data class Piece(val text: String, val kind: TokenKind, val changed: Boolean)

/** Why a diff is folded at first. */
internal enum class FoldReason { Generated, Big, Collapsed, Viewed }

/** The lines hidden before hunk [at] (at = the hunks' count: after the last), by new numbers [from]..[to] (null: to the end). */
internal data class Gap(val at: Int, val from: Int, val to: Int?, val delta: Int)

/** The lines shown of a gap: [top] from its start down, [bottom] from its end up. */
internal data class Shown(val top: Int = 0, val bottom: Int = 0)

/** Lines asked for: [from]..[to] (null: to the end). */
internal data class Need(val from: Int, val to: Int?)

/** Lines picked for a comment on one side: [line], from [startLine] when a range. */
internal data class LinePick(val side: String, val line: Int, val startLine: Int? = null) {
    fun has(side: String, n: Int?): Boolean = n != null && side == this.side && n in (startLine ?: line)..line
}

/** A comment's body: text, or a suggestion's lines. */
internal sealed interface BodyPart {
    data class Text(val text: String) : BodyPart
    data class Suggestion(val lines: List<String>) : BodyPart
}

/** Lines of one side of a file, as they came (any part of it), and how many it has once known. */
internal class LineCache {
    private val lines = HashMap<Int, String>()
    var total: Int? = null
        private set

    fun put(start: Int, got: List<String>, total: Int?) {
        got.forEachIndexed { i, l -> lines[start + i] = l }
        if (total != null) this.total = total
        else if (got.isEmpty() && this.total == null) this.total = start - 1 // asked past the end: there is none
    }

    operator fun get(n: Int): String? = lines[n]

    /** Every line from..to came (to null: to the end, known). */
    fun has(from: Int, to: Int?): Boolean {
        val end = to ?: total ?: return false
        return (from..end).all { it in lines }
    }
}

internal object ExtensionCodeReview {
    private val Word = Regex("[\\p{L}\\p{N}_]+|\\s+|[^\\s\\p{L}\\p{N}_]")
    private const val WordsMost = 500
    private const val CharsMost = 1000
    private const val PairsMost = 1_000_000
    private const val AlikeLeast = 0.4

    /** A line as words: runs of letters, digits and _, runs of whitespace, and each other character on its own. */
    fun words(s: String): List<String> = Word.findAll(s).map { it.value }.toList()

    /**
     * The spans that changed between an old line and the new one it became, word by word (the longest common sequence
     * of words); null when the lines are too long or too unlike for it to help.
     */
    fun wordDiff(a: String, b: String): Pair<List<CharSpan>, List<CharSpan>>? {
        if (a == b) return emptyList<CharSpan>() to emptyList()
        if (a.length > CharsMost || b.length > CharsMost) return null
        val x = words(a)
        val y = words(b)
        if (x.size > WordsMost || y.size > WordsMost) return null
        val n = x.size
        val m = y.size
        val lcs = Array(n + 1) { IntArray(m + 1) }
        for (i in n - 1 downTo 0) for (j in m - 1 downTo 0) lcs[i][j] = if (x[i] == y[j]) lcs[i + 1][j + 1] + 1 else maxOf(lcs[i + 1][j], lcs[i][j + 1])
        val keptX = BooleanArray(n)
        val keptY = BooleanArray(m)
        var common = 0
        var i = 0
        var j = 0
        while (i < n && j < m) {
            when {
                x[i] == y[j] -> { keptX[i] = true; keptY[j] = true; common += x[i].length; i++; j++ }
                lcs[i + 1][j] >= lcs[i][j + 1] -> i++
                else -> j++
            }
        }
        // alike enough as a whole, or one of them nearly all in the other (words put in, or taken out)
        if (2.0 * common / (a.length + b.length) < AlikeLeast && common.toDouble() / minOf(a.length, b.length) < 0.7) return null
        return spansOf(x, keptX) to spansOf(y, keptY)
    }

    private fun spansOf(ws: List<String>, kept: BooleanArray): List<CharSpan> {
        val out = mutableListOf<CharSpan>()
        var at = 0
        ws.forEachIndexed { i, w ->
            if (!kept[i]) {
                val last = out.lastOrNull()
                if (last != null && last.end == at) out[out.lastIndex] = last.copy(end = at + w.length) else out += CharSpan(at, at + w.length)
            } else if (w.isBlank() && out.isNotEmpty() && out.last().end == at && i + 1 < ws.size && !kept[i + 1]) {
                out[out.lastIndex] = out.last().copy(end = at + w.length) // a space between two changed words: one span
            }
            at += w.length
        }
        return out
    }

    /** The changed spans of each row of a hunk (by its index): a run of deletions paired line by line with the additions after it. */
    fun wordSpans(rows: List<DiffRow>): Map<Int, List<CharSpan>> {
        val out = HashMap<Int, List<CharSpan>>()
        var i = 0
        while (i < rows.size) {
            if (rows[i].kind != DiffKind.Del) { i++; continue }
            val dels = mutableListOf<Int>()
            val adds = mutableListOf<Int>()
            while (i < rows.size && rows[i].kind == DiffKind.Del) dels += i++
            while (i < rows.size && rows[i].kind == DiffKind.Add) adds += i++
            for (k in 0 until minOf(dels.size, adds.size)) {
                val (o, nw) = wordDiff(rows[dels[k]].text, rows[adds[k]].text) ?: continue
                if (o.isNotEmpty()) out[dels[k]] = o
                if (nw.isNotEmpty()) out[adds[k]] = nw
            }
        }
        return out
    }

    /** Highlighted tokens (or the plain text) cut where the changed spans begin and end. */
    fun overlay(tokens: List<CodeToken>?, text: String, spans: List<CharSpan>?): List<Piece> {
        val toks = if (tokens.isNullOrEmpty()) listOf(CodeToken(text, TokenKind.Plain)) else tokens
        if (spans.isNullOrEmpty()) return toks.map { Piece(it.text, it.kind, false) }
        val out = mutableListOf<Piece>()
        var at = 0
        var s = 0
        for (tk in toks) {
            var from = 0
            while (from < tk.text.length) {
                while (s < spans.size && spans[s].end <= at + from) s++
                val span = spans.getOrNull(s)
                val inside = span != null && span.start <= at + from
                val stop = if (span == null) tk.text.length else (if (inside) span.end else span.start) - at
                val to = minOf(tk.text.length, maxOf(stop, from + 1))
                out += Piece(tk.text.substring(from, to), tk.kind, inside)
                from = to
            }
            at += tk.text.length
        }
        return out
    }

    /**
     * A hunk's rows with changes that are only whitespace drawn as unchanged: in each run of deletions and additions,
     * the lines that are the same but for whitespace (in order) become one unchanged row with both numbers.
     */
    fun ignoreWhitespace(rows: List<DiffRow>): List<DiffRow> {
        val out = mutableListOf<DiffRow>()
        fun squeeze(s: String) = s.filterNot(Char::isWhitespace)
        var i = 0
        while (i < rows.size) {
            if (rows[i].kind != DiffKind.Del && rows[i].kind != DiffKind.Add) { out += rows[i++]; continue }
            val dels = mutableListOf<DiffRow>()
            val adds = mutableListOf<DiffRow>()
            while (i < rows.size && rows[i].kind == DiffKind.Del) dels += rows[i++]
            while (i < rows.size && rows[i].kind == DiffKind.Add) adds += rows[i++]
            val x = dels.map { squeeze(it.text) }
            val y = adds.map { squeeze(it.text) }
            val delFirst = deletionFirst(x, y)
            var a = 0
            var b = 0
            val pendD = mutableListOf<DiffRow>()
            val pendA = mutableListOf<DiffRow>()
            fun flush() { out += pendD; out += pendA; pendD.clear(); pendA.clear() }
            while (a < x.size || b < y.size) {
                when {
                    a < x.size && b < y.size && x[a] == y[b] -> { flush(); out += DiffRow(DiffKind.Context, dels[a].old, adds[b].new, adds[b].text); a++; b++ }
                    b >= y.size || (a < x.size && delFirst(a, b)) -> pendD += dels[a++]
                    else -> pendA += adds[b++]
                }
            }
            flush()
        }
        return out
    }

    /**
     * Walking deleted lines [x] and added lines [y] forward, whether deletion a goes before addition b where they differ:
     * by the longest common sequence, or for a run with too many pairs for its table (a diff may have 20,000 lines) by
     * which of the two comes again sooner on the other side.
     */
    private fun deletionFirst(x: List<String>, y: List<String>): (Int, Int) -> Boolean {
        if (x.size.toLong() * y.size <= PairsMost) {
            val lcs = Array(x.size + 1) { IntArray(y.size + 1) }
            for (a in x.size - 1 downTo 0) for (b in y.size - 1 downTo 0) lcs[a][b] = if (x[a] == y[b]) lcs[a + 1][b + 1] + 1 else maxOf(lcs[a + 1][b], lcs[a][b + 1])
            return { a, b -> lcs[a + 1][b] >= lcs[a][b + 1] }
        }
        val inX = HashMap<String, ArrayDeque<Int>>()
        val inY = HashMap<String, ArrayDeque<Int>>()
        x.forEachIndexed { i, s -> inX.getOrPut(s) { ArrayDeque() }.addLast(i) }
        y.forEachIndexed { j, s -> inY.getOrPut(s) { ArrayDeque() }.addLast(j) }
        // how far on from [from] a line comes next (Int.MAX_VALUE: never); the walk only goes forward, so places behind go
        fun HashMap<String, ArrayDeque<Int>>.ahead(s: String, from: Int): Int {
            val q = get(s) ?: return Int.MAX_VALUE
            while (q.isNotEmpty() && q.first() < from) q.removeFirst()
            return q.firstOrNull()?.minus(from) ?: Int.MAX_VALUE
        }
        return { a, b -> inX.ahead(y[b], a) <= inY.ahead(x[a], b) }
    }

    /** Whether a diff is all whitespace once whitespace is ignored. */
    fun onlyWhitespace(hunks: List<ExtensionDiffHunk>): Boolean =
        hunks.isNotEmpty() && hunks.all { h -> ignoreWhitespace(ExtensionCodeText.rows(h)).all { it.kind == DiffKind.Context || it.kind == DiffKind.Note } }

    private val Generated = listOf(
        Regex("(^|/)(package-lock\\.json|npm-shrinkwrap\\.json|yarn\\.lock|pnpm-lock\\.yaml|bun\\.lockb?|Cargo\\.lock|Gemfile\\.lock|composer\\.lock|poetry\\.lock|Pipfile\\.lock|uv\\.lock|go\\.sum|go\\.work\\.sum|Podfile\\.lock|Package\\.resolved|gradle\\.lockfile|flake\\.lock|mix\\.lock|pubspec\\.lock|packages\\.lock\\.json)$", RegexOption.IGNORE_CASE),
        Regex("\\.(min\\.js|min\\.css|js\\.map|css\\.map|pb\\.go|pb\\.gw\\.go|pb\\.ts|pb\\.swift|pb\\.h|pb\\.cc|g\\.dart|freezed\\.dart)$", RegexOption.IGNORE_CASE),
        Regex("(_gen\\.go|_generated\\.go|\\.generated\\.[a-z]+|_pb2\\.py|_pb2_grpc\\.py|\\.designer\\.cs)$", RegexOption.IGNORE_CASE),
        Regex("(^|/)(dist|vendor|node_modules|__generated__|__snapshots__)/"),
    )

    /** A lockfile, a generated or a vendored file, by its path (or as the extension says). */
    fun isGenerated(path: String, said: Boolean? = null): Boolean = said == true || Generated.any { it.containsMatchIn(path) }

    /** A file with more changed lines than this is folded at first. */
    const val BigDiff = 400

    fun foldedAtFirst(item: ExtensionViewItem, viewed: Boolean): FoldReason? = when {
        isGenerated(item.path.orEmpty(), item.generated) -> FoldReason.Generated
        (item.additions ?: 0) + (item.deletions ?: 0) > BigDiff -> FoldReason.Big
        item.collapsed == true -> FoldReason.Collapsed
        viewed -> FoldReason.Viewed
        else -> null
    }

    private fun newCount(h: ExtensionDiffHunk) = h.lines.count { !it.startsWith("-") && !it.startsWith("\\") }
    private fun oldCount(h: ExtensionDiffHunk) = h.lines.count { !it.startsWith("+") && !it.startsWith("\\") }

    /** Every gap of a diff: before each hunk, and after the last (to [total] when known). None for a file that is gone. */
    fun gaps(hunks: List<ExtensionDiffHunk>, total: Int? = null): List<Gap> {
        val out = mutableListOf<Gap>()
        var from = 1
        var delta = 0
        for (i in 0..hunks.size) {
            val h = hunks.getOrNull(i)
            if (h != null) {
                if (h.newStart == 0) return emptyList()
                out += Gap(i, from, h.newStart - 1, h.oldStart - h.newStart)
                from = h.newStart + newCount(h)
                delta = h.oldStart + oldCount(h) - from
            } else out += Gap(i, from, total, delta)
        }
        return out
    }

    /** Lines a gap hides (Int.MAX_VALUE when its end is not known yet). */
    fun gapSize(g: Gap): Int = g.to?.let { maxOf(0, it - g.from + 1) } ?: Int.MAX_VALUE

    const val Step = 20

    /** Showing more of a gap: down (below the hunk before), up (above the one after), or all; and the lines it needs. */
    fun reveal(g: Gap, shown: Shown, direction: String): Pair<Shown, Need> {
        val size = gapSize(g)
        if (direction == "all") return Shown(size, 0) to Need(g.from, g.to)
        if (direction == "down" || g.to == null) {
            val top = minOf(size - shown.bottom, shown.top + Step)
            return shown.copy(top = top) to Need(g.from + shown.top, g.from + top - 1)
        }
        val bottom = minOf(size - shown.top, shown.bottom + Step)
        return shown.copy(bottom = bottom) to Need(g.to - bottom + 1, g.to - shown.bottom)
    }

    /** The rows a gap shows from the cache (unchanged lines with both numbers), and how many still hidden. */
    fun gapRows(g: Gap, shown: Shown, cache: LineCache?): Triple<List<DiffRow>, List<DiffRow>, Int> {
        val to = g.to ?: cache?.total
        val size = to?.let { maxOf(0, it - g.from + 1) } ?: Int.MAX_VALUE
        fun row(n: Int) = cache?.get(n)?.let { DiffRow(DiffKind.Context, n + g.delta, n, it) }
        val top = mutableListOf<DiffRow>()
        var n = g.from
        while (n < g.from + minOf(shown.top, size)) { top += row(n) ?: break; n++ }
        val bottom = mutableListOf<DiffRow>()
        if (to != null) for (k in maxOf(g.from + top.size, to - shown.bottom + 1)..to) row(k)?.let { bottom += it }
        val hidden = if (size == Int.MAX_VALUE) Int.MAX_VALUE else maxOf(0, size - top.size - bottom.size)
        return Triple(top, bottom, hidden)
    }

    /** What a file's change is, for a viewed mark kept on the device: its revision, else a digest of its lines. */
    fun changeId(path: String, revision: String?, hunks: List<ExtensionDiffHunk>?): String {
        if (!revision.isNullOrEmpty()) return "$path@$revision"
        var h = 0x811c9dc5.toInt()
        hunks.orEmpty().forEach { hk -> hk.lines.forEach { l -> l.forEach { c -> h = (h xor c.code) * 0x01000193 } } }
        return "$path#${Integer.toUnsignedString(h, 36)}"
    }

    private val SuggestionBlock = Regex("^```suggestion[^\\S\\n]*\\n([\\s\\S]*?)^```[^\\S\\n]*$", RegexOption.MULTILINE)

    /** A comment's body cut at its ```suggestion blocks. */
    fun splitSuggestions(body: String): List<BodyPart> {
        val out = mutableListOf<BodyPart>()
        var at = 0
        for (m in SuggestionBlock.findAll(body)) {
            val before = body.substring(at, m.range.first)
            if (before.isNotBlank()) out += BodyPart.Text(before)
            val text = m.groupValues[1].removeSuffix("\n")
            out += BodyPart.Suggestion(if (text.isEmpty()) emptyList() else text.split('\n'))
            at = m.range.last + 1
        }
        val rest = body.substring(at)
        if (rest.isNotBlank() || out.isEmpty()) out += BodyPart.Text(rest)
        return out
    }

    /** The block "Suggest" inserts: the lines picked, to edit into what they should be. */
    fun suggestionBlock(lines: List<String>): String = "```suggestion\n" + lines.joinToString("\n") + "\n```\n"

    /** The lines from [anchor] to [to] on one side; a spot on the other side (or none) starts afresh. */
    fun pickRange(anchor: LinePick?, to: LinePick): LinePick =
        if (anchor == null || anchor.side != to.side || anchor.line == to.line) LinePick(to.side, to.line)
        else LinePick(to.side, maxOf(anchor.line, to.line), minOf(anchor.line, to.line))

    /** A view with an action's components (UI version 5's items) in place of those with the same key. */
    fun patchView(view: ExtensionView, items: List<ExtensionViewItem>): ExtensionView {
        val by = items.filter { !it.key.isNullOrEmpty() }.associateBy { it.key!! }
        if (by.isEmpty()) return view
        var hit = false
        val sections = view.sections.map { s ->
            if (s.items.none { it.key in by }) s else { hit = true; s.copy(items = s.items.map { by[it.key] ?: it }) }
        }
        return if (hit) view.copy(sections = sections) else view
    }

    /** A view drawn again for the same page, keeping the diffs loaded since that it gives without their lines again. */
    fun keepLoaded(before: ExtensionView?, next: ExtensionView): ExtensionView {
        before ?: return next
        val loaded = before.sections.flatMap { it.items }.filter { it.type == "diff" && !it.key.isNullOrEmpty() && !it.hunks.isNullOrEmpty() }.associateBy { it.key!! }
        if (loaded.isEmpty()) return next
        val keep = next.sections.flatMap { it.items }.filter { it.type == "diff" && it.key in loaded && it.hunks.isNullOrEmpty() && it.load != null }
        return if (keep.isEmpty()) next else patchView(next, keep.map { loaded.getValue(it.key!!) })
    }

    /** A line link from a code's line_url: {start} and {end} filled in, https only. */
    fun lineLink(template: String?, start: Int, end: Int = start): String? {
        val url = template?.replace("{start}", "$start")?.replace("{end}", "$end") ?: return null
        return url.takeIf { ExtensionViewText.isHttps(it) }
    }
}
