package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Read-only access to the machine hosting a session. */
interface SessionFiles {
    suspend fun directory(machineId: String, cwd: String, path: String): SessionDirectory
    suspend fun file(machineId: String, cwd: String, path: String): SessionFileContent
    suspend fun status(machineId: String, cwd: String): SessionGitStatus
    suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean): SessionGitDiff
    suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int = 50): SessionGitLog
    suspend fun show(machineId: String, cwd: String, hash: String): SessionGitShow
    /**
     * Which of the paths a conversation names are there on the machine (`GET …/stat`, relative ones in [cwd]): the
     * chat links only those. A source that cannot say throws, and the paths stay plain text.
     */
    suspend fun stat(machineId: String, cwd: String, paths: List<String>): SessionPathStats =
        throw UnsupportedOperationException("stat")
}

/** `GET /api/v1/machines/{id}/stat`: each path as asked, `file`, `dir`, `denied` (macOS keeps it from the node) or `missing`. */
@Serializable data class SessionPathStats(val stats: List<SessionPathStat> = emptyList())

@Serializable data class SessionPathStat(val path: String, val kind: String = "missing", val size: Long = 0)

@Serializable data class SessionDirectory(
    val path: String,
    val entries: List<SessionFileEntry> = emptyList(),
    val truncated: Boolean = false,
)

@Serializable data class SessionFileEntry(
    val name: String,
    val dir: Boolean = false,
    val symlink: Boolean = false,
    val size: Long = 0,
    val mtime: String? = null,
)

@Serializable data class SessionFileContent(
    val path: String,
    val name: String,
    val size: Long = 0,
    val mime: String? = null,
    val text: String? = null,
    val data: String? = null,
    val truncated: Boolean = false,
    @SerialName("is_dir") val isDir: Boolean = false,
    val entries: List<String> = emptyList(),
)

@Serializable data class SessionGitFile(
    val path: String,
    @SerialName("orig_path") val originalPath: String? = null,
    val index: String = ".",
    val worktree: String = ".",
    val added: Int = 0,
    val deleted: Int = 0,
    val binary: Boolean = false,
) {
    val untracked get() = index == "?"
    val conflicted get() = index == "U" || worktree == "U" || index + worktree in setOf("AA", "DD")
    val staged get() = !untracked && index !in listOf(".", " ", "")
    val unstaged get() = untracked || worktree !in listOf(".", " ", "")
    val letter get() = when { conflicted -> "!"; untracked -> "U"; staged -> index; else -> worktree }
}

@Serializable data class SessionGitCommit(
    val hash: String,
    val short: String = "",
    val subject: String = "",
    val body: String? = null,
    val author: String = "",
    val date: String? = null,
    val refs: String? = null,
)

@Serializable data class SessionGitStatus(
    @SerialName("is_repo") val isRepo: Boolean,
    val root: String? = null,
    val branch: String? = null,
    val head: SessionGitCommit? = null,
    val upstream: String? = null,
    val ahead: Int = 0,
    val behind: Int = 0,
    val stashes: Int = 0,
    val files: List<SessionGitFile> = emptyList(),
    val commits: List<SessionGitCommit> = emptyList(),
)

@Serializable data class SessionGitDiff(val path: String = "", val diff: String = "", val truncated: Boolean = false)
@Serializable data class SessionGitLog(val commits: List<SessionGitCommit> = emptyList())
@Serializable data class SessionGitShow(
    val commit: SessionGitCommit,
    val files: List<SessionGitFile> = emptyList(),
    val diff: String = "",
    val truncated: Boolean = false,
)
