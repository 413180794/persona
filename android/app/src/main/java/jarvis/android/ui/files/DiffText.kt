package jarvis.android.ui.files

/**
 * The line model of every diff in the app (iOS `DiffText.Line` / `parse`): the chat's `DiffView` (Edit / MultiEdit /
 * Write, Codex patches) and the Git page's whole-file diffs.
 */
data class DiffLine(val kind: Kind, val text: String, val old: Int? = null, val new: Int? = null) {
    enum class Kind { ADD, DEL, CONTEXT, HUNK, INFO }
}

/** Lines and their +/− counts. */
data class DiffContent(val lines: List<DiffLine>, val adds: Int, val dels: Int)

object DiffText {
    private val memo = java.util.Collections.synchronizedMap(object : LinkedHashMap<String, DiffContent>(64, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, DiffContent>?) = size > 64
    })

    /** Claude's Edit: all of [old] removed, then all of [new] added (it has no line numbers or context). */
    fun edit(old: String, new: String): DiffContent = memo.getOrPut("e\u0000$old\u0000$new") {
        val removed = if (old.isEmpty()) emptyList() else old.split('\n')
        val added = if (new.isEmpty()) emptyList() else new.split('\n')
        DiffContent(removed.map { DiffLine(DiffLine.Kind.DEL, it) } + added.map { DiffLine(DiffLine.Kind.ADD, it) }, added.size, removed.size)
    }

    /** A unified diff (Codex's patches, `git diff`), with old / new line numbers. */
    fun unified(diff: String): DiffContent = memo.getOrPut("u\u0000$diff") {
        val lines = parse(diff)
        DiffContent(lines, lines.count { it.kind == DiffLine.Kind.ADD }, lines.count { it.kind == DiffLine.Kind.DEL })
    }

    fun parse(diff: String): List<DiffLine> {
        val out = ArrayList<DiffLine>()
        var old = 0
        var new = 0
        for (raw in diff.split('\n')) {
            if (raw.startsWith("diff --git") || raw.startsWith("index ") || raw.startsWith("--- ") || raw.startsWith("+++ ")) continue
            when {
                raw.startsWith("@@") -> {
                    // @@ -12,7 +12,9 @@ context
                    val parts = raw.split(' ')
                    if (parts.size > 2) {
                        old = parts[1].drop(1).substringBefore(',').toIntOrNull() ?: 0
                        new = parts[2].drop(1).substringBefore(',').toIntOrNull() ?: 0
                    }
                    out += DiffLine(DiffLine.Kind.HUNK, raw)
                }
                raw.startsWith("+") -> { out += DiffLine(DiffLine.Kind.ADD, raw.drop(1), new = new); new++ }
                raw.startsWith("-") -> { out += DiffLine(DiffLine.Kind.DEL, raw.drop(1), old = old); old++ }
                raw.startsWith(" ") -> { out += DiffLine(DiffLine.Kind.CONTEXT, raw.drop(1), old, new); old++; new++ }
                raw.startsWith("\\") || raw.startsWith("Binary") || raw.startsWith("new file") || raw.startsWith("deleted file") ||
                    raw.startsWith("similarity") || raw.startsWith("rename ") || raw.startsWith("old mode") || raw.startsWith("new mode") ->
                    out += DiffLine(DiffLine.Kind.INFO, raw)
            }
        }
        return out
    }

    /** A multi-file diff (`git show`) split per file, keyed by the new path (taken from the end: spaces survive). */
    fun split(diff: String): Map<String, String> {
        val res = LinkedHashMap<String, String>()
        var path: String? = null
        val buf = ArrayList<String>()
        fun flush() { path?.let { res[it] = buf.joinToString("\n") } }
        for (line in diff.split('\n')) {
            if (line.startsWith("diff --git ")) {
                flush()
                buf.clear()
                val i = line.lastIndexOf(" b/")
                path = if (i >= 0) line.substring(i + 3) else null
            }
            buf += line
        }
        flush()
        return res
    }
}
