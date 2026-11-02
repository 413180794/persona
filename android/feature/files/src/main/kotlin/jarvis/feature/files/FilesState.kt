package jarvis.feature.files

import jarvis.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

enum class FilesPane { Files, Git }

sealed interface FileSelection {
    data class File(val path: String) : FileSelection
    data class Diff(val file: SessionGitFile, val staged: Boolean) : FileSelection
    data class Commit(val hash: String) : FileSelection
}

data class FilesState(
    val folders: List<String> = emptyList(),
    val directory: SessionDirectory? = null,
    val directoryLoading: Boolean = false,
    val directoryError: Boolean = false,
    val git: SessionGitStatus? = null,
    val gitLoading: Boolean = false,
    val gitError: Boolean = false,
    val older: List<SessionGitCommit> = emptyList(),
    val moreLoading: Boolean = false,
    val moreError: Boolean = false,
    val noMore: Boolean = false,
    val selection: FileSelection? = null,
    val content: SessionFileContent? = null,
    val diff: SessionGitDiff? = null,
    val commit: SessionGitShow? = null,
    val previewError: Boolean = false,
)

/** Owned by the session pane; leaving it cancels its coroutine scope. */
class FilesController(
    private val api: SessionFiles,
    private val machineId: String,
    val cwd: String,
    private val scope: CoroutineScope,
) {
    private val mutable = MutableStateFlow(FilesState())
    val state = mutable.asStateFlow()
    private var directoryJob: Job? = null
    private var gitJob: Job? = null
    private var previewJob: Job? = null
    private var moreJob: Job? = null
    private var directoryGeneration = 0
    private var gitGeneration = 0
    private var previewGeneration = 0

    fun directory(folders: List<String> = state.value.folders) {
        directoryJob?.cancel()
        val generation = ++directoryGeneration
        mutable.update { it.copy(folders = folders, directory = null, directoryError = false, directoryLoading = true) }
        directoryJob = scope.launch {
            try {
                val path = folders.fold(cwd, ::joinRemotePath)
                val result = api.directory(machineId, cwd, path)
                if (generation == directoryGeneration) mutable.update { it.copy(directory = result, directoryLoading = false) }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) {
                if (generation == directoryGeneration) mutable.update { it.copy(directoryLoading = false, directoryError = true) }
            }
        }
    }

    fun refreshGit() {
        gitJob?.cancel()
        moreJob?.cancel()
        val generation = ++gitGeneration
        mutable.update { it.copy(gitLoading = true, gitError = false, moreLoading = false, moreError = false) }
        gitJob = scope.launch {
            try {
                val result = api.status(machineId, cwd)
                if (generation == gitGeneration) mutable.update {
                    it.copy(git = result, gitLoading = false, older = emptyList(), noMore = result.commits.size < 30)
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) {
                if (generation == gitGeneration) mutable.update { it.copy(gitLoading = false, gitError = true) }
            }
        }
    }

    fun more() {
        val snapshot = state.value
        if (snapshot.moreLoading || snapshot.noMore || snapshot.gitLoading || snapshot.git == null) return
        val generation = gitGeneration
        mutable.update { it.copy(moreLoading = true, moreError = false) }
        moreJob = scope.launch {
            try {
                val result = api.log(machineId, cwd, snapshot.git.commits.size + snapshot.older.size)
                if (generation == gitGeneration) mutable.update {
                    val known = snapshot.git.commits.map { commit -> commit.hash }.toSet()
                    it.copy(older = (it.older + result.commits).distinctBy { commit -> commit.hash }.filterNot { commit -> commit.hash in known },
                        moreLoading = false, noMore = result.commits.size < 50)
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) {
                if (generation == gitGeneration) mutable.update { it.copy(moreLoading = false, moreError = true) }
            }
        }
    }

    fun preview(selection: FileSelection?) {
        previewJob?.cancel()
        val generation = ++previewGeneration
        mutable.update { it.copy(selection = selection, content = null, diff = null, commit = null, previewError = false) }
        if (selection == null) return
        previewJob = scope.launch {
            try {
                val result = when (selection) {
                    is FileSelection.File -> state.value.copy(content = api.file(machineId, cwd, selection.path))
                    is FileSelection.Diff -> state.value.copy(diff = api.diff(machineId, state.value.git?.root ?: cwd, selection.file.path, selection.staged))
                    is FileSelection.Commit -> state.value.copy(commit = api.show(machineId, state.value.git?.root ?: cwd, selection.hash))
                }
                if (generation == previewGeneration) mutable.update {
                    it.copy(content = result.content, diff = result.diff, commit = result.commit)
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) {
                if (generation == previewGeneration) mutable.update { it.copy(previewError = true) }
            }
        }
    }
}

fun joinRemotePath(parent: String, child: String): String = parent.trimEnd('/') + "/" + child

fun gitMark(status: SessionGitStatus?, path: String, directory: Boolean): String? {
    val root = status?.root?.trimEnd('/') ?: return null
    if (!path.startsWith("$root/")) return null
    val relative = path.removePrefix("$root/")
    return status.files.firstOrNull { it.path == relative || directory && it.path.startsWith("$relative/") }?.letter
}
