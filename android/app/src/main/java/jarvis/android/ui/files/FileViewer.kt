package jarvis.android.ui.files

import android.graphics.Bitmap
import android.graphics.pdf.PdfRenderer
import android.os.ParcelFileDescriptor
import android.util.Base64
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.data.Hub
import jarvis.android.ui.CardSlice
import jarvis.android.ui.JTheme
import jarvis.android.ui.Markdown
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.DOT_SEP
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.media.ChatImageLoader
import jarvis.android.ui.media.MediaShare
import jarvis.android.ui.media.ImageViewerArgs
import jarvis.android.ui.media.ViewerImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** Behind a PDF's white pages: the neutral gray PDF viewers use (PDFKit's), the same in light and dark. */
private val PdfBackdrop = Color(0xFF808080)

/**
 * One file on a machine (iOS `FileViewer`): Markdown rendered (or its source), code and text monospaced with line
 * numbers scrolling both ways, images (tap for the viewer) and PDFs shown, a directory listed. Up to 2 MB (the node's
 * cap; the meta line says when it was cut). [initialFile]: the file already read, for the first frame (screenshots).
 */
@Composable
internal fun FileViewer(
    hub: Hub,
    machineId: String,
    path: String,
    cwd: String?,
    machineName: String,
    onBack: () -> Unit,
    onOpen: (String) -> Unit,
    onOpenImage: (ImageViewerArgs) -> Unit,
    initialFile: FileContent? = null,
) {
    var file by remember(path) { mutableStateOf(initialFile) }
    var error by remember(path) { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    var raw by rememberSaveable(path) { mutableStateOf(false) }
    LaunchedEffect(path, reload) {
        error = null
        runCatching { hub.api.readFile(machineId, path, cwd) }
            .onSuccess { file = it }
            .onFailure { error = it.shown() }
    }
    val f = file
    val markdown = f != null && f.text != null && FilesPaths.isMarkdown(f.name)
    val ctx = LocalContext.current
    FilesPage(
        title = f?.name?.ifEmpty { null } ?: FilesPaths.name(path),
        subtitle = machineName,
        onBack = onBack,
        actions = {
            var open by remember { mutableStateOf(false) }
            Box {
                CircleButton(JIcons.more, "更多", { open = true })
                DropdownMenu(open, onDismissRequest = { open = false }) {
                    if (markdown) DropdownMenuItem(
                        text = { Text(if (raw) "渲染 Markdown" else "查看源文件", maxLines = 1) },
                        onClick = { raw = !raw; open = false },
                        leadingIcon = { Icon(if (raw) JIcons.document else JIcons.code, null) },
                    )
                    DropdownMenuItem(text = { Text("拷贝路径", maxLines = 1) }, onClick = {
                        MediaShare.copyText(ctx, f?.path?.ifEmpty { null } ?: path, "路径"); MediaShare.toast(ctx, "已拷贝路径"); open = false
                    }, leadingIcon = { Icon(JIcons.copy, null) })
                    f?.text?.let { t ->
                        DropdownMenuItem(text = { Text("拷贝内容", maxLines = 1) }, onClick = {
                            MediaShare.copyText(ctx, t, f.name); MediaShare.toast(ctx, "已拷贝"); open = false
                        }, leadingIcon = { Icon(JIcons.copy, null) })
                        DropdownMenuItem(text = { Text("分享", maxLines = 1) }, onClick = { MediaShare.shareText(ctx, t); open = false },
                            leadingIcon = { Icon(JIcons.share, null) })
                    }
                }
            }
        },
    ) { insets ->
        Box(Modifier.fillMaxSize()) {
            when {
                f != null -> FileBody(hub, f, markdown && !raw, insets, onOpen, onOpenImage)
                error != null -> ErrorNotice("打不开这个文件", error!!, onRetry = { reload++ })
                else -> Loading()
            }
        }
    }
}

/** The path, the size, and when it was cut (iOS `FileViewer.meta`): monospaced, in the secondary color. */
@Composable
private fun MetaLine(f: FileContent) {
    Text(
        listOfNotNull(FilesPaths.keepEnd(f.path), if (f.isDir) null else FilesPaths.formatBytes(f.size), if (f.truncated) "只显示前 2 MB" else null).joinToString(DOT_SEP),
        style = JTheme.type.code,
        color = JTheme.colors.secondaryText,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.fillMaxWidth().padding(horizontal = JSpace.page + JSpace.xs, vertical = JSpace.s),
    )
}

@Composable
private fun FileBody(hub: Hub, f: FileContent, rendered: Boolean, insets: PaddingValues, onOpen: (String) -> Unit, onOpenImage: (ImageViewerArgs) -> Unit) {
    val text = f.text
    val data = f.data
    val bottom = insets.calculateBottomPadding()
    when {
        f.isDir -> LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = JSpace.xxl + bottom)) {
            item { MetaLine(f) }
            val entries = f.entries.orEmpty()
            itemsIndexed(entries) { i, e ->
                CardSlice(first = i == 0, last = i == entries.lastIndex, dividerStart = FileTextColumn) {
                    FileRow(e.trimEnd('/'), e.endsWith("/"), onClick = { onOpen(FilesPaths.join(f.path, e)) })
                }
            }
        }
        text != null && rendered -> Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = JSpace.xxl + bottom)) {
            MetaLine(f)
            SelectionContainer {
                Markdown(simplifyHtml(text), Modifier.padding(horizontal = JSpace.page, vertical = JSpace.s))
            }
        }
        // Code and text in a rounded block on the code background (iOS: the lines in a control-cornered box, 12 from the edges).
        text != null -> Column(Modifier.fillMaxSize()) {
            MetaLine(f)
            Box(
                Modifier.weight(1f).fillMaxWidth().padding(horizontal = JSpace.m).padding(bottom = JSpace.s + bottom)
                    .clip(RoundedCornerShape(JRadius.control)).background(JTheme.colors.codeBackground),
            ) { MonoLines(numberedLines(text), Modifier.fillMaxSize()) }
        }
        data != null && f.mime.orEmpty().startsWith("image/") -> Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = bottom)) {
            MetaLine(f)
            val img = remember(f.path, data.length) { ViewerImage(data = data, mime = f.mime) }
            val bmp by produceState<Bitmap?>(null, img) { value = ChatImageLoader.load(hub, img, 1600) }
            val b = bmp
            if (b == null) {
                Box(Modifier.fillMaxWidth().padding(JSpace.xxl), contentAlignment = Alignment.Center) { Loading() }
            } else {
                Image(
                    b.asImageBitmap(), contentDescription = f.name, contentScale = ContentScale.Fit,
                    modifier = Modifier.padding(JSpace.page).fillMaxWidth().aspectRatio(b.width.toFloat() / b.height.coerceAtLeast(1))
                        .clip(RoundedCornerShape(JRadius.control))
                        .clickable(onClickLabel = "查看大图") { onOpenImage(ImageViewerArgs(listOf(img))) },
                )
            }
        }
        data != null && f.mime == "application/pdf" -> PdfPages(f.path, data, bottom)
        else -> Notice(JIcons.question, "无法预览", "${f.mime ?: "二进制文件"}$DOT_SEP${FilesPaths.formatBytes(f.size)}")
    }
}

/** A PDF's pages as images (PdfRenderer reads from a file: the bytes go to the cache first). */
@Composable
private fun PdfPages(path: String, data: String, bottom: androidx.compose.ui.unit.Dp) {
    val ctx = LocalContext.current
    val renderer by produceState<PdfRenderer?>(null, path) {
        value = withContext(Dispatchers.IO) {
            runCatching {
                val tmp = java.io.File(ctx.cacheDir, "pdf-view.pdf")
                tmp.writeBytes(Base64.decode(data, Base64.DEFAULT))
                PdfRenderer(ParcelFileDescriptor.open(tmp, ParcelFileDescriptor.MODE_READ_ONLY))
            }.getOrNull()
        }
    }
    val r = renderer
    if (r == null) {
        Loading()
        return
    }
    DisposableEffect(r) { onDispose { synchronized(r) { runCatching { r.close() } } } }
    LazyColumn(
        Modifier.fillMaxSize().background(PdfBackdrop),
        contentPadding = PaddingValues(start = JSpace.m, end = JSpace.m, top = JSpace.m, bottom = JSpace.m + bottom),
        verticalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        items(r.pageCount) { i ->
            val page by produceState<Bitmap?>(null, r, i) {
                value = withContext(Dispatchers.IO) {
                    synchronized(r) {
                        runCatching {
                            r.openPage(i).use { p ->
                                val w = 1200
                                val h = (w * p.height / p.width.toFloat()).toInt()
                                Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888).also { b ->
                                    b.eraseColor(android.graphics.Color.WHITE)
                                    p.render(b, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
                                }
                            }
                        }.getOrNull()
                    }
                }
            }
            val b = page
            if (b != null) Image(b.asImageBitmap(), "第 ${i + 1} 页", Modifier.fillMaxWidth().aspectRatio(b.width / b.height.toFloat()))
            else Box(Modifier.fillMaxWidth().aspectRatio(0.707f).background(Color.White))
        }
    }
}

/** READMEs use a little HTML (`<img>`, `<a>`, `<br>`, centered `<p>`): turned into Markdown, other tags dropped (iOS `MarkdownHTML`). */
fun simplifyHtml(md: String): String {
    if ('<' !in md) return md
    val out = ArrayList<String>()
    var fence: String? = null
    for (line in md.split('\n')) {
        val t = line.trim()
        if (fence != null) { if (t.startsWith(fence)) fence = null; out += line; continue }
        if (t.startsWith("```") || t.startsWith("~~~")) { fence = t.take(3); out += line; continue }
        if (t.startsWith("<") && !t.startsWith("<http")) {
            val s = inlineHtml(t)
            if (s.isNotEmpty()) out += s
            continue
        }
        out += line
    }
    return out.joinToString("\n")
}

private fun inlineHtml(html: String): String {
    val opts = setOf(RegexOption.IGNORE_CASE, RegexOption.DOT_MATCHES_ALL)
    fun attr(name: String, tag: String) = Regex("\\b$name\\s*=\\s*[\"']([^\"']*)[\"']", opts).find(tag)?.groupValues?.get(1)
    var s = Regex("<img\\b[^>]*>", opts).replace(html) { m -> attr("src", m.value)?.let { "![${attr("alt", m.value).orEmpty()}]($it)" }.orEmpty() }
    s = Regex("<a\\b[^>]*>(.*?)</a>", opts).replace(s) { m ->
        val text = Regex("<[^>]+>").replace(m.groupValues[1], "")
        attr("href", m.value)?.let { "[$text]($it)" } ?: text
    }
    s = Regex("<br\\s*/?>", opts).replace(s, " ")
    s = Regex("<[^>]+>").replace(s, "")
    for ((k, v) in listOf("&nbsp;" to " ", "&lt;" to "<", "&gt;" to ">", "&quot;" to "\"", "&amp;" to "&")) s = s.replace(k, v)
    return s.split(Regex("\\s+")).filter { it.isNotEmpty() }.joinToString(" ")
}
