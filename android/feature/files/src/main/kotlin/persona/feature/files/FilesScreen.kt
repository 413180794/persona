package persona.feature.files

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.ChatRoute
import java.time.Instant

/**
 * Embedded in the chat destination; Files/Git are panes, not navigation entries.
 *
 * [initialPath] opens a path the conversation mentioned (a file label, a tool's path, a diff's file header): relative
 * paths are in [cwd], "~…" goes to the hub as it is. A file opens straight in its preview and back returns to the
 * conversation; a folder (a trailing slash, or the hub answering `is_dir`) opens in the browser as its top.
 */
@Composable
fun SessionFilesScreen(
    session: ChatRoute,
    cwd: String,
    api: SessionFiles,
    onBackToChat: () -> Unit,
    onInsertPath: (String) -> Unit,
    initialPane: FilesPane = FilesPane.Files,
    modifier: Modifier = Modifier,
    initialPath: String? = null,
) {
    val asked = initialPath?.trim()?.takeIf { it.isNotEmpty() }
    // A path from the conversation that names its machine fully can open before the session's folder is known.
    val base = cwd.ifBlank { asked?.takeIf { it.startsWith("/") || FilesPaths.isHome(it) }?.let { start ->
        if (FilesPaths.namesDirectory(start)) FilesPaths.resolve(start, "/") else FilesPaths.parent(FilesPaths.resolve(start, "/")) ?: "/"
    }.orEmpty() }
    key(session, base, api, asked) {
        if (base.isBlank()) {
            JListPage("文件", modifier, onBack = onBackToChat) {
                item { JEmptyState("还没有会话目录", "恢复会话后再打开文件。", action = { JButton("回到对话", onBackToChat) }) }
            }
        } else {
            val scope = rememberCoroutineScope()
            val controller = remember { FilesController(api, session.machineId, base, scope, ::filesLoadError) }
            val images = remember { MarkdownImages(api, session.machineId, base) }
            val state by controller.state.collectAsStateWithLifecycle()
            var pane by rememberSaveable { mutableStateOf(if (asked != null) FilesPane.Files else initialPane) }
            LaunchedEffect(controller) { controller.start(asked) }
            val back: () -> Unit = { if (!controller.back(pane)) onBackToChat() }
            BackHandler(onBack = back)
            Box(modifier.fillMaxSize()) {
                when (val top = state.selection) {
                    null -> FilesBrowser(state, controller, pane, { pane = it }, onBackToChat, onInsertPath)
                    is FileSelection.Commit -> CommitPage(top.hash, state, controller, back)
                    is FileSelection.CommitFile -> CommitFilePage(top, state, controller, back)
                    else -> FilePreview(state, controller, images, onInsertPath, back)
                }
            }
        }
    }
}

@Composable
private fun FilesBrowser(
    state: FilesState,
    controller: FilesController,
    pane: FilesPane,
    onPane: (FilesPane) -> Unit,
    onBackToChat: () -> Unit,
    onInsertPath: (String) -> Unit,
) {
    val context = LocalContext.current
    val feedback = LocalAppFeedback.current
    var hidden by rememberShowHidden()
    var menu by remember { mutableStateOf(false) }
    val here = state.here
    JListPage(
        title = FilesPaths.name(here), subtitle = here, onBack = onBackToChat, largeTitle = true,
        actions = { JIconButton(PhIcons.ArrowClockwise, "刷新", { controller.directory(); controller.refreshGit() },
            enabled = !state.directoryLoading && !state.gitLoading) },
    ) {
        item("panes") {
            val changes = state.git?.files?.size?.takeIf { it > 0 }
            // Git stays offered while its status loads (the chat menu only opens it for repositories); it goes when there is none.
            val options = if (state.git?.isRepo == false) listOf(JSegment("对话"), JSegment("文件"))
                else listOf(JSegment("对话"), JSegment("文件"), JSegment("Git", count = changes))
            JSegmented(options, if (pane == FilesPane.Files || options.size < 3) 1 else 2,
                { when (it) { 0 -> onBackToChat(); 1 -> onPane(FilesPane.Files); else -> onPane(FilesPane.Git) } },
                Modifier.padding(vertical = Tokens.Space.s))
        }
        if (pane == FilesPane.Files || state.git?.isRepo == false) {
            item("crumbs") {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (state.belowRoot) JIconButton(PhIcons.ArrowUp, "上一级", controller::up)
                    Crumbs(state.crumbs, onPick = { controller.directory(it) }, modifier = Modifier.weight(1f))
                    JIconButton(PhIcons.DotsThree, "文件选项", { menu = true })
                }
            }
            filesBrowser(state, controller, hidden, onInsertPath)
        } else gitBrowser(state, controller)
    }
    if (menu) JActionSheet(buildList {
        add(JAction("显示隐藏文件", PhIcons.Eye, checked = hidden) { hidden = !hidden })
        state.parent?.let { add(JAction("上一级文件夹", PhIcons.ArrowUp) { controller.up() }) }
        add(JAction("拷贝目录", PhIcons.Copy) { copyText(context, here, feedback, "已拷贝路径") })
        add(JAction("插入目录", PhIcons.CursorText) { onInsertPath(here) })
    }, { menu = false }, title = "目录操作")
}

/** The folders from the breadcrumb's top down to the one shown, as chips; it follows you to the end as you go in. */
@Composable
private fun Crumbs(levels: List<Pair<String, String>>, onPick: (String) -> Unit, modifier: Modifier) {
    val list = rememberLazyListState()
    LaunchedEffect(levels.size, levels.lastOrNull()) { if (levels.isNotEmpty()) list.animateScrollToItem(levels.lastIndex) }
    LazyRow(modifier, list, verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        itemsIndexed(levels, key = { _, (_, path) -> path }) { index, (name, path) ->
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                if (index > 0) JIcon(PhIcons.CaretRight, null, size = Tokens.Space.m, tint = PersonaTheme.palette.ink3)
                val current = index == levels.lastIndex
                JChip(name, selected = current, onClick = { if (!current) onPick(path) })
            }
        }
    }
}

private fun LazyListScope.filesBrowser(state: FilesState, controller: FilesController, hidden: Boolean, insert: (String) -> Unit) {
    val listing = state.directory
    when {
        listing == null && state.directoryError != null -> item { RetryState("打不开这个文件夹", state.directoryError) { controller.directory() } }
        listing == null -> item { Loading() }
        else -> {
            if (state.directoryError != null) item("stale") { Notice("刷新失败：${state.directoryError}", padded = false) }
            val entries = listing.entries.filter { hidden || !it.name.startsWith('.') }
                .sortedWith(compareByDescending<SessionFileEntry> { it.dir }.thenBy { it.name.lowercase() })
            if (entries.isEmpty()) item {
                if (listing.entries.isEmpty()) JEmptyState("空文件夹", "这个文件夹里什么都没有。")
                else JEmptyState("只有隐藏文件", "在路径右边的 ⋯ 里打开「显示隐藏文件」就能看到。")
            }
            val now = Instant.now()
            itemsIndexed(entries, key = { _, it -> "entry:${it.name}" }, contentType = { _, _ -> "entry" }) { index, entry ->
                val context = LocalContext.current
                val feedback = LocalAppFeedback.current
                val path = joinRemotePath(listing.path.ifEmpty { state.here }, entry.name)
                JGroupItem(GroupPosition.of(index, entries.size)) {
                    FileRow(entry, gitChange(state.git, path, entry.dir), now,
                        open = { if (entry.dir) controller.directory(path) else controller.push(FileSelection.File(path)) },
                        copy = { copyText(context, path, feedback, "已拷贝路径") }, insert = { insert(path) })
                }
            }
            if (listing.truncated) item { Notice("文件太多，只列出了前 2000 个", padded = false) }
        }
    }
}

/** One entry: its kind, the name (a hidden one quieter), size and when it changed, the Git change, ⋯. */
@Composable
private fun FileRow(entry: SessionFileEntry, change: SessionGitFile?, now: Instant, open: () -> Unit, copy: () -> Unit, insert: () -> Unit) {
    var menu by remember { mutableStateOf(false) }
    val palette = PersonaTheme.palette
    val meta = listOfNotNull(if (entry.dir) "文件夹" else fileSize(entry.size), fileDate(entry.mtime, now), if (entry.symlink) "符号链接" else null)
    JRow(onClick = open, onLongClick = { menu = true }, trailing = {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (change != null) {
                if (entry.dir) JTag("有改动", status = if (change.conflicted) Status.NeedsYou else Status.Unknown) else GitLetter(change)
            }
            JIconButton(PhIcons.DotsThree, "${entry.name} 的操作", { menu = true })
        }
    }) {
        JIcon(if (entry.dir) PhIcons.FolderSimple else fileIcon(entry.name), null, tint = palette.ink2, decorative = true)
        Spacer(Modifier.width(Tokens.Space.l))
        Column(Modifier.weight(1f)) {
            Text(entry.name, style = PersonaTheme.type.body, color = if (entry.name.startsWith('.')) palette.ink2 else palette.ink,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(meta.joinToString("  "), style = PersonaTheme.type.caption.copy(fontWeight = FontWeight.Normal), color = palette.ink2,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
    if (menu) JActionSheet(listOf(JAction("插入路径", PhIcons.CursorText, onClick = insert), JAction("拷贝路径", PhIcons.Copy, onClick = copy)),
        { menu = false }, entry.name)
}

/** The icon of a file by its kind: documents, images, code and config, anything else. */
internal fun fileIcon(name: String): PhIcon = when (FilesPaths.extension(name)) {
    "md", "markdown", "mdx", "txt", "rtf", "pdf" -> PhIcons.FileText
    "png", "jpg", "jpeg", "gif", "webp", "heic", "bmp", "svg" -> PhIcons.Image
    "kt", "kts", "java", "go", "rs", "swift", "ts", "tsx", "js", "jsx", "py", "rb", "c", "h", "cpp", "m", "sh", "zsh",
    "json", "yaml", "yml", "toml", "xml", "html", "css", "sql", "gradle", "plist" -> PhIcons.Code
    else -> PhIcons.File
}

@Composable internal fun OneLine(text: String) { Text(text, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2, maxLines = 1, softWrap = false, overflow = TextOverflow.Ellipsis) }

/** A line of explanation. [padded]: on its own the page's side margin; inside a list page that already has it, none. */
@Composable internal fun Notice(text: String, padded: Boolean = true) {
    Text(text, Modifier.padding(horizontal = if (padded) pagePadding else Tokens.Space.xs, vertical = Tokens.Space.s),
        style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
}

@Composable internal fun Loading() { JLoading() }

@Composable internal fun RetryState(title: String, message: String, retry: () -> Unit) {
    JEmptyState(title, message, action = { JButton("重试", retry, kind = JButtonKind.Secondary) })
}

/** Copies [text] and says so through the app's one notice line. */
internal fun copyText(context: Context, text: String, feedback: AppFeedback?, notice: String) {
    (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("Persona", text))
    feedback?.notice(notice)
}
