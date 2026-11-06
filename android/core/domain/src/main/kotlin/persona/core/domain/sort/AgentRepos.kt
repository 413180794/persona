package persona.core.domain.sort

/** Where a session's cwd sits: the repository it belongs to and the working tree inside it. */
data class RepoPlace(val repo: String, val name: String, val worktree: String, val main: Boolean)

// The layouts a working tree commonly lives in (herdr, Claude Code, a home-level collection), as on web and iOS.
private val WorktreeMarkers = listOf("/.worktrees/", "/.claude/worktrees/", "/.herdr/worktrees/")

private fun baseName(path: String) = path.split('/').lastOrNull { it.isNotEmpty() }.orEmpty()

/**
 * The repository and working tree a cwd belongs to, from the path alone (the protocol carries no repository; web's
 * `repoOf`, iOS's `RepoPlace`). A tree under one of the known layouts belongs to the directory above the marker,
 * `~/.worktrees/<repo>/<tree>` to `<repo>` under it; any other path is its own repository's main checkout.
 */
fun repoOf(cwd: String): RepoPlace {
    val path = cwd.trimEnd('/')
    if (path.isEmpty()) return RepoPlace("", "", "", main = true)
    for (marker in WorktreeMarkers) {
        val at = path.indexOf(marker)
        if (at <= 0) continue
        val repo = path.substring(0, at)
        val rest = path.substring(at + marker.length).split('/').filter { it.isNotEmpty() }
        if (rest.size >= 2) return RepoPlace(repo + marker + rest[0], rest[0], path, main = false)
        return RepoPlace(repo, baseName(repo), path, main = false)
    }
    return RepoPlace(path, baseName(path), path, main = true)
}

/** A working tree of a repository on one computer and the sessions in it (in the order they came). */
data class AgentWorktree(val id: String, val name: String, val path: String, val main: Boolean, val members: List<AgentSession>)

/** One repository on one computer, its working trees the main checkout first, then by name. */
data class AgentRepo(val id: String, val machineId: String, val name: String, val path: String, val worktrees: List<AgentWorktree>) {
    val members: List<AgentSession> get() = worktrees.flatMap { it.members }
    /** Only its main checkout: its sessions sit right under the repository, without a working-tree heading. */
    val flat: Boolean get() = worktrees.size == 1 && worktrees[0].main
}

/**
 * What a repository is called when its sessions have no cwd (iOS `RepoTree.unnamed`): sorts after every name; the
 * heading says "Other" (repoTitle).
 */
const val UnnamedRepo = "\uFFFF"

private val Chunks = Regex("\\d+|\\D+")

/** Natural order, like iOS `localizedStandardCompare`: case-insensitive, digit runs by value (repo2 before repo10). */
internal val NaturalOrder = Comparator<String> { a, b ->
    val xs = Chunks.findAll(a.lowercase()).map { it.value }.toList()
    val ys = Chunks.findAll(b.lowercase()).map { it.value }.toList()
    for (i in 0 until minOf(xs.size, ys.size)) {
        val x = xs[i]
        val y = ys[i]
        val c = if (x[0].isDigit() && y[0].isDigit()) {
            val (u, v) = x.trimStart('0') to y.trimStart('0')
            compareValuesBy(u to x, v to y, { it.first.length }, { it.first }, { it.second.length })
        } else x.compareTo(y)
        if (c != 0) return@Comparator c
    }
    xs.size.compareTo(ys.size)
}

private val byName = compareBy<Pair<String, String>, String>(NaturalOrder) { it.first }.thenBy { it.second }

/** The repositories of [rows] by name in natural order (then path), each computer's apart; ids are `repo/<machine>/<path>`, `tree/<machine>/<path>`. */
fun agentRepos(rows: List<AgentSession>): List<AgentRepo> =
    rows.groupBy { it.session.machineId to repoOf(it.session.cwd).repo }.map { (key, members) ->
        val (machine, repo) = key
        val place = repoOf(members.first().session.cwd)
        val name = place.name.ifEmpty { UnnamedRepo }
        val trees = members.groupBy { repoOf(it.session.cwd).worktree }.map { (path, inTree) ->
            val main = repoOf(inTree.first().session.cwd).main
            AgentWorktree("tree/$machine/$path", if (path == repo) name else baseName(path), path, main, inTree)
        }.sortedWith(compareBy<AgentWorktree> { !it.main }.thenComparing({ it.name to it.path }, byName))
        AgentRepo("repo/$machine/$repo", machine, name, repo, trees)
    }.sortedWith(compareBy<AgentRepo, Pair<String, String>>(byName) { it.name to it.path }.thenBy { it.machineId })
