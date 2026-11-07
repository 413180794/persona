package persona.feature.settings

import persona.core.contracts.api.ExtensionDiffHunk
import persona.core.contracts.api.ExtensionDiffThread
import persona.core.contracts.api.ExtensionTreeEntry
import persona.core.designsystem.Tone

// UI version 4's code hosting components (docs/extensions/extensions.md "UI version 4", #2951) without the looks: a file tree from whole
// paths, a diff's hunks as numbered rows, the threads at their lines, and a small syntax highlighter (keywords,
// strings, comments, numbers of the common languages; a block comment goes on across lines). Drawn by
// ExtensionCodeViews.kt.

/** A node of a file tree: a directory (with [children]) or a file; [name] may be a folded chain (src/upload). */
internal data class CodeTreeNode(
    val name: String,
    val path: String,
    val dir: Boolean,
    val entry: ExtensionTreeEntry? = null,
    val children: List<CodeTreeNode> = emptyList(),
    /** A directory given with nothing under it: opened by asking the extension (expand). */
    val unloaded: Boolean = false,
)

/** One line of a tree as drawn: the node and how deep it sits. */
internal data class CodeTreeLine(val node: CodeTreeNode, val depth: Int)

internal enum class DiffKind { Context, Add, Del, Note }

/** A line of a hunk: its kind, its numbers in the old and the new file, its text without the prefix. */
internal data class DiffRow(val kind: DiffKind, val old: Int?, val new: Int?, val text: String)

internal enum class TokenKind { Plain, Keyword, Text, Comment, Number, Type }

internal data class CodeToken(val text: String, val kind: TokenKind)

internal object ExtensionCodeText {
    /** The tree of [entries] by their paths: directories first, then files, by name; one directory in a directory folds. */
    fun tree(entries: List<ExtensionTreeEntry>): List<CodeTreeNode> {
        class Dir(val name: String, val path: String) {
            var entry: ExtensionTreeEntry? = null
            val dirs = LinkedHashMap<String, Dir>()
            val files = mutableListOf<CodeTreeNode>()
        }
        val root = Dir("", "")
        fun dirOf(path: String): Dir {
            if (path.isEmpty()) return root
            var at = root
            path.split('/').forEach { part -> at = at.dirs.getOrPut(part) { Dir(part, if (at.path.isEmpty()) part else "${at.path}/$part") } }
            return at
        }
        for (e in entries) {
            val path = e.path.trim('/')
            if (path.isEmpty()) continue
            if (e.kind == "dir") { dirOf(path).entry = e; continue }
            val cut = path.lastIndexOf('/')
            dirOf(if (cut < 0) "" else path.substring(0, cut)).files += CodeTreeNode(path.substring(cut + 1), path, dir = false, entry = e)
        }
        fun node(d: Dir): CodeTreeNode {
            val children = d.dirs.values.map(::node).sortedBy { it.name.lowercase() } + d.files.sortedBy { it.name.lowercase() }
            var n = CodeTreeNode(d.name, d.path, dir = true, entry = d.entry, children = children, unloaded = children.isEmpty() && d.entry != null)
            while (n.children.size == 1 && n.children[0].dir && !n.children[0].unloaded) {
                val only = n.children[0]
                n = only.copy(name = "${n.name}/${only.name}")
            }
            return n
        }
        // the root itself never folds: its directories and files are the tree's first level
        return root.dirs.values.map(::node).sortedBy { it.name.lowercase() } + root.files.sortedBy { it.name.lowercase() }
    }

    /** The lines of [tree] as drawn, the directories in [closed] (and those still to load) without what is under them. */
    fun lines(tree: List<CodeTreeNode>, closed: Set<String>): List<CodeTreeLine> = buildList {
        fun walk(nodes: List<CodeTreeNode>, depth: Int) {
            for (n in nodes) {
                add(CodeTreeLine(n, depth))
                if (n.dir && !n.unloaded && n.path !in closed) walk(n.children, depth + 1)
            }
        }
        walk(tree, 0)
    }

    /** The files of a tree (not its directories). */
    fun files(entries: List<ExtensionTreeEntry>): Int = entries.count { it.kind != "dir" }

    /** A file's status as a letter and a tone. */
    fun status(status: String?): Pair<String, Tone>? = when (status) {
        "added" -> "A" to Tone.Positive
        "deleted" -> "D" to Tone.Danger
        "renamed" -> "R" to Tone.Caution
        "modified" -> "M" to Tone.Caution
        else -> null
    }

    /** A hunk's lines, numbered on both sides. */
    fun rows(h: ExtensionDiffHunk): List<DiffRow> {
        var o = h.oldStart
        var n = h.newStart
        return h.lines.map { line ->
            val text = if (line.isEmpty()) "" else line.substring(1)
            when (line.firstOrNull() ?: ' ') {
                '+' -> DiffRow(DiffKind.Add, null, n++, text)
                '-' -> DiffRow(DiffKind.Del, o++, null, text)
                '\\' -> DiffRow(DiffKind.Note, null, null, text.trim())
                else -> DiffRow(DiffKind.Context, o++, n++, text)
            }
        }
    }

    /** How many lines lie hidden before hunk [i] (after the one before it). */
    fun gapBefore(hunks: List<ExtensionDiffHunk>, i: Int): Int {
        val h = hunks[i]
        if (i == 0) return (h.newStart - 1).coerceAtLeast(0)
        val prev = hunks[i - 1]
        val end = prev.newStart + prev.lines.count { it.firstOrNull() != '-' && it.firstOrNull() != '\\' }
        return (h.newStart - end).coerceAtLeast(0)
    }

    /** Where a thread sits: `new:12`, `old:30`. */
    fun spot(side: String, line: Int): String = "${if (side == "old") "old" else "new"}:$line"

    /** Threads under the lines they are at, and the rest (outdated, or at a line not in the diff) for the end. */
    fun placeThreads(threads: List<ExtensionDiffThread>, hunks: List<ExtensionDiffHunk>): Pair<Map<String, List<ExtensionDiffThread>>, List<ExtensionDiffThread>> {
        val lines = buildSet { hunks.forEach { h -> rows(h).forEach { r -> r.old?.let { add(spot("old", it)) }; r.new?.let { add(spot("new", it)) } } } }
        val at = LinkedHashMap<String, MutableList<ExtensionDiffThread>>()
        val rest = mutableListOf<ExtensionDiffThread>()
        for (t in threads) {
            val key = spot(t.side, t.line)
            if (!t.outdated && key in lines) at.getOrPut(key) { mutableListOf() } += t else rest += t
        }
        return at to rest
    }

    /** The spots a row's threads and comment box sit at: a deleted line's old one, any other's new one. */
    fun spotsOf(r: DiffRow): List<String> = buildList {
        r.new?.let { add(spot("new", it)) }
        if (r.kind == DiffKind.Del) r.old?.let { add(spot("old", it)) }
        if (r.kind == DiffKind.Context) r.old?.let { add(spot("old", it)) }
    }

    // ── highlighting ──

    private val cLike = setOf("go", "kotlin", "java", "swift", "javascript", "typescript", "rust", "c", "cpp", "csharp", "scala", "dart", "php", "objectivec", "groovy")
    private val hashComments = setOf("python", "ruby", "bash", "yaml", "toml", "perl", "r", "makefile", "dockerfile", "elixir")

    private val keywords: Map<String, Set<String>> = mapOf(
        "go" to "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false iota",
        "kotlin" to "abstract as break by catch class companion const continue data do else enum false final for fun if import in init interface internal is lateinit null object open operator out override package private protected public return sealed super suspend this throw true try typealias val var when while",
        "java" to "abstract boolean break byte case catch char class const continue default do double else enum extends final finally float for if implements import instanceof int interface long native new null package private protected public return short static super switch synchronized this throw throws transient true false try void volatile while var record",
        "swift" to "as associatedtype break case catch class continue default defer do else enum extension fallthrough false fileprivate for func guard if import in init inout internal is let nil open operator private protocol public repeat rethrows return self Self static struct subscript super switch throw throws true try typealias var where while async await actor some any",
        "javascript" to "async await break case catch class const continue debugger default delete do else export extends false finally for from function if import in instanceof let new null of return static super switch this throw true try typeof undefined var void while with yield",
        "typescript" to "abstract any as async await boolean break case catch class const continue declare default delete do else enum export extends false finally for from function if implements import in instanceof interface keyof let namespace never new null number of private protected public readonly return static string super switch this throw true try type typeof undefined unknown var void while yield",
        "python" to "and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield self",
        "rust" to "as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while",
        "c" to "auto break case char const continue default do double else enum extern float for goto if inline int long register return short signed sizeof static struct switch typedef union unsigned void volatile while NULL true false",
        "cpp" to "auto bool break case catch char class const constexpr continue default delete do double else enum explicit extern false float for friend if inline int long namespace new noexcept nullptr operator private protected public return short signed sizeof static struct switch template this throw true try typedef typename union unsigned using virtual void volatile while",
        "ruby" to "alias and begin break case class def defined? do else elsif end ensure false for if in module next nil not or redo rescue retry return self super then true undef unless until when while yield",
        "bash" to "case do done elif else esac export fi for function if in local return then until while echo",
        "sql" to "select from where and or not insert into values update set delete create table alter drop index join left right inner outer on group by order having limit as null is in like distinct union all case when then else end primary key",
    ).mapValues { (_, v) -> v.split(' ').toSet() }

    private val byExtension = mapOf(
        "go" to "go", "kt" to "kotlin", "kts" to "kotlin", "java" to "java", "swift" to "swift", "js" to "javascript", "jsx" to "javascript",
        "mjs" to "javascript", "ts" to "typescript", "tsx" to "typescript", "py" to "python", "rs" to "rust", "c" to "c", "h" to "c",
        "cc" to "cpp", "cpp" to "cpp", "hpp" to "cpp", "m" to "objectivec", "cs" to "csharp", "rb" to "ruby", "sh" to "bash", "bash" to "bash",
        "zsh" to "bash", "yaml" to "yaml", "yml" to "yaml", "toml" to "toml", "json" to "json", "sql" to "sql", "php" to "php", "scala" to "scala",
        "dart" to "dart", "gradle" to "groovy", "groovy" to "groovy", "pl" to "perl", "r" to "r", "ex" to "elixir", "exs" to "elixir",
    )
    private val aliases = mapOf("golang" to "go", "ts" to "typescript", "js" to "javascript", "py" to "python", "shell" to "bash", "c++" to "cpp", "c#" to "csharp", "objective-c" to "objectivec")

    /** The language a file is highlighted as: the extension's, else from its name; "" plain. */
    fun language(language: String?, path: String?): String {
        val l = language?.trim()?.lowercase().orEmpty()
        if (l.isNotEmpty()) return aliases[l] ?: byExtension[l] ?: l
        val name = path?.substringAfterLast('/')?.lowercase().orEmpty()
        if (name == "makefile") return "makefile"
        if (name == "dockerfile") return "dockerfile"
        return byExtension[name.substringAfterLast('.', "")] ?: ""
    }

    /**
     * [lines] highlighted as [language], line by line; a block comment (`/* … */`) open at a line's end goes on in the
     * next. A language it knows nothing of is plain.
     */
    fun highlight(lines: List<String>, language: String): List<List<CodeToken>> {
        if (language.isEmpty() || language == "plaintext") return lines.map { listOf(CodeToken(it, TokenKind.Plain)) }
        val words = keywords[language].orEmpty()
        val slashes = language in cLike || language == "json"
        val hashes = language in hashComments
        val dashes = language == "sql" || language == "lua"
        var inBlock = false
        return lines.map { line ->
            val out = mutableListOf<CodeToken>()
            fun add(text: String, kind: TokenKind) {
                if (text.isEmpty()) return
                val last = out.lastOrNull()
                if (last != null && last.kind == kind) out[out.lastIndex] = last.copy(text = last.text + text) else out += CodeToken(text, kind)
            }
            var i = 0
            while (i < line.length) {
                if (inBlock) {
                    val end = line.indexOf("*/", i)
                    if (end < 0) { add(line.substring(i), TokenKind.Comment); i = line.length } else { add(line.substring(i, end + 2), TokenKind.Comment); i = end + 2; inBlock = false }
                    continue
                }
                val c = line[i]
                val rest = line.length - i
                when {
                    slashes && c == '/' && rest > 1 && line[i + 1] == '/' -> { add(line.substring(i), TokenKind.Comment); i = line.length }
                    slashes && c == '/' && rest > 1 && line[i + 1] == '*' -> { inBlock = true; add("/*", TokenKind.Comment); i += 2 }
                    hashes && c == '#' -> { add(line.substring(i), TokenKind.Comment); i = line.length }
                    dashes && c == '-' && rest > 1 && line[i + 1] == '-' -> { add(line.substring(i), TokenKind.Comment); i = line.length }
                    c == '"' || c == '\'' || c == '`' -> {
                        var j = i + 1
                        while (j < line.length && line[j] != c) { if (line[j] == '\\') j++; j++ }
                        val end = (j + 1).coerceAtMost(line.length)
                        add(line.substring(i, end), TokenKind.Text); i = end
                    }
                    c.isDigit() && (i == 0 || !line[i - 1].isLetterOrDigit() && line[i - 1] != '_') -> {
                        var j = i
                        while (j < line.length && (line[j].isLetterOrDigit() || line[j] == '.' || line[j] == '_')) j++
                        add(line.substring(i, j), TokenKind.Number); i = j
                    }
                    c.isLetter() || c == '_' || c == '@' || c == '$' -> {
                        var j = i + 1
                        while (j < line.length && (line[j].isLetterOrDigit() || line[j] == '_')) j++
                        val word = line.substring(i, j)
                        val kind = when {
                            word in words || (language == "sql" && word.lowercase() in words) -> TokenKind.Keyword
                            word.first().isUpperCase() && word.length > 1 && language != "sql" -> TokenKind.Type
                            else -> TokenKind.Plain
                        }
                        add(word, kind); i = j
                    }
                    else -> { add(c.toString(), TokenKind.Plain); i++ }
                }
            }
            out
        }
    }

    /** A hunk's old and new sides' lines, for highlighting each with its own state. */
    fun sides(h: ExtensionDiffHunk): Pair<List<String>, List<String>> {
        val old = mutableListOf<String>()
        val new = mutableListOf<String>()
        h.lines.forEach { line ->
            val text = if (line.isEmpty()) "" else line.substring(1)
            when (line.firstOrNull() ?: ' ') {
                '+' -> new += text
                '-' -> old += text
                '\\' -> {}
                else -> { old += text; new += text }
            }
        }
        return old to new
    }

    /** How long ago a time (RFC 3339) was, in milliseconds; null when it does not read. */
    fun ago(at: String?, now: Long = System.currentTimeMillis()): Long? = at?.let { runCatching { java.time.OffsetDateTime.parse(it).toInstant().toEpochMilli() }.getOrNull() }
        ?.let { (now - it).coerceAtLeast(0) }
}
