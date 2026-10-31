package jarvis.android.ui.files

import jarvis.android.data.ApiClient
import jarvis.android.data.JsonCodec
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.time.Instant

// Read-only files and Git of a machine (docs/protocol.md「REST」, server/internal/hub/http.go): the hub proxies each
// call to the node of that machine. Paths may be absolute, "~" / "~/…", or relative to `cwd`.

/** `GET /machines/{mid}/file`: a file's text (UTF-8), base64 [data] (images, PDFs) or a directory's names. */
@Serializable
data class FileContent(
    val path: String = "",
    val name: String = "",
    val size: Long = 0,
    val mime: String? = null,
    val text: String? = null,
    val data: String? = null,
    val truncated: Boolean = false,
    @SerialName("is_dir") val isDir: Boolean = false,
    /** Directory listing: names, directories end with "/". */
    val entries: List<String>? = null,
)

/** One row of `GET /machines/{mid}/fs`. */
@Serializable
data class FsEntry(
    val name: String = "",
    val dir: Boolean = false,
    val symlink: Boolean = false,
    val size: Long = 0,
    val mtime: String? = null,
) {
    val modified: Instant? get() = offsetTime(mtime)
}

/** A directory: [path] is the resolved absolute path ("~" expanded), directories first, at most 2000 entries. */
@Serializable
data class FsListing(
    val path: String = "",
    val entries: List<FsEntry> = emptyList(),
    val truncated: Boolean = false,
)

@Serializable
data class GitCommit(
    val hash: String = "",
    val short: String = "",
    val subject: String = "",
    val body: String? = null,
    val author: String = "",
    val date: String? = null,
    val refs: String? = null,
) {
    val time: Instant? get() = offsetTime(date)

    /** Branch / tag names (iOS `refNames`): "HEAD -> main" becomes "main", a bare HEAD is dropped. */
    val refNames: List<String>
        get() = refs.orEmpty().split(',').map { it.trim() }
            .map { it.removePrefix("HEAD -> ") }
            .filter { it.isNotEmpty() && it != "HEAD" }
}

/** One changed file: [index] / [worktree] are porcelain letters ("." = unchanged, "?" = untracked, "U" = conflict). */
@Serializable
data class GitFile(
    val path: String = "",
    @SerialName("orig_path") val origPath: String? = null,
    val index: String = ".",
    val worktree: String = ".",
    val added: Int = 0,
    val deleted: Int = 0,
    val binary: Boolean = false,
) {
    val isUntracked: Boolean get() = index == "?"
    val isConflicted: Boolean get() = index == "U" || worktree == "U"
    val isStaged: Boolean get() = !isUntracked && index != "."
    val hasUnstaged: Boolean get() = !isUntracked && worktree != "."

    /** The one letter of the change list: untracked, conflict, then the staged change, then the working tree's. */
    val letter: String
        get() = when {
            isUntracked -> "U"
            isConflicted -> "!"
            index != "." -> index
            else -> worktree
        }
    val name: String get() = path.substringAfterLast('/')
    val folder: String get() = if ('/' in path) path.substringBeforeLast('/') else ""
}

@Serializable
data class GitStatus(
    @SerialName("is_repo") val isRepo: Boolean = false,
    val root: String? = null,
    val branch: String? = null,
    val head: GitCommit? = null,
    val upstream: String? = null,
    val ahead: Int = 0,
    val behind: Int = 0,
    val files: List<GitFile> = emptyList(),
    val stashes: Int = 0,
    /** The latest 30; older ones via [gitLog]. */
    val commits: List<GitCommit> = emptyList(),
)

@Serializable
data class GitDiff(val path: String = "", val diff: String = "", val truncated: Boolean = false)

@Serializable
data class GitShow(
    val commit: GitCommit = GitCommit(),
    val files: List<GitFile> = emptyList(),
    val diff: String = "",
    val truncated: Boolean = false,
)

@Serializable
internal data class GitLog(val commits: List<GitCommit> = emptyList())

private fun m(machineId: String) = "/api/v1/machines/$machineId"

private fun withCwd(cwd: String?, vararg kv: Pair<String, String>): Map<String, String> =
    buildMap { kv.forEach { put(it.first, it.second) }; if (!cwd.isNullOrEmpty()) put("cwd", cwd) }

private suspend inline fun <reified T> ApiClient.getJson(path: String, query: Map<String, String>): T =
    JsonCodec.decodeFromString(raw("GET", path, query = query))

/** A file (up to 2 MB, then [FileContent.truncated]) or a directory's names. */
suspend fun ApiClient.readFile(machineId: String, path: String, cwd: String? = null): FileContent =
    getJson("${m(machineId)}/file", withCwd(cwd, "path" to path))

/** A directory for the browser; an empty [path] lists [cwd]. */
suspend fun ApiClient.listDir(machineId: String, path: String, cwd: String? = null): FsListing =
    getJson("${m(machineId)}/fs", withCwd(cwd, "path" to path))

/** The repository [cwd] is in (`is_repo` false when it is in none). */
suspend fun ApiClient.gitStatus(machineId: String, cwd: String): GitStatus =
    getJson("${m(machineId)}/git/status", mapOf("cwd" to cwd))

/** One file's unstaged (or [staged]) diff, relative to the repository root; untracked files diff against /dev/null. */
suspend fun ApiClient.gitDiff(machineId: String, cwd: String, path: String, staged: Boolean): GitDiff =
    getJson("${m(machineId)}/git/diff", mapOf("cwd" to cwd, "path" to path, "staged" to if (staged) "1" else "0"))

suspend fun ApiClient.gitShow(machineId: String, cwd: String, hash: String): GitShow =
    getJson("${m(machineId)}/git/show", mapOf("cwd" to cwd, "hash" to hash))

/** Commits after the first [skip] (the status already has 30). */
suspend fun ApiClient.gitLog(machineId: String, cwd: String, skip: Int, limit: Int = 50): List<GitCommit> =
    getJson<GitLog>("${m(machineId)}/git/log", mapOf("cwd" to cwd, "skip" to "$skip", "limit" to "$limit")).commits

/**
 * Nodes send file and commit times with the machine's own offset ("2026-10-31T13:58:40+08:00"); `Instant.parse` on
 * older Android (Java 8's ISO_INSTANT) only takes "Z", so these go through OffsetDateTime.
 */
internal fun offsetTime(s: String?): Instant? {
    if (s.isNullOrEmpty() || s.startsWith("0001-")) return null
    return runCatching { java.time.OffsetDateTime.parse(s).toInstant() }.getOrNull()
}
