package persona.feature.files

import persona.core.contracts.api.SessionGitFile

/**
 * Comparing two versions of a changed file in the Git page (iOS `CompareView` / `DocumentText`, web `lib/compare.ts`,
 * #1579 / #1452): pictures are shown, documents are cut into the units a person thinks in (pages, slides, sheets) and
 * their lines diffed like code. Pure; the readers are in [DocumentText].
 */
internal enum class CompareKind { Image, Document }

/** One page / slide / sheet: what it is called and its lines of text. */
internal data class DocUnit(val label: String, val lines: List<String>)

internal enum class OpKind(val mark: Char) { Same('='), Add('+'), Del('-') }

internal data class DiffOp(val kind: OpKind, val text: String)

/** A shown stretch of the diff: the unit it starts in, its lines, how many unchanged lines were folded before it. */
internal data class Hunk(val unit: String, val ops: List<DiffOp>, val skippedBefore: Int)

/** A line as the page draws it; a changed table row shows each cell before and after (equal when it did not change). */
internal sealed interface CompareLine {
    data class Same(val text: String) : CompareLine
    data class Added(val text: String) : CompareLine
    data class Removed(val text: String) : CompareLine
    data class Row(val cells: List<Pair<String, String>>) : CompareLine
}

/** The units' names in the app's words (Page N, Slide N, Sheet "...", Notes). */
internal interface UnitNames {
    fun page(n: Int): String
    fun slide(n: Int): String
    fun sheet(name: String): String
    val notes: String
}

internal object DocumentCompare {
    /** Between the cells of a table row on one line. */
    const val ROW_SEP = " | "
    /** Marks a unit's start in the flattened lines; never in a document's own text. */
    private const val HEADER = "\u0000§"

    private val images = setOf("png", "jpg", "jpeg", "gif", "webp", "bmp", "heic", "heif", "avif", "ico")
    private val documents = setOf("pdf", "docx", "pptx", "xlsx", "xlsm")

    /** Whether the Git page compares this file's versions itself (instead of a text diff). */
    fun kind(path: String): CompareKind? = when (FilesPaths.extension(path).lowercase()) {
        in images -> CompareKind.Image
        in documents -> CompareKind.Document
        else -> null
    }

    /** Units into one list of lines, each led by a header line, so a change can say where it is. */
    fun flatten(units: List<DocUnit>): List<String> = units.flatMap { listOf(HEADER + it.label) + it.lines }

    /** A diff line shown: an added or removed unit header reads as the unit's name. */
    fun shown(text: String): String = if (text.startsWith(HEADER)) "—— ${text.removePrefix(HEADER)} ——" else text

    /**
     * Myers' line diff (the one git uses). Past [limit] lines in all, or more than [maxEdits] changes: null, too much
     * to show as a diff. Each step keeps only the frontier it reached (2d + 3 ints), so the most it holds is about
     * [maxEdits]² ints (16 MB at 2 000); the web's 6 000 would be 144 MB on a phone.
     */
    fun diff(a: List<String>, b: List<String>, limit: Int = 60_000, maxEdits: Int = 2_000): List<DiffOp>? {
        val n = a.size
        val m = b.size
        if (n + m > limit) return null
        val max = minOf(n + m, maxEdits)
        val off = max + 1
        val v = IntArray(2 * max + 3)
        // trace[d] holds v[-(d+1)..d+1] as it was when step d began.
        val trace = ArrayList<IntArray>()
        var found = -1
        search@ for (d in 0..max) {
            trace += v.copyOfRange(off - d - 1, off + d + 2)
            for (k in -d..d step 2) {
                var x = if (k == -d || (k != d && v[off + k - 1] < v[off + k + 1])) v[off + k + 1] else v[off + k - 1] + 1
                var y = x - k
                while (x < n && y < m && a[x] == b[y]) { x++; y++ }
                v[off + k] = x
                if (x >= n && y >= m) { found = d; break@search }
            }
        }
        if (found < 0) return null
        // Walk back through the saved frontiers.
        val ops = ArrayList<DiffOp>()
        var x = n
        var y = m
        for (d in found downTo 1) {
            val frontier = trace[d]
            fun pv(k: Int) = frontier[k + d + 1]
            val k = x - y
            val prevK = if (k == -d || (k != d && pv(k - 1) < pv(k + 1))) k + 1 else k - 1
            val prevX = pv(prevK)
            val prevY = prevX - prevK
            while (x > prevX && y > prevY) { ops += DiffOp(OpKind.Same, a[--x]); y-- }
            if (x == prevX) ops += DiffOp(OpKind.Add, b[--y]) else ops += DiffOp(OpKind.Del, a[--x])
        }
        while (x > 0 && y > 0) { ops += DiffOp(OpKind.Same, a[--x]); y-- }
        return ops.asReversed()
    }

    /** The changed stretches with [context] unchanged lines around each, labeled by the unit they start in. */
    fun hunks(ops: List<DiffOp>, context: Int = 2): List<Hunk> {
        val out = ArrayList<Hunk>()
        var unit = ""
        val unitAt = ops.map { o -> if (o.text.startsWith(HEADER)) unit = o.text.removePrefix(HEADER); unit }
        val changed = ops.map { it.kind != OpKind.Same }
        var i = 0
        var last = 0
        while (i < ops.size) {
            if (!changed[i]) { i++; continue }
            val start = maxOf(last, i - context)
            var end = i
            while (end < ops.size) {
                if (changed[end]) { end++; continue }
                var j = end
                while (j < ops.size && !changed[j] && j - end < context * 2) j++
                if (j < ops.size && changed[j] && j - end < context * 2) end = j else break
            }
            val stop = minOf(ops.size, end + context)
            val slice = ops.subList(start, stop).filterNot { it.text.startsWith(HEADER) && it.kind == OpKind.Same }
            // Named after the unit the change is in (the lines before it may be the previous page's end); folded lines
            // are the document's own, a unit's header line is not one of them.
            out += Hunk(unitAt[i], slice, (last until start).count { !ops[it].text.startsWith(HEADER) })
            last = stop
            i = stop
        }
        return out
    }

    /**
     * Lines to draw: removed lines followed by as many added ones that are table rows of the same width become one row
     * each, old → new only in the cells that changed (instead of the whole row twice).
     */
    fun pairRows(ops: List<DiffOp>): List<CompareLine> {
        val out = ArrayList<CompareLine>()
        var i = 0
        while (i < ops.size) {
            val op = ops[i]
            if (op.kind != OpKind.Del) { out += line(op); i++; continue }
            var d = i
            while (d < ops.size && ops[d].kind == OpKind.Del) d++
            var a = d
            while (a < ops.size && ops[a].kind == OpKind.Add) a++
            val dels = ops.subList(i, d)
            val adds = ops.subList(d, a)
            val paired = dels.indices.filter { k ->
                k < adds.size && ROW_SEP in dels[k].text && dels[k].text.split(ROW_SEP).size == adds[k].text.split(ROW_SEP).size
            }.toSet()
            for (k in paired) out += CompareLine.Row(dels[k].text.split(ROW_SEP).zip(adds[k].text.split(ROW_SEP)))
            dels.filterIndexed { k, _ -> k !in paired }.forEach { out += line(it) }
            adds.filterIndexed { k, _ -> k !in paired }.forEach { out += line(it) }
            i = a
        }
        return out
    }

    /** A unit's header line of [flatten], not a line of the document. */
    fun isHeader(op: DiffOp) = op.text.startsWith(HEADER)

    private fun line(op: DiffOp): CompareLine = when (op.kind) {
        OpKind.Same -> CompareLine.Same(shown(op.text))
        OpKind.Add -> CompareLine.Added(shown(op.text))
        OpKind.Del -> CompareLine.Removed(shown(op.text))
    }
}

/**
 * The versions of a working-tree change: unstaged, the index against the tree; [staged], HEAD against the index. A
 * rename's old version is under its old name; a file deleted in the tree has no new one.
 */
internal fun compareChange(file: SessionGitFile, staged: Boolean): FileSelection.Compare =
    if (staged) FileSelection.Compare(file.path, file.originalPath ?: file.path, "HEAD", "INDEX", change = file)
    else FileSelection.Compare(file.path, file.path, "INDEX", "WORKTREE", deleted = file.worktree == "D", change = file)

/** A commit's file: its parent's version against the commit's. */
internal fun compareCommit(hash: String, file: SessionGitFile): FileSelection.Compare =
    FileSelection.Compare(file.path, file.originalPath ?: file.path, "$hash^", hash)
