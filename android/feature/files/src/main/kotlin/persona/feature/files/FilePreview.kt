package persona.feature.files

import androidx.compose.foundation.gestures.rememberTransformableState
import androidx.compose.foundation.gestures.transformable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import coil3.compose.AsyncImage
import coil3.compose.AsyncImagePainter
import persona.core.contracts.api.SessionFileContent
import persona.core.contracts.api.SessionFiles
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.Base64

/**
 * A file or a working-tree diff over the browser: Markdown rendered (or its source), code and text with line numbers,
 * images to zoom, PDFs page by page, anything else described. ⋯ copies, shares and reloads.
 */
@Composable
internal fun FilePreview(state: FilesState, controller: FilesController, images: MarkdownImages, insert: (String) -> Unit, back: () -> Unit) {
    val selection = state.selection ?: return
    val context = LocalContext.current
    val feedback = LocalAppFeedback.current
    val scope = rememberCoroutineScope()
    var menu by remember(selection) { mutableStateOf(false) }
    val file = state.content
    val markdown = file?.text != null && FilesPaths.isMarkdown(file.name)
    val table = file != null && isTable(file)
    // A CSV / TSV is text too: ⋯ switches between the grid and its source.
    val delimited = table && file?.text != null
    val notebook = file?.text != null && Notebooks.isNotebook(file.name)
    val html = file?.text != null && HtmlPreview.isHtml(file.name)
    // Per file and only while it's open: scripts and the network come back off next time.
    var policy by remember(file?.path) { mutableStateOf(HtmlPreview.Policy()) }
    var source by rememberSaveable(selection.toString()) { mutableStateOf(false) }
    val path = when (selection) {
        is FileSelection.File -> file?.path?.ifEmpty { null } ?: selection.path
        is FileSelection.Diff -> joinRemotePath(state.git?.root ?: controller.cwd, selection.file.path)
        else -> null
    }
    val title = when (selection) {
        is FileSelection.Diff -> selection.file.name
        else -> file?.name?.ifEmpty { null } ?: path?.let(FilesPaths::name) ?: "文件"
    }
    JPage(title, subtitle = path, onBack = back, actions = {
        if (path != null) JTextButton("插入路径", { insert(path) })
        JIconButton(PhIcons.DotsThree, "预览选项", { menu = true })
    }) { padding ->
        val diff = state.diff
        val error = state.previewError
        val header: LazyListScope.() -> Unit = {
            if (selection is FileSelection.Diff && selection.file.staged && selection.file.unstaged) item("staging") {
                JSegmented(listOf(JSegment("未暂存"), JSegment("已暂存")), if (selection.staged) 1 else 0,
                    { controller.preview(selection.copy(staged = it == 1)) }, Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
            }
            if (file != null) item("size") { FileMeta(file.size, file.truncated) }
            if (diff?.truncated == true) item("truncated") { Notice("改动太大，只显示了前 512 KB") }
        }
        when {
            error != null -> LazyColumn(contentPadding = padding) {
                item { RetryState(if (selection is FileSelection.Diff) "读不到改动" else "打不开这个文件", error, controller::reload) }
            }
            file != null && table && !source -> key(file.path) { SheetFile(file, padding, header) }
            file != null && notebook && !source -> key(file.path) {
                NotebookFile(file.text.orEmpty(), FilesPaths.parent(file.path) ?: controller.cwd, padding, header,
                    onOpenPath = { controller.push(FileSelection.File(it)) }, loadImage = images::load, truncated = file.truncated)
            }
            file != null && html && !source -> key(file.path) {
                Column(Modifier.fillMaxSize().padding(padding)) {
                    FileMeta(file.size, file.truncated)
                    HtmlFile(file, policy, Modifier.weight(1f))
                }
            }
            file != null -> key(file.path) { FileBody(file, markdown && !source, padding, header, controller, images) }
            diff != null -> key(selection) {
                if (diff.diff.isBlank()) LazyColumn(contentPadding = padding) {
                    header(); item { JEmptyState("没有文本改动", "可能是二进制文件、只改了权限，或者改动已被撤销。") }
                } else FileText(diff.diff, diff = true, contentPadding = padding, header = header)
            }
            else -> LazyColumn(contentPadding = padding) { item { Loading() } }
        }
    }
    if (menu) JActionSheet(buildList {
        if (markdown) add(JAction(if (source) "渲染 Markdown" else "查看源文件", if (source) PhIcons.FileText else PhIcons.Code) { source = !source })
        if (delimited) add(JAction(if (source) "查看表格" else "查看源文件", if (source) PhIcons.SquaresFour else PhIcons.Code) { source = !source })
        if (notebook) add(JAction(if (source) "查看笔记本" else "查看源文件", if (source) PhIcons.BookOpen else PhIcons.Code) { source = !source })
        if (html) {
            add(JAction(if (source) "预览网页" else "查看源文件", if (source) PhIcons.GlobeSimple else PhIcons.Code) { source = !source })
            if (!source) {
                add(JAction("允许运行脚本", PhIcons.Lightning, checked = policy.scripts) { policy = policy.copy(scripts = !policy.scripts) })
                add(JAction("允许联网", PhIcons.WifiHigh, checked = policy.network) { policy = policy.copy(network = !policy.network) })
            }
        }
        if (path != null) add(JAction("拷贝路径", PhIcons.Copy) { copyText(context, path, feedback, "已拷贝路径") })
        file?.text?.let { text -> add(JAction("拷贝内容", PhIcons.Copy) { copyText(context, text, feedback, "已拷贝内容") }) }
        state.diff?.diff?.takeIf { it.isNotEmpty() }?.let { text -> add(JAction("拷贝 diff", PhIcons.Copy) { copyText(context, text, feedback, "已拷贝 diff") }) }
        if (file != null && FileShare.canShare(file)) add(JAction("分享", PhIcons.ShareNetwork) {
            scope.launch {
                val shared = runCatching { FileShare.share(context, file) }.getOrDefault(false)
                if (!shared) feedback?.notice("这个文件分享不了")
            }
        })
        add(JAction("刷新", PhIcons.ArrowClockwise, separatorBefore = true) { controller.reload() })
    }, { menu = false }, title)
}

/** CSV / TSV (as text or bytes) and xlsx (bytes) open in the table viewer ([SheetFile]). */
internal fun isTable(file: SessionFileContent): Boolean =
    TableFiles.isDelimited(file.name) && (file.text != null || file.data != null) || TableFiles.isWorkbook(file.name) && file.data != null

@Composable
private fun FileBody(file: SessionFileContent, rendered: Boolean, padding: PaddingValues, header: LazyListScope.() -> Unit,
    controller: FilesController, images: MarkdownImages) {
    val text = file.text
    val mime = file.mime.orEmpty()
    when {
        text != null && rendered -> FileMarkdown(text, FilesPaths.parent(file.path) ?: controller.cwd, padding,
            onOpenPath = { controller.push(FileSelection.File(it)) }, loadImage = images::load, header = header)
        text != null || file.size == 0L -> FileText(text.orEmpty(), contentPadding = padding, header = header)
        file.data != null && mime.startsWith("image/") -> Column(Modifier.fillMaxSize().padding(padding)) {
            FileMeta(file.size, file.truncated)
            FileImage(file, Modifier.weight(1f))
        }
        file.data != null && mime.substringBefore(';').trim() == "application/pdf" -> PdfPages(file, padding, header)
        AudioFiles.isAudio(file) -> AudioFile(file, controller::download, padding, header)
        else -> LazyColumn(contentPadding = padding) {
            header()
            item { JEmptyState("无法预览", "${mime.ifEmpty { "二进制文件" }}，${fileSize(file.size)}。可以插入路径或分享。") }
        }
    }
}

/** The size line above a file's content: the size, and that only the first 2 MB came when the file is bigger. */
@Composable
internal fun FileMeta(size: Long, truncated: Boolean) {
    Notice(fileSize(size) + if (truncated) "，只显示前 2 MB" else "")
}

/** Reads images a Markdown file names from the same machine (the hub's file read, base64 bytes). */
internal class MarkdownImages(private val api: SessionFiles, private val machineId: String, private val cwd: String) {
    private val cache = object : LinkedHashMap<String, ByteArray>(8, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, ByteArray>?) = size > 12
    }

    suspend fun load(path: String): ByteArray? {
        synchronized(cache) { cache[path] }?.let { return it }
        val file = runCatching { api.file(machineId, cwd, path) }.getOrNull() ?: return null
        val mime = file.mime.orEmpty()
        if (file.data == null || !mime.startsWith("image/") || mime.startsWith("image/svg")) return null
        val bytes = withContext(Dispatchers.Default) { runCatching { Base64.getMimeDecoder().decode(file.data) }.getOrNull() } ?: return null
        synchronized(cache) { cache[path] = bytes }
        return bytes
    }
}

@Composable
private fun FileImage(file: SessionFileContent, modifier: Modifier) {
    val bytes by produceState<ByteArray?>(null, file.data) {
        value = withContext(Dispatchers.Default) {
            try { Base64.getMimeDecoder().decode(file.data) } catch (_: IllegalArgumentException) { byteArrayOf() }
        }
    }
    var scale by remember(file.path) { mutableFloatStateOf(1f) }
    var offset by remember(file.path) { mutableStateOf(Offset.Zero) }
    var failed by remember(file.path) { mutableStateOf(false) }
    val gestures = rememberTransformableState { zoom, pan, _ ->
        scale = (scale * zoom).coerceIn(1f, 6f)
        offset = if (scale == 1f) Offset.Zero else offset + pan
    }
    Box(modifier.fillMaxWidth().clipToBounds()) {
        when {
            bytes == null -> Loading()
            bytes!!.isEmpty() || failed -> JEmptyState("图片无法显示", "图片数据不完整，或格式暂不支持。")
            else -> AsyncImage(model = bytes, contentDescription = file.name, contentScale = ContentScale.Fit,
                modifier = Modifier.fillMaxSize().transformable(gestures).graphicsLayer {
                    scaleX = scale; scaleY = scale; translationX = offset.x; translationY = offset.y
                }, onState = { failed = it is AsyncImagePainter.State.Error })
        }
    }
}
