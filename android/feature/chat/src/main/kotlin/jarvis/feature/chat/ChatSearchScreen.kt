package jarvis.feature.chat

import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.unit.dp
import jarvis.core.contracts.Item
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.ChatRepository
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Composable
internal fun ChatSearchScreen(
    state: ChatSearchState, items: List<Item>, rounds: Int, hasOlder: Boolean,
    repository: ChatRepository, session: SessionKey, onClose: () -> Unit,
) {
    val palette = JarvisTheme.palette
    val scope = rememberCoroutineScope()
    var loading by remember { mutableStateOf(false) }
    var pageJob by remember { mutableStateOf<Job?>(null) }
    var pages by remember { mutableIntStateOf(0) }
    var notice by remember { mutableStateOf<String?>(null) }
    var searching by remember { mutableStateOf(false) }
    val results by produceState(ChatSearchResults(), state.query, items) {
        searching = true
        value = ChatSearchResults()
        delay(200)
        value = withContext(Dispatchers.Default) { searchChat(items, state.query) }
        searching = false
    }
    JFullScreen(onClose) {
        JPage(title = "会话内搜索", onBack = onClose, modifier = Modifier.imePadding(),
            topBar = {
                Column {
                    JTopBar("会话内搜索", onBack = onClose)
                    JSearchField(state.query, { state.query = it }, "搜索已加载的记录",
                        Modifier.padding(horizontal = pagePadding))
                    Text("${results.hits.size}${if (results.truncated) "+" else ""} 条命中，已加载 $rounds 轮",
                        Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
                        style = JarvisTheme.type.caption, color = palette.ink2)
                    if (results.truncated) Text("只显示前 100 条命中，请缩小搜索范围",
                        Modifier.padding(horizontal = pagePadding), color = palette.ink2, style = JarvisTheme.type.caption)
                    if (searching) JLinearProgress(null)
                }
            },
            bottomBar = {
                JBottomBar { Column(Modifier.fillMaxWidth()) {
                    notice?.let { Text(it, style = JarvisTheme.type.caption, color = palette.ink2) }
                    if (loading) {
                        Text("正在加载更早的记录，已加载 $pages 页", style = JarvisTheme.type.caption, color = palette.ink2)
                        JTextButton("停止加载", { pageJob?.cancel(); notice = "已停止，载入的记录已保留" })
                    } else if (hasOlder) JButton("加载更早的记录并继续搜", onClick = {
                        loading = true
                        pages = 0
                        notice = null
                        pageJob = scope.launch {
                            try { notice = loadEarlierForSearch(repository, session) { pages = it } }
                            catch (e: CancellationException) { throw e }
                            catch (_: Exception) { notice = "更早的记录未能载入，请重试" }
                            finally { loading = false }
                        }
                    }, modifier = Modifier.fillMaxWidth(), kind = JButtonKind.Secondary)
                    else Text("已到最早的记录", style = JarvisTheme.type.caption, color = palette.ink2)
                } }
            }) { padding ->
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(
                start = pagePadding, end = pagePadding, top = padding.calculateTopPadding(),
                bottom = padding.calculateBottomPadding() + Tokens.Space.m)) {
                if (!searching && results.hits.isEmpty()) item {
                    JEmptyState(if (state.query.isBlank()) "输入关键词" else "没有找到匹配记录",
                        "可搜索正文、思考、工具名、工具输入和输出、系统事件。")
                }
                results.hits.groupBy { it.category }.forEach { (category, hits) ->
                    item(key = "category-$category") { JSectionHeader(category) }
                    itemsIndexed(hits, key = { index, hit -> "$category/${hit.itemId}/$index" }) { index, hit ->
                        JGroupItem(GroupPosition.of(index, hits.size)) {
                            JRow(category, onClick = { state.choose(hit) }, leadingIcon = PhIcons.MagnifyingGlass)
                            Text(buildAnnotatedString {
                                append(hit.snippet)
                                hit.ranges.forEach { addStyle(SpanStyle(background = palette.marker,
                                    color = palette.ink), it.first, it.last + 1) }
                            }, Modifier.fillMaxWidth().clickable { state.choose(hit) }.padding(Tokens.Space.m),
                                style = JarvisTheme.type.callout, color = palette.ink)
                        }
                    }
                }
                item {
                    Text("仅搜索已加载的内容；未展开的工作记录和截断的全文不在搜索范围内。",
                        Modifier.padding(vertical = Tokens.Space.m), style = JarvisTheme.type.caption, color = palette.ink2)
                }
            }
        }
    }
}

@Composable
internal fun ReturnChip(onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(horizontal = Tokens.Space.m)) {
        JChip("返回搜索前位置", onClick = onClick, icon = PhIcons.ArrowLeft)
    }
}

@Composable
internal fun ChatSearchHighlight(search: ChatSearchState, rowId: String, content: @Composable () -> Unit) {
    CompositionLocalProvider(LocalChatSearchItem provides search.selected?.itemId) {
        Box(if (search.highlight == rowId) Modifier.fillMaxWidth().border(2.dp, JarvisTheme.palette.marker,
            JarvisTheme.shapes.controlShape) else Modifier.fillMaxWidth()) { content() }
    }
}
