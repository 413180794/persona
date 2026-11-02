package jarvis.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.background
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.buildAnnotatedString
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** The opening value is retained until dismissal, independently of streaming updates. */
@Composable
internal fun FullTextReader(text: String, onDismiss: () -> Unit, title: String = "全文") {
    val frozen = remember { text }
    val snapshot by produceState<ReaderSnapshot?>(null) {
        value = withContext(Dispatchers.Default) { ReaderSnapshot(frozen) }
    }
    val clipboard = LocalClipboardManager.current
    JFullScreen(onDismiss) {
        val ready = snapshot
        val copy = { clipboard.setText(AnnotatedString(frozen)) }
        if (ready == null) JPage(title, onBack = onDismiss) { padding ->
            LazyColumn(contentPadding = PaddingValues(start = pagePadding, end = pagePadding,
                top = padding.calculateTopPadding(), bottom = padding.calculateBottomPadding())) { item { JLoading() } }
        } else ReaderPages(ready, Modifier.fillMaxSize().imePadding(), title, onDismiss, copy)
    }
}

@Composable
private fun ReaderPages(snapshot: ReaderSnapshot, modifier: Modifier, title: String, onBack: () -> Unit, onCopy: () -> Unit) {
    var page by rememberSaveable { mutableIntStateOf(0) }
    var query by rememberSaveable { mutableStateOf("") }
    var onlyPage by rememberSaveable { mutableStateOf(true) }
    val searchPage = page.takeIf { onlyPage }
    val matches by produceState(emptyList<ReaderRange>(), snapshot, query, searchPage) {
        value = emptyList()
        value = withContext(Dispatchers.Default) { snapshot.search(query, searchPage) }
    }
    var selected by remember(query, searchPage) { mutableIntStateOf(-1) }
    var target by remember { mutableStateOf<ReaderRange?>(null) }
    val bounds = snapshot.pages[page]
    val body = remember(snapshot, page) { snapshot.text.substring(bounds.start, bounds.end) }
    val scroll = rememberLazyListState()
    var layout by remember(body) { mutableStateOf<TextLayoutResult?>(null) }
    val active = matches.getOrNull(selected)
    val palette = JarvisTheme.palette
    val annotated = remember(body, matches, active, bounds, palette) {
        buildAnnotatedString {
            append(body)
            matches.forEach { match ->
                val start = maxOf(match.start, bounds.start) - bounds.start
                val end = minOf(match.end, bounds.end) - bounds.start
                if (start < end) addStyle(SpanStyle(
                    background = palette.marker,
                    color = palette.ink,
                    textDecoration = if (match == active) androidx.compose.ui.text.style.TextDecoration.Underline else null,
                ), start, end)
            }
        }
    }
    LaunchedEffect(page) { scroll.scrollToItem(0) }
    LaunchedEffect(target, page, layout) {
        val match = target ?: return@LaunchedEffect
        val result = layout ?: return@LaunchedEffect
        if (match.start in bounds.start until bounds.end && body.isNotEmpty()) {
            scroll.scrollToItem(0, result.getLineTop(result.getLineForOffset(match.start - bounds.start)).toInt())
        }
    }
    fun moveMatch(delta: Int) {
        if (matches.isEmpty()) return
        selected = if (selected < 0) (if (delta > 0) 0 else matches.lastIndex)
            else Math.floorMod(selected + delta, matches.size)
        target = matches[selected]
        page = snapshot.pageAt(matches[selected].start)
    }
    fun movePage(index: Int) {
        target = null
        selected = -1
        page = index.coerceIn(snapshot.pages.indices)
    }
    JPage(title = title, onBack = onBack, modifier = modifier,
        topBar = {
            Column {
                JTopBar(title, onBack = onBack, actions = { JIconButton(PhIcons.Copy, "复制全文", onCopy) })
                JSearchField(query, { query = it; target = null }, if (onlyPage) "搜索本页" else "搜索全文",
                    Modifier.padding(horizontal = pagePadding))
                Row(Modifier.fillMaxWidth().padding(horizontal = pagePadding), verticalAlignment = Alignment.CenterVertically) {
                    JTextButton(if (onlyPage) "范围：本页" else "范围：全文", { onlyPage = !onlyPage; target = null })
                    Text(if (query.isEmpty()) "" else if (matches.isEmpty()) "未找到" else "${(selected + 1).coerceAtLeast(0)}/${matches.size}",
                        Modifier.weight(1f), style = JarvisTheme.type.micro, color = palette.ink2, maxLines = 1)
                    JIconButton(PhIcons.CaretUp, "上一处匹配", { moveMatch(-1) }, enabled = matches.isNotEmpty())
                    JIconButton(PhIcons.CaretDown, "下一处匹配", { moveMatch(1) }, enabled = matches.isNotEmpty())
                }
            }
        },
        bottomBar = {
            JBottomBar { Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceEvenly, verticalAlignment = Alignment.CenterVertically) {
                JIconButton(PhIcons.ArrowLeft, "首页", { movePage(0) }, enabled = page > 0)
                JIconButton(PhIcons.CaretLeft, "上一页", { movePage(page - 1) }, enabled = page > 0)
                Text("${page + 1}/${snapshot.pages.size}", style = JarvisTheme.type.caption, color = palette.ink, maxLines = 1)
                JIconButton(PhIcons.CaretRight, "下一页", { movePage(page + 1) }, enabled = page < snapshot.pages.lastIndex)
                JIconButton(PhIcons.ArrowLeft, "末页", { movePage(snapshot.pages.lastIndex) }, modifier = Modifier.rotate(180f), enabled = page < snapshot.pages.lastIndex)
            } }
        }) { padding ->
        LazyColumn(Modifier.fillMaxSize(), state = scroll, contentPadding = PaddingValues(
            start = pagePadding, end = pagePadding, top = padding.calculateTopPadding(),
            bottom = padding.calculateBottomPadding() + Tokens.Space.m)) {
            item {
                SelectionContainer {
                    Text(if (body.isEmpty()) AnnotatedString("没有内容") else annotated,
                        style = JarvisTheme.type.code, color = palette.ink, onTextLayout = { layout = it },
                        modifier = Modifier.fillMaxWidth().clip(JarvisTheme.shapes.controlShape)
                            .background(palette.sunken).padding(Tokens.Space.m))
                }
            }
        }
    }
}

@Composable
internal fun ReaderEntry(text: String, title: String = "全文", always: Boolean = false) {
    var opened by remember { mutableStateOf<String?>(null) }
    if (always || text.length > 8192 || text.count { it == '\n' } >= 40) {
        JTextButton("看全文", { opened = text })
    }
    opened?.let { FullTextReader(it, onDismiss = { opened = null }, title = title) }
}

@Composable
internal fun ToolReaderActions(input: String?, output: String?, onFullOutput: (() -> Unit)?) {
    var opened by remember { mutableStateOf<Pair<String, String>?>(null) }
    val clipboard = LocalClipboardManager.current
    Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        JTextButton("原始输入", { opened = "原始输入" to input.orEmpty() }, enabled = input != null)
        JTextButton(if (onFullOutput != null) "完整输出" else "原始输出", { if (onFullOutput != null) onFullOutput() else opened = "原始输出" to output.orEmpty() },
            enabled = output != null || onFullOutput != null)
        JTextButton("全部复制", { clipboard.setText(AnnotatedString(listOfNotNull(input, output).joinToString("\n\n"))) },
            enabled = input != null || output != null)
    }
    opened?.let { (title, text) -> FullTextReader(text, onDismiss = { opened = null }, title = title) }
}
