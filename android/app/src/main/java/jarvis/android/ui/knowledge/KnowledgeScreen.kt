package jarvis.android.ui.knowledge

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.automirrored.outlined.Article
import androidx.compose.material.icons.filled.Cancel
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.FilterAlt
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.FilterAlt
import androidx.compose.material.icons.outlined.Forum
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.material.icons.outlined.Videocam
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.Hub
import jarvis.android.data.KbEntry
import jarvis.android.data.KbHit
import jarvis.android.data.KbKind
import jarvis.android.data.KbList
import jarvis.android.data.KbProjectGroups
import jarvis.android.data.KbSearchGroup
import jarvis.android.data.SessionKey
import jarvis.android.data.kbList
import jarvis.android.data.kbSearch
import jarvis.android.ui.ago
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * What Jarvis knows (the hub's ~/.jarvis/data, docs/knowledge.md; iOS `KnowledgeView`), opened from the Jarvis tab's
 * drawer: projects first, then chats, people, docs, meetings and events, most important and most relevant first.
 * Not a browser of Feishu (CLAUDE.md「不做第二个飞书」): every row is Jarvis' own digest, and the way out is handing it
 * to Jarvis ([onAsk]) or an Agent. Entries open on a stack of their own (a detail's links push further entries).
 */
@Composable
fun KnowledgeScreen(hub: Hub, onBack: () -> Unit, onAsk: (String) -> Unit, onOpenSession: (SessionKey) -> Unit) {
    val scope = rememberCoroutineScope()
    val model = remember { KnowledgeListModel(hub, scope) }
    val stack = remember { mutableStateListOf<KbEntry>() }
    BackHandler { if (stack.isNotEmpty()) stack.removeAt(stack.lastIndex) else onBack() }
    val top = stack.lastOrNull()
    if (top != null) {
        androidx.compose.runtime.key(stack.size, top.key) {
            KnowledgeDetailScreen(
                hub, top, onBack = { stack.removeAt(stack.lastIndex) }, onOpen = { stack.add(model.known(it)) },
                onAsk = onAsk, onOpenSession = onOpenSession,
                onChanged = { model.load() },
            )
        }
    } else {
        KnowledgeHome(model, onBack, onOpen = { stack.add(it) })
    }
}

/** The list page's state: one list per kind and tag, and the search; kept while an entry is open on top of it. */
internal class KnowledgeListModel(private val hub: Hub, private val scope: CoroutineScope) {
    data class State(
        val kind: KbKind = KbKind.PROJECTS,
        val tag: String? = null,
        val lists: Map<KbKind, KbList> = emptyMap(),
        val error: String? = null,
        val refreshing: Boolean = false,
        val query: String = "",
        val hits: List<KbHit>? = null,
    ) {
        val list: KbList? get() = lists[kind]
    }

    private val _state = MutableStateFlow(State())
    val state: StateFlow<State> = _state.asStateFlow()
    private var loadJob: Job? = null
    private var searchJob: Job? = null
    /** Entries by "kind/id" from every list loaded: search hits and links carry little more than an id. */
    private val index = HashMap<String, KbEntry>()

    init { load() }

    fun known(e: KbEntry): KbEntry = index[e.key] ?: e
    fun known(kind: KbKind, id: String): KbEntry? = index[kind.wire + "/" + id]

    fun select(kind: KbKind) {
        if (kind == _state.value.kind) return
        _state.update { it.copy(kind = kind, tag = null, error = null) }
        load()
    }

    fun selectTag(tag: String?) {
        if (tag == _state.value.tag) return
        _state.update { it.copy(tag = tag, lists = it.lists - it.kind, error = null) }
        load()
    }

    fun load(pull: Boolean = false) {
        val (kind, tag) = _state.value.let { it.kind to it.tag }
        loadJob?.cancel()
        loadJob = scope.launch {
            if (pull) _state.update { it.copy(refreshing = true) }
            runCatching { hub.api.kbList(kind.wire, tag) }
                .onSuccess { list ->
                    list.items.forEach { index[it.key] = it }
                    _state.update { s -> if (s.kind == kind && s.tag == tag) s.copy(lists = s.lists + (kind to list), error = null) else s }
                }
                .onFailure { e ->
                    if (e is CancellationException) throw e
                    _state.update { s -> if (s.kind == kind && s.tag == tag) s.copy(error = e.message ?: "出错了") else s }
                }
            if (pull) _state.update { it.copy(refreshing = false) }
        }
    }

    fun search(q: String) {
        _state.update { it.copy(query = q, hits = if (q.isBlank()) null else it.hits) }
        searchJob?.cancel()
        if (q.isBlank()) return
        searchJob = scope.launch {
            delay(300) // typing
            val hits = runCatching { hub.api.kbSearch(q.trim()) }.getOrElse { if (it is CancellationException) throw it; emptyList() }
            // Names for the hits: the lists of the kinds they are in, once each.
            for (k in hits.mapNotNull { it.entry?.first }.toSet()) {
                if (index.keys.none { it.startsWith(k.wire + "/") }) runCatching { hub.api.kbList(k.wire) }.getOrNull()?.items?.forEach { index[it.key] = it }
            }
            _state.update { s -> if (s.query == q) s.copy(hits = hits) else s }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun KnowledgeHome(model: KnowledgeListModel, onBack: () -> Unit, onOpen: (KbEntry) -> Unit) {
    val s by model.state.collectAsState()
    var showIgnored by rememberSaveable { mutableStateOf(false) }
    var tagMenu by remember { mutableStateOf(false) }

    Scaffold(topBar = {
        TopAppBar(
            title = { Text("知识库", maxLines = 1) },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
            actions = {
                Box {
                    IconButton(onClick = { tagMenu = true }) {
                        Icon(if (s.tag == null) Icons.Outlined.FilterAlt else Icons.Filled.FilterAlt, s.tag?.let { "按标签筛选，当前 #$it" } ?: "按标签筛选")
                    }
                    DropdownMenu(tagMenu, { tagMenu = false }) {
                        DropdownMenuItem(
                            text = { Text("全部", maxLines = 1) }, onClick = { tagMenu = false; model.selectTag(null) },
                            leadingIcon = { if (s.tag == null) Icon(Icons.Default.Check, null) else Spacer(Modifier.size(24.dp)) },
                        )
                        val tags = s.list?.tags.orEmpty()
                        if (tags.isNotEmpty()) HorizontalDivider()
                        tags.forEach { t ->
                            DropdownMenuItem(
                                text = { Text("#$t", maxLines = 1) }, onClick = { tagMenu = false; model.selectTag(t) },
                                leadingIcon = { if (s.tag == t) Icon(Icons.Default.Check, null) else Spacer(Modifier.size(24.dp)) },
                            )
                        }
                    }
                }
            },
        )
    }) { pad ->
        PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { model.load(pull = true) }, modifier = Modifier.fillMaxSize().padding(pad)) {
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item(key = "search") {
                    OutlinedTextField(
                        s.query, model::search, singleLine = true,
                        placeholder = { Text("搜索档案和聊天记录", maxLines = 1) },
                        leadingIcon = { Icon(Icons.Default.Search, null) },
                        trailingIcon = { if (s.query.isNotEmpty()) IconButton(onClick = { model.search("") }) { Icon(Icons.Default.Cancel, "清除") } },
                        shape = RoundedCornerShape(24.dp),
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                    )
                }
                // Search covers every kind: the chips (one of them lit) would say otherwise.
                if (s.query.isBlank()) item(key = "kinds") {
                    LazyRow(contentPadding = PaddingValues(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        items(KbKind.entries, key = { it.wire }) { k ->
                            val on = s.kind == k
                            val n = s.lists[k]?.items?.size
                            FilterChip(
                                selected = on, onClick = { model.select(k) },
                                leadingIcon = { Icon(k.icon(), null, Modifier.size(18.dp)) },
                                label = { Text(if (on && n != null) "${k.title} $n" else k.title, maxLines = 1) },
                            )
                        }
                    }
                }
                val list = s.list
                when {
                    s.query.isNotBlank() -> searchResults(s.hits, s.query, model, onOpen)
                    list != null -> listContent(list, s.kind, s.tag, showIgnored, { showIgnored = !showIgnored }, onOpen)
                    s.error != null -> item(key = "error") {
                        Column(Modifier.fillMaxWidth().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                            Text("读不到知识库：${s.error}", color = MaterialTheme.colorScheme.error)
                            TextButton(onClick = { model.load() }) { Text("重试", maxLines = 1) }
                        }
                    }
                    else -> item(key = "loading") { Loading() }
                }
            }
        }
    }
}

private fun LazyListScope.listContent(
    list: KbList, kind: KbKind, tag: String?, showIgnored: Boolean, toggleIgnored: () -> Unit, onOpen: (KbEntry) -> Unit,
) {
    if (list.items.isEmpty()) {
        item(key = "empty") {
            Column(Modifier.fillMaxWidth().padding(horizontal = 32.dp, vertical = 48.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Icon(kind.icon(), null, Modifier.size(40.dp), tint = MaterialTheme.colorScheme.outline)
                Spacer(Modifier.size(12.dp))
                Text(if (tag == null) "还没有${kind.title}" else "没有带 #$tag 的${kind.title}", style = MaterialTheme.typography.titleMedium)
                Spacer(Modifier.size(6.dp))
                Text(
                    "hub 会把飞书和邮件里的会话归档、压缩，自动整理出项目、人、文档和会议，并按重要级和相关性排好。",
                    style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        return
    }
    fun rows(prefix: String, entries: List<KbEntry>) = items(entries, key = { prefix + it.key }) { e -> KbRow(e) { onOpen(e) } }
    if (kind != KbKind.PROJECTS) {
        item(key = "h") { SectionTitle(tag?.let { "#$it" } ?: "按重要级和相关性", list.items.size) }
        rows("e:", list.items)
        return
    }
    val g = KbProjectGroups.of(list.items)
    if (g.focus.isNotEmpty()) { item(key = "focus:h") { SectionTitle("重点", g.focus.size) }; rows("f:", g.focus) }
    if (g.rest.isNotEmpty()) { item(key = "rest:h") { SectionTitle(if (g.focus.isEmpty()) "进行中" else "其他进行中", g.rest.size) }; rows("r:", g.rest) }
    if (g.done.isNotEmpty()) { item(key = "done:h") { SectionTitle("已完成", g.done.size) }; rows("d:", g.done) }
    if (g.ignored.isNotEmpty()) {
        item(key = "ig:toggle") {
            Row(
                Modifier.fillMaxWidth().clickable(onClick = toggleIgnored).padding(horizontal = 16.dp, vertical = 14.dp)
                    .semantics { contentDescription = "不是项目 ${g.ignored.size} 个，" + if (showIgnored) "收起" else "展开" },
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("不是项目", color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                Text("  ${g.ignored.size}", color = MaterialTheme.colorScheme.outline, maxLines = 1)
                Spacer(Modifier.weight(1f))
                Icon(if (showIgnored) Icons.Default.KeyboardArrowDown else Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = MaterialTheme.colorScheme.outline)
            }
        }
        if (showIgnored) rows("i:", g.ignored)
        item(key = "ig:f") { Note("只在通知、营销邮件、机器人会话里出现的，不会再更新。打标里改回「进行中」就恢复。") }
    }
}

private fun LazyListScope.searchResults(hits: List<KbHit>?, query: String, model: KnowledgeListModel, onOpen: (KbEntry) -> Unit) {
    when {
        hits == null -> item(key = "searching") { Loading() }
        // Hits outside any entry (the directory's README) open nothing: those alone are no result either.
        KbSearchGroup.make(hits).isEmpty() -> item(key = "nohits") { Note("没有找到「${query.trim()}」") }
        else -> {
            val groups = KbSearchGroup.make(hits)
            for (k in KbKind.entries) {
                val these = groups.filter { it.kind == k }
                if (these.isEmpty()) continue
                item(key = "sh:" + k.wire) { SectionTitle(k.title, these.size) }
                items(these, key = { "s:" + it.key }) { g ->
                    val known = model.known(g.kind, g.id)
                    val entry = known ?: KbEntry(kind = g.kind.wire, id = g.id, name = g.id)
                    HitRow(entry, named = known != null, group = g, query = query) { onOpen(entry) }
                }
            }
        }
    }
}

internal fun KbKind.icon(): ImageVector = when (this) {
    KbKind.PROJECTS -> Icons.Outlined.Layers
    KbKind.CHATS -> Icons.Outlined.Forum
    KbKind.PEOPLE -> Icons.Outlined.Groups
    KbKind.DOCS -> Icons.AutoMirrored.Outlined.Article
    KbKind.MEETINGS -> Icons.Outlined.Videocam
    KbKind.EVENTS -> Icons.Outlined.CalendarMonth
}

@Composable
internal fun Loading() {
    Box(Modifier.fillMaxWidth().padding(top = 40.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(28.dp), strokeWidth = 2.5.dp) }
}

@Composable
internal fun Note(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp))
}

/** A section title and, dimmer, how many are in it. */
@Composable
internal fun SectionTitle(text: String, count: Int? = null) {
    Row(Modifier.padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, maxLines = 1)
        if (count != null) Text("  $count", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.outline, maxLines = 1)
    }
}

/** P0 red, P1 orange, P2 blue, P3 and unranked grey (iOS `ImportanceBadge.color`). */
@Composable
internal fun importanceColor(p: String?): Color = when (p) {
    "P0" -> Color(0xFFFF453A)
    "P1" -> Color(0xFFFF9F0A)
    "P2" -> Color(0xFF0A84FF)
    else -> MaterialTheme.colorScheme.onSurfaceVariant
}

/** The color a relevance pill takes: high red, middling orange, low neutral. */
internal fun relevanceTint(v: Int): Color? = when {
    v >= 70 -> Color(0xFFFF453A)
    v >= 40 -> Color(0xFFFF9F0A)
    else -> null
}

/** A small capsule: tinted text on a soft tint of the same color, or neutral. */
@Composable
internal fun KbPill(text: String, tint: Color? = null, bold: Boolean = false) {
    val c = tint ?: MaterialTheme.colorScheme.onSurfaceVariant
    Text(
        text, fontSize = 11.sp, fontWeight = if (bold) FontWeight.Bold else FontWeight.Medium, color = c, maxLines = 1, softWrap = false,
        modifier = Modifier.background(c.copy(alpha = if (tint == null) 0.10f else 0.14f), CircleShape).padding(horizontal = 7.dp, vertical = 2.dp),
    )
}

@Composable
internal fun ImportanceBadge(importance: String?) {
    val p = importance.orEmpty()
    if (p.isNotEmpty()) Box(Modifier.clearAndSetSemantics { contentDescription = "重要级 $p" }) { KbPill(p, importanceColor(p), bold = true) }
}

/** A rounded tile: the kind's icon, or a person's / chat's first character; tinted by importance. */
@Composable
internal fun KbAvatar(entry: KbEntry, size: Dp = 40.dp) {
    val tint = importanceColor(entry.importance)
    val kind = KbKind.of(entry.kind)
    Box(
        Modifier.size(size).background(tint.copy(alpha = 0.14f), RoundedCornerShape(size * 0.28f)).clearAndSetSemantics {},
        contentAlignment = Alignment.Center,
    ) {
        val c = entry.name.firstOrNull { !it.isWhitespace() && it !in "#@（(【[「『《〈\"'“‘" }
        if ((kind == KbKind.PEOPLE || kind == KbKind.CHATS) && c != null) {
            Text(c.uppercase(), color = tint, fontWeight = FontWeight.SemiBold, fontSize = (size.value * 0.42f).sp, maxLines = 1)
        } else {
            Icon(kind?.icon() ?: Icons.Outlined.Layers, null, tint = tint, modifier = Modifier.size(size * 0.5f))
        }
    }
}

/** One ranked entry: a tile carrying the importance, the name and when it changed, its one-line state, then pills and tags. */
@Composable
internal fun KbRow(entry: KbEntry, onClick: () -> Unit) {
    val cs = MaterialTheme.colorScheme
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        KbAvatar(entry)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(entry.name, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                entry.updated?.let { Text(ago(it), fontSize = 11.sp, color = cs.outline, maxLines = 1, modifier = Modifier.padding(start = 8.dp)) }
            }
            entry.oneLine?.takeIf { it.isNotEmpty() }?.let {
                Text(it, style = MaterialTheme.typography.bodyMedium, color = cs.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            Row(Modifier.padding(top = 2.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                ImportanceBadge(entry.importance)
                entry.projectKind?.let { KbPill(it, if (it == "长期") Color(0xFF0A84FF) else null) }
                if (entry.relevance > 0) KbPill("相关 ${entry.relevance}", relevanceTint(entry.relevance))
                // Whole tags only, as many as fit (a cut "#Manage…" says nothing): the rest are dropped.
                entry.tags.orEmpty().take(2).forEach { t ->
                    Text("#$t", fontSize = 11.sp, color = cs.onSurfaceVariant, maxLines = 1, softWrap = false, overflow = TextOverflow.Clip, modifier = Modifier.weight(1f, fill = false))
                }
            }
        }
    }
}

/** One entry found by a search: its tile and name, then up to two matching lines with what was searched for in bold. */
@Composable
private fun HitRow(entry: KbEntry, named: Boolean, group: KbSearchGroup, query: String, onClick: () -> Unit) {
    val cs = MaterialTheme.colorScheme
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        KbAvatar(entry, 34.dp)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(if (named) entry.name else group.kind.title, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                if (group.hits.size > 2) Text("  ${group.hits.size} 处", fontSize = 11.sp, color = cs.outline, maxLines = 1)
            }
            group.hits.take(2).forEach { h ->
                Text(h.source, fontSize = 11.sp, color = cs.outline, maxLines = 1)
                Text(marked(h.text, query, cs.onSurface), style = MaterialTheme.typography.bodyMedium, color = cs.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/** [text] with every case-insensitive occurrence of [query] bold and in [strong]. */
internal fun marked(text: String, query: String, strong: Color): AnnotatedString = buildAnnotatedString {
    val q = query.trim()
    if (q.isEmpty()) { append(text); return@buildAnnotatedString }
    var from = 0
    while (true) {
        val i = text.indexOf(q, from, ignoreCase = true)
        if (i < 0) break
        append(text.substring(from, i))
        withStyle(SpanStyle(color = strong, fontWeight = FontWeight.SemiBold)) { append(text.substring(i, i + q.length)) }
        from = i + q.length
    }
    append(text.substring(from))
}
