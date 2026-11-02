package jarvis.feature.files

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import jarvis.core.contracts.api.*
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import jarvis.core.navigation.ChatRoute
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** Embedded in the chat destination; Files/Git are panes, not navigation entries. */
@Composable
fun SessionFilesScreen(
    session: ChatRoute,
    cwd: String,
    api: SessionFiles,
    onBackToChat: () -> Unit,
    onInsertPath: (String) -> Unit,
    initialPane: FilesPane = FilesPane.Files,
    modifier: Modifier = Modifier,
) {
    key(session, cwd, api) {
        if (cwd.isBlank()) {
            JListPage("文件", modifier, onBack = onBackToChat) {
                item { JEmptyState("还没有会话目录", "恢复会话后再打开文件。", action = { JButton("回到对话", onBackToChat) }) }
            }
        } else {
            val scope = rememberCoroutineScope()
            val controller = remember { FilesController(api, session.machineId, cwd, scope) }
            val state by controller.state.collectAsStateWithLifecycle()
            var pane by rememberSaveable { mutableStateOf(initialPane) }
            var hidden by rememberSaveable { mutableStateOf(false) }
            val context = LocalContext.current
            var menu by remember { mutableStateOf(false) }
            LaunchedEffect(controller) { controller.directory(); controller.refreshGit() }
            val back: () -> Unit = {
                when {
                    state.selection != null -> controller.preview(null)
                    pane == FilesPane.Files && state.folders.isNotEmpty() -> controller.directory(state.folders.dropLast(1))
                    else -> onBackToChat()
                }
            }
            BackHandler(onBack = back)
            val path = state.directory?.path ?: state.folders.fold(controller.cwd, ::joinRemotePath)
            if (state.selection != null) {
                Box(modifier.fillMaxSize()) { FilePreview(state, controller, onInsertPath, back) }
            } else {
                JListPage(
                    title = path.trimEnd('/').substringAfterLast('/').ifEmpty { "/" }, modifier = modifier,
                    subtitle = path, onBack = onBackToChat, largeTitle = true,
                    actions = { JIconButton(PhIcons.ArrowClockwise, "刷新", { controller.directory(); controller.refreshGit() },
                        enabled = !state.directoryLoading && !state.gitLoading) },
                ) {
                    item("panes") {
                        val options = if (state.git?.isRepo == false) listOf("对话", "文件") else listOf("对话", "文件", "Git")
                        JSegmented(options.map { JSegment(it) }, if (pane == FilesPane.Files) 1 else 2,
                            { when (it) { 0 -> onBackToChat(); 1 -> pane = FilesPane.Files; else -> pane = FilesPane.Git } },
                            Modifier.padding(vertical = 8.dp))
                    }
                    if (pane == FilesPane.Files) {
                        item("folders") {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                if (state.folders.isNotEmpty()) JIconButton(PhIcons.ArrowLeft, "上一级", { controller.directory(state.folders.dropLast(1)) })
                                LazyRow(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically) {
                                    item { JTextButton(controller.cwd.trimEnd('/').substringAfterLast('/').ifEmpty { "/" }, { controller.directory(emptyList()) }) }
                                    items(state.folders.size) { index ->
                                        JTextButton("› ${state.folders[index]}", { controller.directory(state.folders.take(index + 1)) })
                                    }
                                }
                                JIconButton(PhIcons.DotsThree, "文件选项", { menu = true })
                            }
                        }
                        filesBrowser(state, controller, hidden, onInsertPath)
                    } else gitBrowser(state, controller)
                }
                if (menu) JActionSheet(listOf(
                    JAction(if (hidden) "隐藏点文件" else "显示隐藏文件", checked = hidden) { hidden = !hidden },
                    JAction("拷贝目录") { copyPath(context, path) },
                    JAction("插入目录") { onInsertPath(path) },
                ), { menu = false }, title = "目录操作")
            }
        }
    }
}

private fun LazyListScope.filesBrowser(state: FilesState, controller: FilesController, hidden: Boolean, insert: (String) -> Unit) {
    when {
        state.directoryError -> item { RetryState("打不开这个文件夹", "请检查连接后重试。") { controller.directory() } }
        state.directoryLoading -> item { Loading() }
        state.directory != null -> {
            val listing = state.directory
            val entries = listing.entries.filter { hidden || !it.name.startsWith('.') }
                .sortedWith(compareByDescending<SessionFileEntry> { it.dir }.thenBy { it.name.lowercase() })
            if (entries.isEmpty()) item { JEmptyState(if (listing.entries.isEmpty()) "空文件夹" else "只有隐藏文件", "可以返回上一级或刷新目录。") }
            itemsIndexed(entries, key = { _, it -> "entry:${it.name}" }, contentType = { _, _ -> "entry" }) { index, entry ->
                val context = LocalContext.current
                val path = joinRemotePath(listing.path, entry.name)
                JGroupItem(GroupPosition.of(index, entries.size)) {
                    FileRow(entry, gitMark(state.git, path, entry.dir),
                        open = { if (entry.dir) controller.directory(state.folders + entry.name) else controller.preview(FileSelection.File(path)) },
                        copy = { copyPath(context, path) }, insert = { insert(path) })
                }
            }
            if (listing.truncated) item { Notice("目录条目较多，只显示了前面部分。") }
        }
    }
}

@Composable
private fun FileRow(entry: SessionFileEntry, mark: String?, open: () -> Unit, copy: () -> Unit, insert: () -> Unit) {
    var menu by remember { mutableStateOf(false) }
    val textFile = entry.name.substringAfterLast('.', "").lowercase() in setOf("kt", "go", "md", "txt", "json", "ts", "js", "py", "yaml", "yml", "xml", "swift")
    JRow(entry.name, subtitle = listOfNotNull(if (entry.dir) "文件夹" else fileSize(entry.size), entry.mtime?.let(::fileDate),
        if (entry.symlink) "符号链接" else null).joinToString("  "),
        leadingIcon = when { entry.dir -> PhIcons.FolderSimple; textFile -> PhIcons.FileText; else -> PhIcons.File },
        onClick = open, onLongClick = { menu = true }, trailing = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (mark != null) JTag(if (entry.dir) "有改动" else mark, status = Status.NeedsYou)
                JIconButton(PhIcons.DotsThree, "${entry.name} 的操作", { menu = true })
            }
        })
    if (menu) JActionSheet(listOf(JAction("插入路径", onClick = insert), JAction("拷贝路径", onClick = copy)), { menu = false }, entry.name)
}

private fun LazyListScope.gitBrowser(state: FilesState, controller: FilesController) {
    val git = state.git
    when {
        state.gitError && git == null -> item { RetryState("读不到 Git 状态", "请检查连接后重试。", controller::refreshGit) }
        git == null -> item { Loading() }
        !git.isRepo -> item { JEmptyState("不是 Git 仓库", "这个会话目录没有 Git 仓库。") }
        else -> {
            if (state.gitError) item { RetryState("刷新失败", "下方保留上次读取的状态。", controller::refreshGit) }
            if (state.gitLoading) item { JLinearProgress(null, Modifier.fillMaxWidth()) }
            item {
                JSectionHeader(git.branch ?: "分离的 HEAD")
                JGroupItem(GroupPosition.Single) {
                    JRow(git.upstream ?: "没有上游分支", subtitle = git.root,
                        value = "↑${git.ahead}  ↓${git.behind}  贮藏 ${git.stashes}")
                }
                JSectionHeader("改动", count = git.files.size)
            }
            if (git.files.isEmpty()) item { JEmptyState("工作区是干净的", "当前没有未提交的改动。") }
            itemsIndexed(git.files, key = { _, it -> "file:${it.path}" }, contentType = { _, _ -> "change" }) { index, file ->
                val status = if (file.conflicted) Status.NeedsYou else if (file.staged) Status.Unread else Status.Unknown
                JGroupItem(GroupPosition.of(index, git.files.size)) {
                    JRow(file.path, subtitle = file.originalPath?.let { "原路径：$it" }, leadingIcon = PhIcons.FileText,
                        onClick = { controller.preview(FileSelection.Diff(file, file.staged && !file.unstaged)) }, trailing = {
                            Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                    JTag(file.letter, status = status)
                                    JTag(when { file.conflicted -> "有冲突"; file.untracked -> "未跟踪"; file.staged && file.unstaged -> "部分已暂存"; file.staged -> "已暂存"; else -> "未暂存" }, status = status)
                                }
                                if (file.binary) OneLine("二进制") else Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    Text("+${file.added}", style = JarvisTheme.type.micro, color = JarvisTheme.palette.diffAdd)
                                    Text("−${file.deleted}", style = JarvisTheme.type.micro, color = JarvisTheme.palette.diffDel)
                                }
                            }
                        })
                }
            }
            item { JSectionHeader("提交历史") }
            val commits = git.commits + state.older
            itemsIndexed(commits, key = { _, it -> "commit:${it.hash}" }, contentType = { _, _ -> "commit" }) { index, commit ->
                JGroupItem(GroupPosition.of(index, commits.size)) {
                    JRow(commit.subject, subtitle = listOfNotNull(commit.short.ifEmpty { commit.hash.take(8) }, commit.author, commit.date?.let(::fileDate)).joinToString("  "),
                        onClick = { controller.preview(FileSelection.Commit(commit.hash)) })
                }
            }
            if (git.commits.isEmpty()) item { Notice("还没有提交") }
            if (!state.noMore) item {
                JTextButton(when { state.moreLoading -> "正在加载…"; state.moreError -> "加载失败，重试"; else -> "加载更早的提交" },
                    controller::more, Modifier.fillMaxWidth(), enabled = !state.moreLoading && !state.gitLoading)
            }
        }
    }
}

@Composable internal fun OneLine(text: String) { Text(text, style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2, maxLines = 1, softWrap = false, overflow = TextOverflow.Ellipsis) }
@Composable internal fun Notice(text: String) { Text(text, Modifier.padding(horizontal = 16.dp, vertical = 10.dp), style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2) }
@Composable internal fun Loading() { JLoading() }
@Composable internal fun RetryState(title: String, message: String, retry: () -> Unit) {
    JEmptyState(title, message, action = { JButton("重试", retry, kind = JButtonKind.Secondary) })
}
internal fun copyPath(context: Context, text: String) {
    (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("文件", text))
}
internal fun fileSize(size: Long): String = when { size < 1024 -> "$size B"; size < 1024 * 1024 -> "${size / 1024} KB"; else -> "${size / (1024 * 1024)} MB" }
internal fun fileDate(value: String): String = try {
    DateTimeFormatter.ofPattern("MM-dd HH:mm").withZone(ZoneId.systemDefault()).format(Instant.parse(value))
} catch (_: Exception) { value }
