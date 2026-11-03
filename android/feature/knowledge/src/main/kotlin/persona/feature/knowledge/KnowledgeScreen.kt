package persona.feature.knowledge

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import com.mikepenz.markdown.m3.Markdown
import com.mikepenz.markdown.model.State as MarkdownState
import com.mikepenz.markdown.model.parseMarkdown
import persona.core.contracts.api.*
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.net.URI
import java.time.OffsetDateTime
import java.time.format.DateTimeFormatter


@Composable
internal fun KnowledgeScreen(model: KnowledgeViewModel, navigator: Navigator) {
    val page by model.page.collectAsStateWithLifecycle()
    var tagMenu by remember { mutableStateOf(false) }
    var ignored by rememberSaveable(page.scope?.path) { mutableStateOf(false) }
    val search = page.filter.query.isNotBlank()
    JListPage("知识库", onBack = navigator::back, largeTitle = true, actions = {
        if (!search) JIconButton(PhIcons.FunnelSimple, page.filter.tag?.let { "筛选标签，当前为 $it" } ?: "筛选标签", { tagMenu = true })
        JIconButton(PhIcons.ArrowClockwise, "刷新知识库", model::refresh, enabled = !page.loading)
    }) {
        item("search") {
            JSearchField(page.filter.query, model::query, "搜索人、群聊和项目", Modifier.padding(vertical = 8.dp))
        }
        if (!search) item("kinds") {
            val kinds = listOf(KnowledgeKind.Projects, KnowledgeKind.Chats, KnowledgeKind.People)
            JSegmented(kinds.map { JSegment(it.title) }, kinds.indexOf(page.filter.kind), { model.kind(kinds[it]) }, Modifier.padding(vertical = 8.dp))
            page.filter.tag?.let { JTag("标签：#$it", Modifier.padding(bottom = 8.dp)) }
        }
        when {
            !page.paired -> item { JEmptyState("还没连接 hub", "配对后，就能查看 Jarvis 整理的知识库。") }
            page.loading -> item { JLoading() }
            page.message != null -> item { RetryState(requireNotNull(page.message), model::refresh) }
            search && page.groups.isEmpty() -> item { JEmptyState("没有找到相关内容", "试试其他关键词，搜索会包含档案和聊天记录。") }
            !search && page.entries.isEmpty() -> item { JEmptyState("暂无${page.filter.kind.title}",
                if (page.filter.tag != null) "试试其他标签，或查看全部。" else "hub 整理出档案后，会显示在这里。") }
            search -> items(page.groups, key = { it.entry.key }, contentType = { "search" }) { group ->
                KnowledgeRow(group.entry, onClick = { navigator.open(group.entry) }) {
                    group.hits.take(3).forEach { hit ->
                        Text(hit.line, maxLines = 3, overflow = TextOverflow.Ellipsis, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
                        Text(hit.path, maxLines = 1, overflow = TextOverflow.Ellipsis, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                    }
                    if (group.hits.size > 3) Text("共 ${group.hits.size} 处匹配", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                }
            }
            else -> {
                val sections = if (page.filter.kind == KnowledgeKind.Projects) projectSections(page.entries)
                    else listOf(KnowledgeSection("按重要级和相关性", page.entries))
                sections.forEach { section ->
                    item(key = "section/${section.title}", contentType = "section") {
                        JSectionHeader(section.title, count = section.entries.size,
                            action = if (section.collapsed) ({ JTextButton(if (ignored) "收起" else "展开", { ignored = !ignored }) }) else null)
                    }
                    if (!section.collapsed || ignored) items(section.entries, key = { it.key }, contentType = { "entry" }) {
                        KnowledgeRow(it, onClick = { navigator.open(it) })
                    }
                }
            }
        }
    }
    if (tagMenu) JActionSheet(listOf(JAction("全部标签", checked = page.filter.tag == null) { model.tag(null) }) +
        page.tags.map { tag -> JAction("#$tag", checked = page.filter.tag == tag) { model.tag(tag) } },
        { tagMenu = false }, "筛选标签")
}

@Composable
private fun KnowledgeRow(entry: KnowledgeEntry, onClick: () -> Unit, extra: @Composable ColumnScope.() -> Unit = {}) {
    JCard(Modifier.padding(bottom = 12.dp), onClick = onClick) {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(entry.name, style = PersonaTheme.type.title, color = PersonaTheme.palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
            EntryMetadata(entry)
            entry.oneLine?.takeIf { it.isNotBlank() }?.let {
                Text(it, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink2, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
            extra()
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun EntryMetadata(entry: KnowledgeEntry) {
    val labels = buildList {
        entry.importance?.takeIf { it.isNotBlank() }?.let(::add)
        if (entry.kind == "projects") {
            add(if (entry.extra.orEmpty().startsWith("long")) "长期项目" else "短期项目")
            if (entry.extra.orEmpty().endsWith("paused")) add("暂停")
        }
        add("相关性 ${entry.relevance.coerceIn(0, 100)}")
    }
    FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        labels.forEach { JTag(it) }
        entry.tags.orEmpty().take(3).forEach { JTag("#$it") }
    }
    val updated = remember(entry.updated) {
        runCatching { OffsetDateTime.parse(entry.updated).takeIf { it.year > 1970 }
            ?.format(DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm")) }.getOrNull()
    }
    updated?.let { Text("更新于 $it", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3) }
}

@Composable
internal fun KnowledgeDetailScreen(model: KnowledgeDetailViewModel, navigator: Navigator, openLinkInApp: (String) -> Unit) {
    val page by model.page.collectAsStateWithLifecycle()
    val sending by model.sending.collectAsStateWithLifecycle()
    var allLog by rememberSaveable(page.scope?.path) { mutableStateOf(false) }
    var recent by rememberSaveable(page.scope?.path) { mutableStateOf(false) }
    var question by rememberSaveable(page.scope?.path) { mutableStateOf("请说说最新进展、需要我处理的事，以及接下来该做什么。") }
    var linkMessage by remember(page.scope) { mutableStateOf<String?>(null) }
    val currentOpenLink by rememberUpdatedState(openLinkInApp)
    val uriHandler = remember(page.scope) {
        object : UriHandler {
            override fun openUri(uri: String) {
                val parsed = runCatching { URI(uri) }.getOrNull()
                if (parsed != null && parsed.scheme?.lowercase() in setOf("http", "https") && !parsed.host.isNullOrBlank() && parsed.userInfo == null) {
                    currentOpenLink(uri)
                } else linkMessage = "暂不支持打开这个链接"
            }
        }
    }
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(model, lifecycle, navigator) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            model.handoffs.collect { navigator.selectTab(AppTab.Jarvis) }
        }
    }
    CompositionLocalProvider(LocalUriHandler provides uriHandler) {
        JListPage(page.detail?.entry()?.name ?: "知识库详情", onBack = navigator::back, actions = {
            JIconButton(PhIcons.ArrowClockwise, "刷新详情", model::refresh, enabled = !page.loading)
        }) {
            when {
                !page.paired -> item { JEmptyState("还没连接 hub", "配对后可查看这份档案。") }
                page.loading -> item { JLoading() }
                page.message != null -> item { RetryState(requireNotNull(page.message), model::refresh) }
                else -> page.detail?.let { detail ->
                    val entry = detail.entry()
                    item("header", contentType = "header") {
                        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            EntryMetadata(entry)
                            entry.oneLine?.takeIf { it.isNotBlank() }?.let { Text(it, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink2) }
                            ComposerFrame(field = {
                                ComposerTextField(question, { question = it }, "想问 Jarvis 什么", enabled = !sending.busy)
                            })
                            JButton("问 Jarvis", { model.ask(question) }, Modifier.fillMaxWidth(), enabled = question.isNotBlank(),
                                loading = sending.busy, loadingText = "正在交给 Jarvis…")
                            sending.message?.let { JNotice(it) }
                            linkMessage?.let { JNotice(it) }
                        }
                    }
                    item("profile", contentType = "markdown") {
                        JSectionHeader("档案")
                        KnowledgeMarkdown(detail.summary?.takeIf { it.isNotBlank() } ?: "还没有整理出档案。")
                    }
                    val log = detail.log.orEmpty().asReversed()
                    if (log.isNotEmpty()) {
                        item("timeline", contentType = "section") { JSectionHeader("时间线", count = log.size) }
                        items(if (allLog) log.size else minOf(log.size, 5), key = { "log/$it" }, contentType = { "markdown" }) { index ->
                            TimelineMark(if (index == 0) "最新记录" else "更早的记录")
                            JGroupItem(GroupPosition.Single) {
                                JRow("进展记录", value = "${log.size - index}")
                                Box(Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) { KnowledgeMarkdown(log[index]) }
                            }
                        }
                        if (log.size > 5) item("more-log", contentType = "action") {
                            JTextButton(if (allLog) "收起时间线" else "查看全部 ${log.size} 条", { allLog = !allLog })
                        }
                    }
                    val links = detail.links.orEmpty().filter { it.inScope() }.distinctBy { it.key }
                    if (links.isNotEmpty()) {
                        item("related", contentType = "section") { JSectionHeader("关联档案") }
                        items(links, key = { "link/${it.key}" }, contentType = { "entry" }) { linked -> KnowledgeRow(linked, onClick = { navigator.open(linked) }) }
                    }
                    val dirs = detail.meta.strings("dirs")
                    if (dirs.isNotEmpty()) item("dirs", contentType = "text") {
                        JSectionHeader("代码目录")
                        SelectionContainer { Text(dirs.joinToString("\n"), style = PersonaTheme.type.code, color = PersonaTheme.palette.ink2) }
                    }
                    if (!detail.recent.isNullOrBlank()) {
                        item("recent-toggle", contentType = "action") {
                            JTextButton(if (recent) "收起聊天记录" else "最近七天的聊天记录", { recent = !recent })
                        }
                        if (recent) item("recent", contentType = "text") {
                            SelectionContainer { Text(detail.recent.orEmpty(), style = PersonaTheme.type.code, color = PersonaTheme.palette.ink2) }
                        }
                    }
                    item("path", contentType = "text") {
                        JSectionHeader("知识库目录")
                        SelectionContainer { Text(detail.path, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3) }
                    }
                }
            }
        }
    }
}

@Composable
private fun KnowledgeMarkdown(text: String) {
    val parsed by produceState<MarkdownState?>(null, text) {
        value = withContext(Dispatchers.Default) { parseMarkdown(text) }
    }
    SelectionContainer {
        parsed?.let { Markdown(it, modifier = Modifier.fillMaxWidth()) } ?: Text(text, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
    }
}

private fun Navigator.open(entry: KnowledgeEntry) {
    KnowledgeKind.fromWire(entry.kind)?.let { navigate(KnowledgeDetailRoute(it, entry.id)) }
}

@Composable private fun RetryState(message: String, retry: () -> Unit) {
    JEmptyState("暂时无法显示", message, action = { JButton("重试", retry, kind = JButtonKind.Secondary) })
}
