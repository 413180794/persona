package persona.core.domain

/**
 * A path a message names, where it is: `[start, end)` covers the path and its line; [path] is as written, without
 * the line (what the machine is asked about).
 */
data class PathRef(val start: Int, val end: Int, val path: String, val line: Int? = null, val col: Int? = null)

/**
 * Paths a message names (the web's `lib/path-refs.ts`, iOS `FilePaths`, plus a line): what looks like a file on the
 * agent's machine, so the chat can ask the machine which are there and link only those.
 *
 * - `~/x`, `/x/y` (two parts at least: `/tmp` alone is a word), or relative with a folder and an extension
 *   (`src/a.kt`; a letter-led extension, so `deepseek/v4.1` is not a file).
 * - A line: `path:12`, `path:12:5`, `path:12-20`, `path#L12`.
 * - Outside backticks a path is ASCII: Chinese text or full-width punctuation right after it ends it (`/tmp/a.md` + a Chinese word).
 *   A path in backticks is whole, whatever it is written in: a home path with Chinese folder and file names.
 * - Not inside a URL or a longer word; a sentence's full stop after it is not part of it.
 */
object PathRefs {
    private const val SEG = "[A-Za-z0-9_.@+\\-]+"
    private const val LINE = "(?::(\\d{1,7})(?::(\\d{1,5}))?(?:[-–]\\d{1,7})?|#L(\\d{1,7})(?:-L?\\d{1,7})?)?"
    private val ascii = Regex(
        "(?<![A-Za-z0-9_/:.\\-~])(~(?:/$SEG)+/?|/$SEG(?:/$SEG)+/?|[A-Za-z0-9_.@\\-]+(?:/$SEG)+\\.[A-Za-z][A-Za-z0-9]{0,7})" +
            "$LINE(?![A-Za-z0-9_/\\-])",
    )
    private val codeSpan = Regex("`([^`\\n]+)`")
    // A whole code span that is a path: ~/ or / then anything but a backtick (non-ASCII names too), and a line.
    private val whole = Regex("^(~?/[^\\s`]+(?: [^\\s`]+)*?)$LINE$")
    private val trailingDots = Regex("(?<=[^./])\\.+$")
    private val onlyRoot = Regex("^~?/+$")

    /**
     * The paths in plain text, in order, at most [limit]. A code span is a candidate whole and also for the ASCII
     * paths in it (`` `/a.md and /b.md` `` with a Chinese "and"): [split] takes the first that is there, the longer one at the same place.
     */
    fun find(text: String, limit: Int = 40): List<PathRef> {
        val out = ArrayList<PathRef>()
        for (m in codeSpan.findAll(text)) {
            val start = m.range.first + 1
            codePath(m.groupValues[1])?.let { out += it.copy(start = start + it.start, end = start + it.end) }
        }
        for (m in ascii.findAll(text)) {
            val ref = fromMatch(m, m.range.first) ?: continue
            if (out.none { it.start == ref.start && it.end == ref.end }) out += ref
        }
        out.sortWith(compareBy<PathRef> { it.start }.thenByDescending { it.end })
        return out.take(limit)
    }

    /** Only the ASCII paths, outside any notion of code spans (Markdown, where code spans are their own nodes). */
    fun findAscii(text: String): List<PathRef> = ascii.findAll(text).mapNotNull { fromMatch(it, it.range.first) }.toList()

    /** The path a whole code span is (`/tmp/a.md`, `src/a.kt:12`, a home path with Chinese names), or null. */
    fun codePath(code: String): PathRef? {
        val lead = code.length - code.trimStart().length
        val t = code.trim()
        if (t.isEmpty() || t.startsWith("//") || "://" in t) return null
        whole.find(t)?.let { w ->
            val ref = fromMatch(w, 0)
            if (ref != null && ref.path.split('/').count { it.isNotEmpty() } >= (if (ref.path.startsWith("~")) 1 else 2))
                return ref.copy(start = ref.start + lead, end = ref.end + lead)
        }
        val m = ascii.find(t) ?: return null
        if (m.range.first != 0 || m.value.length != t.length) return null
        return fromMatch(m, 0)?.let { it.copy(start = it.start + lead, end = it.end + lead) }
    }

    private fun fromMatch(m: MatchResult, at: Int): PathRef? {
        var path = m.groupValues[1]
        val lineText = m.value.substring(path.length)
        // A sentence's full stop (or an ellipsis) is not part of the path, unless the path is only dots.
        if (lineText.isEmpty()) path = trailingDots.replace(path, "")
        if (path.isEmpty() || path == "~" || onlyRoot.matches(path)) return null
        val line = (m.groupValues[2].ifEmpty { m.groupValues[4] }).toIntOrNull()?.takeIf { it > 0 }
        val col = if (line != null) m.groupValues[3].toIntOrNull() else null
        return PathRef(at, at + if (lineText.isNotEmpty()) m.value.length else path.length, path, line, col)
    }

    /** Each path once, in order, for asking the machine. */
    fun unique(refs: List<PathRef>, limit: Int = 24): List<String> {
        val out = ArrayList<String>()
        for (r in refs) if (r.path !in out && out.size < limit) out += r.path
        return out
    }

    /** The refs [linkable] says yes to, in order and none overlapping (the first, the longer at the same place). */
    fun split(refs: List<PathRef>, linkable: (String) -> Boolean): List<PathRef> {
        val out = ArrayList<PathRef>()
        var at = 0
        for (r in refs) {
            if (r.start < at || !linkable(r.path)) continue
            out += r
            at = r.end
        }
        return out
    }
}
