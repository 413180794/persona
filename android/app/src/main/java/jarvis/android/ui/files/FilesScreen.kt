package jarvis.android.ui.files

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateList
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.Hub
import jarvis.android.data.JsonCodec
import jarvis.android.ui.media.ImageViewerArgs
import jarvis.android.ui.media.MediaShare
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** A page pushed inside the files screen (its own back stack: the browser stays where it was under a file or a diff). */
@Serializable
sealed interface FilesRoute {
    @Serializable @SerialName("file") data class File(val path: String) : FilesRoute
    @Serializable @SerialName("diff") data class Diff(val file: GitFile, val staged: Boolean) : FilesRoute
    @Serializable @SerialName("commit") data class Commit(val commit: GitCommit) : FilesRoute
    /** One file of a commit; its diff comes from the (cached) `git/show` of [hash]. */
    @Serializable @SerialName("commit_file") data class CommitFile(val hash: String, val file: GitFile) : FilesRoute
}

/** The pushed pages survive leaving for the image viewer and rotation (as JSON strings). */
private val RouteStackSaver = listSaver<SnapshotStateList<FilesRoute>, String>(
    save = { list -> list.map { JsonCodec.encodeToString(FilesRoute.serializer(), it) } },
    restore = { saved ->
        mutableStateListOf<FilesRoute>().apply {
            addAll(saved.mapNotNull { runCatching { JsonCodec.decodeFromString(FilesRoute.serializer(), it) }.getOrNull() })
        }
    },
)

/**
 * Files and Git of a machine (iOS `FilesPane` / `GitPane` / `FileViewer`), read-only, from [target]'s path ("~", an
 * absolute path, or relative to its cwd). A directory shows the browser, with a Git half
 * (文件 · Git) when it is in a repository; a file shows its viewer. Files, diffs and commits open on top; back
 * returns to where you were, then up the folders, then leaves.
 */
@Composable
fun FilesScreen(hub: Hub, target: FilesTarget, onBack: () -> Unit, onOpenImage: (ImageViewerArgs) -> Unit) {
    val machineId = target.machineId
    val path = target.path
    val mode = target.resolvedMode
    val cwd = target.cwd
    val machines by hub.machines.collectAsState()
    val machineName = machines.firstOrNull { it.id == machineId }?.displayName ?: machineId
    val stack = rememberSaveable(saver = RouteStackSaver) { mutableStateListOf() }
    var tab by rememberSaveable { mutableIntStateOf(if (mode == FilesTarget.Mode.GIT) 1 else 0) }
    // The browser's top folder and where it is now; both become absolute once the hub has listed them.
    var root by rememberSaveable { mutableStateOf(path) }
    var here by rememberSaveable { mutableStateOf(path) }
    // Git runs in the folder asked for (the hub expands "~"); relative paths are in the caller's cwd.
    val gitCwd = remember(path, cwd) {
        if (!path.startsWith("/") && !path.startsWith("~") && !cwd.isNullOrEmpty()) FilesPaths.join(cwd, path) else path
    }
    var git by remember { mutableStateOf<GitStatus?>(null) }
    var gitError by remember { mutableStateOf<String?>(null) }
    var gitReload by remember { mutableIntStateOf(0) }

    if (mode != FilesTarget.Mode.FILE) {
        LaunchedEffect(gitCwd, gitReload) {
            runCatching { hub.api.gitStatus(machineId, gitCwd) }
                .onSuccess { git = it; gitError = null }
                .onFailure { if (git == null) gitError = it.shown() }
        }
    }

    val goBack: () -> Unit = {
        when {
            stack.isNotEmpty() -> stack.removeAt(stack.lastIndex)
            mode != FilesTarget.Mode.FILE && tab == 0 && here != root ->
                here = FilesPaths.parent(here)?.takeIf { it.startsWith(root) } ?: root
            else -> onBack()
        }
    }
    BackHandler(onBack = goBack)

    when (val top = stack.lastOrNull()) {
        is FilesRoute.File -> FileViewer(hub, machineId, top.path, cwd ?: root, machineName, onBack = goBack, onOpen = { stack += FilesRoute.File(it) }, onOpenImage = onOpenImage)
        is FilesRoute.Diff -> GitDiffPage(hub, machineId, gitRoot(git, gitCwd), top.file, top.staged, onBack = goBack)
        is FilesRoute.Commit -> GitCommitPage(
            hub, machineId, gitRoot(git, gitCwd), top.commit, onBack = goBack,
            onFile = { f -> stack += FilesRoute.CommitFile(top.commit.hash, f) },
        )
        is FilesRoute.CommitFile -> CommitFilePage(hub, machineId, gitRoot(git, gitCwd), top.hash, top.file, onBack = goBack)
        null -> if (mode == FilesTarget.Mode.FILE) {
            FileViewer(hub, machineId, path, cwd, machineName, onBack = onBack, onOpen = { stack += FilesRoute.File(it) }, onOpenImage = onOpenImage)
        } else {
            val repo = git?.isRepo == true
            val ctx = LocalContext.current
            FilesPage(
                title = if (tab == 0) "文件" else "Git",
                subtitle = machineName + " · " + (if (tab == 0) here else git?.root ?: gitCwd),
                onBack = goBack,
                actions = {
                    if (tab == 0) BrowserMenu(ctx, here, canGoUp = here == root && FilesPaths.parent(root) != null) {
                        FilesPaths.parent(root)?.let { root = it; here = it }
                    }
                },
            ) { pad ->
                Column(Modifier.fillMaxSize().padding(pad)) {
                    if (repo || mode == FilesTarget.Mode.GIT) {
                        val n = git?.files?.size ?: 0
                        Segments(listOf("文件", if (n > 0) "Git（$n）" else "Git"), tab, { tab = it })
                    }
                    if (tab == 0) {
                        Browser(
                            hub, machineId, root, here, cwd, git,
                            onResolved = { asked, resolved -> if (asked == root) { root = resolved; here = resolved } },
                            onDir = { here = it },
                            onFile = { stack += FilesRoute.File(it) },
                        )
                    } else {
                        GitPane(
                            hub, machineId, gitRoot(git, gitCwd), git, gitError, onRefresh = { gitError = null; gitReload++ },
                            onFile = { f -> stack += FilesRoute.Diff(f, f.isStaged && !f.hasUnstaged) },
                            onCommit = { c -> stack += FilesRoute.Commit(c) },
                        )
                    }
                }
            }
        }
    }
}

/** Where Git calls run: the repository root once known. */
private fun gitRoot(git: GitStatus?, fallback: String) = git?.root?.takeIf { it.isNotEmpty() } ?: fallback

private object ShowHidden {
    var value by mutableStateOf<Boolean?>(null)
    fun get(ctx: Context): Boolean = value ?: ctx.getSharedPreferences("files", Context.MODE_PRIVATE).getBoolean("showHidden", false).also { value = it }
    fun set(ctx: Context, on: Boolean) {
        value = on
        ctx.getSharedPreferences("files", Context.MODE_PRIVATE).edit().putBoolean("showHidden", on).apply()
    }
}

@Composable
private fun BrowserMenu(ctx: Context, here: String, canGoUp: Boolean, onUp: () -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { open = true }) { Icon(Icons.Default.MoreVert, contentDescription = "更多") }
        DropdownMenu(open, onDismissRequest = { open = false }) {
            val hidden = ShowHidden.get(ctx)
            DropdownMenuItem(
                text = { Text(if (hidden) "不显示隐藏文件" else "显示隐藏文件", maxLines = 1) },
                onClick = { ShowHidden.set(ctx, !hidden); open = false },
            )
            DropdownMenuItem(text = { Text("拷贝路径", maxLines = 1) }, onClick = {
                MediaShare.copyText(ctx, here, "路径"); MediaShare.toast(ctx, "已拷贝路径"); open = false
            })
            if (canGoUp) DropdownMenuItem(text = { Text("上一级文件夹", maxLines = 1) }, onClick = { onUp(); open = false })
        }
    }
}

@Composable
private fun Browser(
    hub: Hub,
    machineId: String,
    root: String,
    here: String,
    cwd: String?,
    git: GitStatus?,
    onResolved: (asked: String, resolved: String) -> Unit,
    onDir: (String) -> Unit,
    onFile: (String) -> Unit,
) {
    val ctx = LocalContext.current
    // Listings already seen: going back up a level shows at once, then refreshes.
    val listings = remember { mutableStateMapOf<String, FsListing>() }
    var error by remember(here) { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(here, reload) {
        error = null
        runCatching { hub.api.listDir(machineId, here, cwd) }
            .onSuccess {
                listings[here] = it
                if (it.path.isNotEmpty() && it.path != here) {
                    listings[it.path] = it
                    onResolved(here, it.path)
                }
            }
            .onFailure { error = it.shown() }
    }
    Column(Modifier.fillMaxSize()) {
        Breadcrumb(FilesPaths.crumbs(root, here), onPick = onDir)
        HorizontalDivider()
        val l = listings[here]
        when {
            l != null -> {
                val showHidden = ShowHidden.get(ctx)
                val entries = l.entries.filter { showHidden || !it.name.startsWith(".") }
                val dir = l.path.ifEmpty { here }
                if (entries.isEmpty()) {
                    Notice(Icons.Outlined.Folder, if (l.entries.isEmpty()) "空文件夹" else "只有隐藏文件")
                } else {
                    val now = remember(l) { java.time.Instant.now() }
                    LazyColumn(Modifier.fillMaxSize()) {
                        items(entries, key = { it.name }) { e ->
                            val full = FilesPaths.join(dir, e.name)
                            EntryRow(e, FilesPaths.gitMark(git, full, e.dir), now) { if (e.dir) onDir(full) else onFile(full) }
                        }
                        if (l.truncated) item {
                            Text(
                                "文件太多，只列出了前 2000 个", style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp),
                            )
                        }
                    }
                }
            }
            error != null -> ErrorNotice("打不开这个文件夹", error!!, onRetry = { reload++ })
            else -> Loading()
        }
    }
}

@Composable
private fun Breadcrumb(levels: List<Pair<String, String>>, onPick: (String) -> Unit) {
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        levels.forEachIndexed { i, (name, full) ->
            if (i > 0) Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = MaterialTheme.colorScheme.outline, modifier = Modifier.size(16.dp))
            val current = i == levels.lastIndex
            Text(
                name,
                style = MaterialTheme.typography.labelLarge,
                fontWeight = if (current) FontWeight.SemiBold else FontWeight.Normal,
                color = if (current) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                modifier = Modifier.background(if (current) MaterialTheme.colorScheme.surfaceVariant else Color.Transparent, CircleShape)
                    .clickable(enabled = !current) { onPick(full) }
                    .padding(horizontal = 10.dp, vertical = 5.dp),
            )
        }
    }
}

@Composable
private fun EntryRow(e: FsEntry, mark: String?, now: java.time.Instant, onClick: () -> Unit) {
    val hidden = e.name.startsWith(".")
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(
            fileIcon(e.name, e.dir), null,
            tint = if (e.dir) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(22.dp),
        )
        Column(Modifier.weight(1f)) {
            Text(
                e.name + if (e.symlink) " ↗" else "", fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                color = if (hidden) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
            )
            val meta = listOfNotNull(if (e.dir) null else FilesPaths.formatBytes(e.size), e.modified?.let { FilesPaths.relative(it, now) })
            if (meta.isNotEmpty()) {
                Text(meta.joinToString("  "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            }
        }
        if (mark != null) {
            if (e.dir) Box(Modifier.size(7.dp).background(gitColor(mark), RoundedCornerShape(50)))
            else GitBadge(mark)
        }
        if (e.dir) Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = MaterialTheme.colorScheme.outline, modifier = Modifier.size(18.dp))
    }
}
