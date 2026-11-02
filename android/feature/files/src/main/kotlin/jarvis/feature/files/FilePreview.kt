package jarvis.feature.files

import androidx.compose.foundation.gestures.rememberTransformableState
import androidx.compose.foundation.gestures.transformable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import coil3.compose.AsyncImagePainter
import jarvis.core.contracts.api.SessionFileContent
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.util.Base64

@Composable
internal fun FilePreview(state: FilesState, controller: FilesController, insert: (String) -> Unit, back: () -> Unit) {
    val selection = state.selection ?: return
    val context = LocalContext.current
    var menu by remember(selection) { mutableStateOf(false) }
    var expanded by remember(selection) { mutableStateOf(false) }
    val path = when (selection) {
        is FileSelection.File -> state.content?.path ?: selection.path
        is FileSelection.Diff -> joinRemotePath(state.git?.root ?: controller.cwd, selection.file.path)
        is FileSelection.Commit -> null
    }
    val title = path?.substringAfterLast('/') ?: state.commit?.commit?.subject ?: "提交详情"
    JPage(title, subtitle = path, onBack = back, actions = {
        if (path != null) JTextButton("插入路径", { insert(path) })
        JIconButton(PhIcons.DotsThree, "预览选项", { menu = true })
    }) { padding ->
        val header: LazyListScope.() -> Unit = {
            if (selection is FileSelection.Diff && selection.file.staged && selection.file.unstaged) item("staging") {
                JSegmented(listOf(JSegment("未暂存"), JSegment("已暂存")), if (selection.staged) 1 else 0,
                    { controller.preview(selection.copy(staged = it == 1)) }, Modifier.padding(16.dp))
            }
            state.content?.let { file -> item("size") { Notice("${fileSize(file.size)}${if (file.truncated) "，内容过长，只显示前面部分" else ""}") } }
            if (state.diff?.truncated == true || state.commit?.truncated == true) item("truncated") { Notice("改动较多，只显示前面部分。") }
            state.commit?.let { commit ->
                item("commit") {
                    Notice("${commit.commit.short}，${commit.commit.author}，修改了 ${commit.files.size} 个文件")
                    commit.commit.body?.takeIf { it.isNotBlank() }?.let {
                        Text(it, Modifier.padding(horizontal = 16.dp), style = JarvisTheme.type.body, color = JarvisTheme.palette.ink,
                            maxLines = if (expanded) 8 else 2, overflow = TextOverflow.Ellipsis)
                        JTextButton(if (expanded) "收起说明" else "展开说明", { expanded = !expanded })
                    }
                }
            }
        }
        val file = state.content
        val diff = state.diff
        val commit = state.commit
        when {
            state.previewError -> LazyColumn(contentPadding = padding) { item { RetryState("无法打开预览", "文件可能已移动，或连接暂时不可用。") { controller.preview(selection) } } }
            file != null -> key(file.path) {
                when {
                    file.isDir -> LazyColumn(Modifier.fillMaxSize(), contentPadding = padding) {
                        header()
                        if (file.entries.isEmpty()) item { JEmptyState("空文件夹", "这里没有文件。") }
                        items(file.entries, key = { it }, contentType = { "directory" }) { entry ->
                            JRow(entry, leadingIcon = PhIcons.File, onClick = { controller.preview(FileSelection.File(joinRemotePath(file.path, entry))) })
                        }
                    }
                    file.mime?.startsWith("image/") == true && file.data != null -> Column(Modifier.fillMaxSize().padding(padding)) {
                        Notice("${fileSize(file.size)}${if (file.truncated) "，内容过长，只显示前面部分" else ""}")
                        FileImage(file, Modifier.weight(1f))
                    }
                    file.text != null || file.size == 0L -> FileText(file.text.orEmpty(), contentPadding = padding, header = header)
                    else -> LazyColumn(contentPadding = padding) {
                        header()
                        item { JEmptyState("无法预览这个文件", "${file.mime ?: "二进制文件"}，可以把路径插入对话。") }
                    }
                }
            }
            diff != null -> key(selection) {
                if (diff.diff.isBlank()) LazyColumn(contentPadding = padding) {
                    header(); item { JEmptyState("没有可显示的文本差异", "文件可能是二进制，或改动已经更新。") }
                } else FileText(diff.diff, diff = true, contentPadding = padding, header = header)
            }
            commit != null -> {
                if (commit.diff.isBlank()) LazyColumn(contentPadding = padding) {
                    header(); item { JEmptyState("没有文本差异", "这个提交没有可显示的文本改动。") }
                } else FileText(commit.diff, diff = true, contentPadding = padding, header = header)
            }
            else -> LazyColumn(contentPadding = padding) { item { Loading() } }
        }
    }
    if (menu) JActionSheet(buildList {
        if (path != null) add(JAction("拷贝路径") { copyPath(context, path) })
        val text = state.content?.text ?: state.diff?.diff ?: state.commit?.diff
        if (text != null) add(JAction("拷贝内容") { copyPath(context, text) })
        add(JAction("刷新") { controller.preview(selection) })
    }, { menu = false }, "预览选项")
}

@Composable
private fun FileImage(file: SessionFileContent, modifier: Modifier) {
    val bytes by produceState<ByteArray?>(null, file.data) {
        value = withContext(Dispatchers.Default) {
            try { Base64.getDecoder().decode(file.data) } catch (_: IllegalArgumentException) { byteArrayOf() }
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
