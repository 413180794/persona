package jarvis.android.ui.files

import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

// Pure helpers of the file browser and Git pages (unit-tested in FilesPathsTest).

object FilesPaths {
    /** [dir] + [name] without doubling or losing the slash ("/" + "a" = "/a", "~" + "a" = "~/a"). */
    fun join(dir: String, name: String): String {
        val n = name.trim('/')
        if (n.isEmpty()) return dir
        return if (dir.endsWith("/")) dir + n else "$dir/$n"
    }

    /** The parent of an absolute or "~" path; null at the top ("/", "~"). */
    fun parent(path: String): String? {
        val p = path.trimEnd('/')
        if (p.isEmpty() || p == "~") return null
        val i = p.lastIndexOf('/')
        return when {
            i < 0 -> null
            i == 0 -> "/"
            else -> p.substring(0, i)
        }
    }

    /** The last component ("/" for the root). */
    fun name(path: String): String = path.trimEnd('/').substringAfterLast('/').ifEmpty { "/" }

    /**
     * The levels from [root] down to [here] as (title, path) pairs, for the breadcrumb. When [here] is not under
     * [root], it is the only level.
     */
    fun crumbs(root: String, here: String): List<Pair<String, String>> {
        val r = root.trimEnd('/').ifEmpty { "/" }
        val h = here.trimEnd('/').ifEmpty { "/" }
        if (h == r) return listOf(name(r) to r)
        val prefix = if (r == "/") "/" else "$r/"
        if (!h.startsWith(prefix)) return listOf(name(h) to h)
        val out = mutableListOf(name(r) to r)
        var acc = r
        for (part in h.removePrefix(prefix).split('/').filter { it.isNotEmpty() }) {
            acc = join(acc, part)
            out += part to acc
        }
        return out
    }

    /** "~/…" for paths in [home] (the meta line under a file's title). */
    fun short(path: String, home: String?): String {
        if (home.isNullOrEmpty() || home == "/") return path
        val h = home.trimEnd('/')
        return when {
            path == h -> "~"
            path.startsWith("$h/") -> "~" + path.removePrefix(h)
            else -> path
        }
    }

    /** Whether a path most likely names a directory when nobody said (ends in "/", "~", or has no extension). */
    fun looksLikeDir(path: String): Boolean {
        if (path.endsWith("/") || path == "~" || path.isEmpty()) return true
        return '.' !in path.trimEnd('/').substringAfterLast('/')
    }

    /**
     * The Git letter for [full] in [git] (a file: its own change; a folder: the first change inside it), or null
     * when unchanged or outside the repository.
     */
    fun gitMark(git: GitStatus?, full: String, dir: Boolean): String? {
        val root = git?.root?.trimEnd('/') ?: return null
        if (git.files.isEmpty() || !full.startsWith("$root/")) return null
        val rel = full.removePrefix("$root/").trimEnd('/')
        return if (dir) git.files.firstOrNull { it.path.startsWith("$rel/") }?.letter
        else git.files.firstOrNull { it.path == rel }?.letter
    }

    /** [s] cut from the start to at most [max] characters ("…/b/c"): paths keep their informative end. */
    fun keepEnd(s: String, max: Int = 48): String = if (s.length <= max) s else "…" + s.takeLast(max - 1)

    fun extension(name: String): String = name.substringAfterLast('.', "").lowercase()

    fun isMarkdown(name: String) = extension(name) in setOf("md", "markdown", "mdx")

    /** 1023 B, 1.5 KB, 12 MB (binary units, one decimal under 10). */
    fun formatBytes(n: Long): String {
        if (n < 1024) return "$n B"
        val units = listOf("KB", "MB", "GB", "TB")
        var v = n / 1024.0
        var i = 0
        while (v >= 1024 && i < units.lastIndex) { v /= 1024; i++ }
        return if (v < 10) String.format(java.util.Locale.ROOT, "%.1f %s", v, units[i]).replace(".0 ", " ") else "${v.toLong()} ${units[i]}"
    }

    /** 刚刚 / 5 分钟前 / 3 小时前 / 2 天前, then the date. */
    fun relative(t: Instant, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String {
        val s = Duration.between(t, now).seconds
        return when {
            s < 60 -> "刚刚"
            s < 3600 -> "${s / 60} 分钟前"
            s < 86400 -> "${s / 3600} 小时前"
            s < 7 * 86400 -> "${s / 86400} 天前"
            else -> {
                val d = t.atZone(zone)
                val pattern = if (d.year == now.atZone(zone).year) "M月d日" else "yyyy年M月d日"
                d.format(DateTimeFormatter.ofPattern(pattern))
            }
        }
    }
}
