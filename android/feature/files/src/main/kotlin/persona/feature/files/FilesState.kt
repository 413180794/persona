package persona.feature.files

import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

enum class FilesPane { Files, Git }

/** A page shown over the browser. Pages stack: a commit's file opens over the commit, a linked file over its README. */
sealed interface FileSelection {
    data class File(val path: String) : FileSelection
    data class Diff(val file: SessionGitFile, val staged: Boolean) : FileSelection
    data class Commit(val hash: String) : FileSelection
    /** One file of a commit: its part of the commit's diff (the commit is read once and kept). */
    data class CommitFile(val hash: String, val file: SessionGitFile) : FileSelection
}

data class FilesState(
    /** The top of the breadcrumb: the session's folder, or above it once the user went up. Absolute once listed. */
    val root: String = "",
    /** The folder the browser shows. */
    val here: String = "",
    val directory: SessionDirectory? = null,
    val directoryLoading: Boolean = false,
    val directoryError: String? = null,
    val git: SessionGitStatus? = null,
    val gitLoading: Boolean = false,
    val gitError: Boolean = false,
    val older: List<SessionGitCommit> = emptyList(),
    val moreLoading: Boolean = false,
    val moreError: Boolean = false,
    val noMore: Boolean = false,
    val stack: List<FileSelection> = emptyList(),
    /** The pages were opened straight from the conversation (a path in a message): back returns there. */
    val direct: Boolean = false,
    val content: SessionFileContent? = null,
    val diff: SessionGitDiff? = null,
    val commit: SessionGitShow? = null,
    val previewError: String? = null,
) {
    val selection: FileSelection? get() = stack.lastOrNull()
    /** The browser is in a folder below its top: back goes up a level before it leaves. */
    val belowRoot: Boolean get() = here.isNotEmpty() && here.trimEnd('/') != root.trimEnd('/')
    val crumbs: List<Pair<String, String>> get() = FilesPaths.crumbs(root, here)
    /** The folder above the browser's, if any: the ⋯ menu's 上一级文件夹, which may go above the session's folder. */
    val parent: String? get() = FilesPaths.parent(here)
}

/** Owned by the session pane; leaving it cancels its coroutine scope. */
class FilesController(
    private val api: SessionFiles,
    private val machineId: String,
    val cwd: String,
    private val scope: CoroutineScope,
    /** Why a load failed, in words for the page (the hub's message when the machine said why). */
    private val describe: (Throwable) -> String = { "请检查连接后重试。" },
) {
    private val mutable = MutableStateFlow(FilesState(root = cwd, here = cwd))
    val state = mutable.asStateFlow()
    private var directoryJob: Job? = null
    private var gitJob: Job? = null
    private var previewJob: Job? = null
    private var moreJob: Job? = null
    private var directoryGeneration = 0
    private var gitGeneration = 0
    private var previewGeneration = 0
    /** Folders already listed: going back up shows them at once while they refresh. */
    private val listings = LinkedHashMap<String, SessionDirectory>()
    /** Files shown lately: back to a page under the one on top (a README under the file it links) shows it at once. */
    private val contents = object : LinkedHashMap<String, SessionFileContent>(4, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, SessionFileContent>?) = size > 4
    }
    /** `git show` by hash: a commit's files open their part of the diff without asking again. */
    private val shows = object : LinkedHashMap<String, SessionGitShow>(8, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, SessionGitShow>?) = size > 8
    }

    /**
     * The first load. With [initialPath] (a path from the conversation: absolute, "~…", or relative to the session's
     * folder) a file opens straight away and back returns to the conversation; a folder (a trailing slash, or the hub
     * saying it is one) opens in the browser as its top.
     */
    fun start(initialPath: String? = null) {
        refreshGit()
        val asked = initialPath?.trim()?.takeIf { it.isNotEmpty() }
        when {
            asked == null -> directory()
            FilesPaths.namesDirectory(asked) -> browse(FilesPaths.resolve(asked, cwd), top = true)
            else -> {
                mutable.update { it.copy(direct = true) }
                preview(FileSelection.File(FilesPaths.resolve(asked, cwd)))
            }
        }
    }

    /** Lists [path] (the folder shown by default); keeps showing what was there before while it reloads. */
    fun directory(path: String = state.value.here) {
        directoryJob?.cancel()
        val generation = ++directoryGeneration
        mutable.update {
            it.copy(here = path, directory = listings[path], directoryError = null, directoryLoading = true)
        }
        directoryJob = scope.launch {
            try {
                val result = api.directory(machineId, cwd, path)
                if (generation != directoryGeneration) return@launch
                listings[path] = result
                if (result.path.isNotEmpty()) listings[result.path] = result
                mutable.update {
                    // The hub answers with the folder it read ("~" expanded, "a/../b" cleaned): the breadcrumb follows.
                    val resolved = result.path.ifEmpty { path }
                    it.copy(directory = result, directoryLoading = false, here = resolved,
                        root = if (it.root == path) resolved else it.root)
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) {
                if (generation == directoryGeneration) mutable.update { it.copy(directoryLoading = false, directoryError = describe(error)) }
            }
        }
    }

    /** Into a folder: below the top it extends the breadcrumb; anywhere else it becomes the top. */
    fun open(path: String) = browse(path, top = !FilesPaths.isWithin(path, state.value.root))

    private fun browse(path: String, top: Boolean) {
        mutable.update {
            it.copy(root = if (top) path else it.root, stack = emptyList(), direct = false,
                content = null, diff = null, commit = null, previewError = null)
        }
        previewJob?.cancel()
        directory(path)
    }

    /** One level up: the breadcrumb's top moves up too when the browser is already there (as far as the root). */
    fun up() {
        val now = state.value
        val parent = now.parent ?: return
        if (now.belowRoot) directory(parent) else browse(parent, top = true)
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

    /** Shows [selection] as the only page over the browser; null closes the pages. */
    fun preview(selection: FileSelection?) = show(listOfNotNull(selection))

    /** Opens [selection] over the page shown now. */
    fun push(selection: FileSelection) = show(state.value.stack + selection)

    /** Reloads the page shown now. */
    fun reload() = show(state.value.stack, fresh = true)

    /**
     * Back: closes the page on top (to the conversation when it was opened from there), then goes up to the
     * breadcrumb's top. False when there is nothing left to go back to here: the caller returns to the conversation.
     */
    fun back(pane: FilesPane = FilesPane.Files): Boolean {
        val now = state.value
        return when {
            now.stack.size > 1 -> { show(now.stack.dropLast(1), fresh = false); true }
            now.stack.size == 1 -> if (now.direct) false else { show(emptyList()); true }
            pane == FilesPane.Files && now.belowRoot -> { now.parent?.let(::directory); true }
            else -> false
        }
    }

    /** Shows the top of [stack]; a file seen lately shows as it was unless [fresh]. */
    private fun show(stack: List<FileSelection>, fresh: Boolean = true) {
        previewJob?.cancel()
        val generation = ++previewGeneration
        val top = stack.lastOrNull()
        val kept = (top as? FileSelection.File)?.takeIf { !fresh }?.let { contents[it.path] }
        mutable.update { it.copy(stack = stack, content = kept, diff = null, commit = null, previewError = null) }
        if (top == null || kept != null) return
        val gitRoot = state.value.git?.root ?: cwd
        previewJob = scope.launch {
            try {
                when (top) {
                    is FileSelection.File -> {
                        val file = api.file(machineId, cwd, top.path)
                        if (generation != previewGeneration) return@launch
                        // A folder opens in the browser: opened from the conversation, as the breadcrumb's top (back
                        // returns to the conversation from there); from a page here, where the browser can reach it.
                        val folder = file.path.ifEmpty { top.path }
                        when {
                            !file.isDir -> { contents[top.path] = file; mutable.update { it.copy(content = file) } }
                            state.value.direct -> browse(folder, top = true)
                            else -> open(folder)
                        }
                    }
                    is FileSelection.Diff -> {
                        val diff = api.diff(machineId, gitRoot, top.file.path, top.staged)
                        if (generation == previewGeneration) mutable.update { it.copy(diff = diff) }
                    }
                    is FileSelection.Commit -> {
                        val show = commit(gitRoot, top.hash)
                        if (generation == previewGeneration) mutable.update { it.copy(commit = show) }
                    }
                    is FileSelection.CommitFile -> {
                        val show = commit(gitRoot, top.hash)
                        if (generation == previewGeneration) mutable.update { it.copy(commit = show) }
                    }
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) {
                if (generation == previewGeneration) mutable.update { it.copy(previewError = describe(error)) }
            }
        }
    }

    private suspend fun commit(root: String, hash: String): SessionGitShow =
        synchronized(shows) { shows[hash] } ?: api.show(machineId, root, hash).also { synchronized(shows) { shows[hash] = it } }
}

fun gitMark(status: SessionGitStatus?, path: String, directory: Boolean): String? = gitChange(status, path, directory)?.letter

/** The change of a file, or for a folder the first change inside it; null when unchanged or outside the repository. */
internal fun gitChange(status: SessionGitStatus?, path: String, directory: Boolean): SessionGitFile? {
    val root = status?.root?.trimEnd('/') ?: return null
    if (!path.startsWith("$root/")) return null
    val relative = path.removePrefix("$root/").trimEnd('/')
    if (!directory) return status.files.firstOrNull { it.path == relative }
    // A folder shows its first change, or a conflict inside it when there is one.
    val inside = status.files.filter { it.path.startsWith("$relative/") }
    return inside.firstOrNull { it.conflicted } ?: inside.firstOrNull()
}
