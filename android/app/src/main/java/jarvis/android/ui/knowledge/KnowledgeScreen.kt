package jarvis.android.ui.knowledge

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import jarvis.android.data.AgentState
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
import jarvis.android.ui.CardSlice
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.ChoiceChip
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.FittingRow
import jarvis.android.ui.design.GroupFooter
import jarvis.android.ui.design.IconTile
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.LetterTile
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.PageLoading
import jarvis.android.ui.design.SearchField
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.listTime
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
fun KnowledgeScreen(
    hub: Hub, onBack: () -> Unit, onAsk: (String) -> Unit, onOpenSession: (SessionKey) -> Unit,
    /** The list's state when it is made elsewhere (the screenshots load it first); by default the page makes its own. */
    model: KnowledgeListModel? = null,
) {
    val scope = rememberCoroutineScope()
    val list = remember { model ?: KnowledgeListModel(hub, scope) }
    val stack = remember { mutableStateListOf<KbEntry>() }
    BackHandler { if (stack.isNotEmpty()) stack.removeAt(stack.lastIndex) else onBack() }
    val top = stack.lastOrNull()
    if (top != null) {
        androidx.compose.runtime.key(stack.size, top.key) {
            KnowledgeDetailScreen(
                hub, top, onBack = { stack.removeAt(stack.lastIndex) }, onOpen = { stack.add(list.known(it)) },
                onAsk = onAsk, onOpenSession = onOpenSession,
                onChanged = { list.load() },
            )
        }
    } else {
        KnowledgeHome(list, onBack, onOpen = { stack.add(it) })
    }
}

/** The list page's state: one list per kind and tag, and the search; kept while an entry is open on top of it. */
class KnowledgeListModel(private val hub: Hub, private val scope: CoroutineScope) {
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

/**
 * The list (iOS `KnowledgeView`): the search field and the kinds stay on top, as on 收件箱; below, the sections as
 * cards — projects as 重点 / 进行中 / 已完成 with 不是项目 folded, the other kinds by importance and relevance.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun KnowledgeHome(model: KnowledgeListModel, onBack: () -> Unit, onOpen: (KbEntry) -> Unit) {
    val s by model.state.collectAsState()
    var showIgnored by rememberSaveable { mutableStateOf(false) }

    Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
        PageHeader("知识库", onBack) { TagMenu(s.tag, s.list?.tags.orEmpty(), model::selectTag) }
        Column(Modifier.fillMaxWidth().padding(bottom = JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
            SearchField(s.query, model::search, "搜索档案和聊天记录", Modifier.padding(horizontal = JSpace.page))
            // Search covers every kind: the chips (one of them lit) would say otherwise.
            if (s.query.isBlank()) KindChips(s.kind, s.lists, model::select)
        }
        PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { model.load(pull = true) }, modifier = Modifier.weight(1f).fillMaxWidth()) {
            val list = s.list
            val error = s.error
            LazyColumn(
                Modifier.fillMaxSize(),
                contentPadding = PaddingValues(bottom = JSpace.xxl + WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()),
            ) {
                when {
                    s.query.isNotBlank() -> searchResults(s.hits, s.query, model, onOpen)
                    list != null -> listContent(list, s.kind, s.tag, showIgnored, { showIgnored = !showIgnored }, onOpen)
                    error != null -> item(key = "error") {
                        EmptyState(JIcons.errorOutline, "读不到知识库", error, action = "重试" to { model.load() })
                    }
                    else -> item(key = "loading") { PageLoading() }
                }
            }
        }
    }
}

/** The six kinds as choices with their symbol; the chosen one says how many it holds once loaded. */
@Composable
private fun KindChips(kind: KbKind, lists: Map<KbKind, KbList>, onPick: (KbKind) -> Unit) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = JSpace.page),
        horizontalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        KbKind.entries.forEach { k ->
            val on = k == kind
            ChoiceChip(
                k.title, on, { onPick(k) },
                lead = { Icon(k.icon(), null, Modifier.size(JSize.markM), tint = if (on) c.accent else c.secondaryText) },
                count = if (on) lists[k]?.items?.size else null,
            )
        }
    }
}

/** The header's filter: 全部 or one of the kind's common tags; lit in the accent while a tag is picked. */
@Composable
private fun TagMenu(tag: String?, tags: List<String>, onPick: (String?) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        CircleButton(
            JIcons.filter, tag?.let { "按标签筛选，当前 #$it" } ?: "按标签筛选", { open = true },
            tint = if (tag != null) JTheme.colors.accent else JTheme.colors.label,
        )
        DropdownMenu(open, { open = false }) {
            TagItem("全部", tag == null) { open = false; onPick(null) }
            if (tags.isNotEmpty()) HorizontalDivider(thickness = JSize.hairline, color = JTheme.colors.separator)
            tags.forEach { t -> TagItem("#$t", tag == t) { open = false; onPick(t) } }
        }
    }
}

@Composable
private fun TagItem(text: String, on: Boolean, onClick: () -> Unit) {
    DropdownMenuItem(
        text = { Text(text, maxLines = 1) }, onClick = onClick,
        leadingIcon = { if (on) Icon(JIcons.done, null, tint = JTheme.colors.accent) else Spacer(Modifier.size(JSize.icon)) },
    )
}

private fun LazyListScope.listContent(
    list: KbList, kind: KbKind, tag: String?, showIgnored: Boolean, toggleIgnored: () -> Unit, onOpen: (KbEntry) -> Unit,
) {
    if (list.items.isEmpty()) {
        item(key = "empty") {
            EmptyState(
                kind.icon(), if (tag == null) "还没有${kind.title}" else "没有带 #$tag 的${kind.title}",
                "hub 会把飞书和邮件里的会话归档、压缩，自动整理出项目、人、文档和会议，并按重要级和相关性排好。",
            )
        }
        return
    }
    if (kind != KbKind.PROJECTS) {
        item(key = "h") { SectionHeader(tag?.let { "#$it" } ?: "按重要级和相关性", count = list.items.size) }
        entryCard("e", list.items, onOpen)
        return
    }
    val g = KbProjectGroups.of(list.items)
    if (g.focus.isNotEmpty()) { item(key = "focus:h") { SectionHeader("重点", count = g.focus.size) }; entryCard("f", g.focus, onOpen) }
    if (g.rest.isNotEmpty()) {
        item(key = "rest:h") { SectionHeader(if (g.focus.isEmpty()) "进行中" else "其他进行中", count = g.rest.size) }
        entryCard("r", g.rest, onOpen)
    }
    if (g.done.isNotEmpty()) { item(key = "done:h") { SectionHeader("已完成", count = g.done.size) }; entryCard("d", g.done, onOpen) }
    if (g.ignored.isNotEmpty()) {
        // Its own card, folded: a row that opens it, the entries under it in the same card.
        item(key = "ig:toggle") {
            Column(Modifier.padding(top = JSpace.section)) {
                CardSlice(first = true, last = !showIgnored) { FoldRow("不是项目", g.ignored.size, showIgnored, toggleIgnored) }
            }
        }
        if (showIgnored) entryCard("i", g.ignored, onOpen, startsCard = false)
        item(key = "ig:f") { GroupFooter("只在通知、营销邮件、机器人会话里出现的，不会再更新。打标里改回「进行中」就恢复。") }
    }
}

/**
 * Entries as one card, each row its own lazy item drawing its slice of it; [startsCard] false: under a row above (the
 * fold's), whose words start at the card's inset, so the first separator does too.
 */
private fun LazyListScope.entryCard(prefix: String, entries: List<KbEntry>, onOpen: (KbEntry) -> Unit, startsCard: Boolean = true) {
    itemsIndexed(entries, key = { _, e -> "$prefix:" + e.key }) { i, e ->
        CardSlice(
            first = startsCard && i == 0, last = i == entries.lastIndex, divider = !startsCard || i > 0,
            dividerStart = if (i == 0) JSpace.l else KbTextColumn,
        ) { KbRow(e) { onOpen(e) } }
    }
}

/** A folded group's row: its name and count quiet, the chevron turning down when open. */
@Composable
private fun FoldRow(title: String, count: Int, open: Boolean, onClick: () -> Unit) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).heightIn(min = JSize.box50).padding(horizontal = JSpace.l, vertical = JSpace.s)
            .semantics { contentDescription = "$title $count 个，" + if (open) "收起" else "展开" },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
    ) {
        Text(title, style = JTheme.type.body, color = c.secondaryText, maxLines = 1)
        Text("$count", style = JTheme.type.body, color = c.tertiaryText, maxLines = 1, modifier = Modifier.weight(1f))
        Icon(if (open) JIcons.expand else JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = c.tertiaryText)
    }
}

private fun LazyListScope.searchResults(hits: List<KbHit>?, query: String, model: KnowledgeListModel, onOpen: (KbEntry) -> Unit) {
    val groups = hits?.let(KbSearchGroup::make)
    when {
        groups == null -> item(key = "searching") { PageLoading() }
        // Hits outside any entry (the directory's README) open nothing: those alone are no result either.
        groups.isEmpty() -> item(key = "nohits") { EmptyState(JIcons.search, "没有找到", "档案和聊天记录里没有「${query.trim()}」。") }
        else -> for (k in KbKind.entries) {
            val these = groups.filter { it.kind == k }
            if (these.isEmpty()) continue
            item(key = "sh:" + k.wire) { SectionHeader(k.title, count = these.size) }
            itemsIndexed(these, key = { _, g -> "s:" + g.key }) { i, g ->
                val known = model.known(g.kind, g.id)
                val entry = known ?: KbEntry(kind = g.kind.wire, id = g.id, name = g.id)
                CardSlice(first = i == 0, last = i == these.lastIndex, dividerStart = HitTextColumn) {
                    HitRow(entry, named = known != null, group = g, query = query) { onOpen(entry) }
                }
            }
        }
    }
}

/** Where a row's words start (after its tile): the separators start there too. */
private val KbTextColumn: Dp = JSpace.l + JSize.wellL + JSpace.m
private val HitTextColumn: Dp = JSpace.l + JSize.wellS + JSpace.m

internal fun KbKind.icon(): ImageVector = when (this) {
    KbKind.PROJECTS -> JIcons.project
    KbKind.CHATS -> JIcons.feishuChats
    KbKind.PEOPLE -> JIcons.people
    KbKind.DOCS -> JIcons.document
    KbKind.MEETINGS -> JIcons.meeting
    KbKind.EVENTS -> JIcons.event
}

/** P0 red, P1 amber (the 需要你 and 工作中 tones), P2 blue, P3 and unranked gray (iOS `ImportanceBadge.color`). */
@Composable
internal fun importanceColor(p: String?): Color = when (p) {
    "P0" -> JTheme.needsYou.dot
    "P1" -> JTheme.tone(AgentState.Working).dot
    "P2" -> JTheme.hue.blue
    else -> JTheme.colors.secondaryText
}

/** A pill in an importance's color: its words dark enough to read on its soft fill. */
@Composable
private fun importanceLook(p: String?): ChipLook = when (p) {
    "P0" -> JTheme.needsYou.let { ChipLook.Tint(it.tint, it.text) }
    "P1" -> JTheme.tone(AgentState.Working).let { ChipLook.Tint(it.tint, it.text) }
    "P2" -> ChipLook.Tint(JTheme.hue.blue)
    else -> ChipLook.Neutral
}

@Composable
internal fun ImportanceBadge(importance: String?) {
    val p = importance.orEmpty()
    if (p.isNotEmpty()) Chip(p, Modifier.clearAndSetSemantics { contentDescription = "重要级 $p" }, look = importanceLook(p))
}

/** 与我相关: high in the 需要你 red, middling amber, low neutral (iOS `RelevanceMeter`). */
@Composable
internal fun RelevanceBadge(relevance: Int) {
    val look = when {
        relevance >= 70 -> JTheme.needsYou.let { ChipLook.Tint(it.tint, it.text) }
        relevance >= 40 -> JTheme.tone(AgentState.Working).let { ChipLook.Tint(it.tint, it.text) }
        else -> ChipLook.Neutral
    }
    Chip("相关 $relevance", look = look)
}

/** 短期 / 长期, the long ones in blue; [suffix] 「项目」 on the entry's page. */
@Composable
internal fun ProjectKindBadge(kind: String, suffix: String = "") {
    Chip(kind + suffix, look = if (kind == "长期") ChipLook.Tint(JTheme.hue.blue) else ChipLook.Neutral)
}

/** A rounded tile: the kind's symbol, or a person's / chat's first character; in the importance's color. */
@Composable
internal fun KbAvatar(entry: KbEntry, size: Dp = JSize.wellL) {
    val tint = importanceColor(entry.importance)
    val kind = KbKind.of(entry.kind)
    val letter = entry.name.firstOrNull { !it.isWhitespace() && it !in "#@（(【[「『《〈\"'“‘" }
    val hidden = Modifier.clearAndSetSemantics {}
    if ((kind == KbKind.PEOPLE || kind == KbKind.CHATS) && letter != null) LetterTile(letter.uppercase(), tint, hidden, size)
    else IconTile(kind?.icon() ?: JIcons.project, tint, hidden, size, filled = false)
}

/**
 * One ranked entry (iOS `KBRow`): the tile carrying its kind and importance, the name and when it changed, its
 * one-line state, then its importance, kind, relevance and as many whole tags as fit.
 */
@Composable
internal fun KbRow(entry: KbEntry, onClick: () -> Unit) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(start = JSpace.l, end = JSpace.m, top = JSpace.m, bottom = JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Row(Modifier.weight(1f), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
            KbAvatar(entry)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    // Two lines: documents and meetings are told apart by the end of their titles.
                    Text(entry.name, style = JTheme.type.heading, color = c.label, maxLines = 2, overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f).alignByBaseline())
                    entry.updated?.let {
                        Text(listTime(it), style = JTheme.type.label, color = c.tertiaryText, maxLines = 1, modifier = Modifier.alignByBaseline())
                    }
                }
                entry.oneLine?.takeIf { it.isNotEmpty() }?.let {
                    Text(it, style = JTheme.type.secondary, color = c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
                FittingRow(Modifier.padding(top = JSpace.xxs)) {
                    ImportanceBadge(entry.importance)
                    entry.projectKind?.let { ProjectKindBadge(it) }
                    if (entry.relevance > 0) RelevanceBadge(entry.relevance)
                    entry.tags.orEmpty().forEach { t -> Text("#$t", style = JTheme.type.label, color = c.secondaryText, maxLines = 1, softWrap = false) }
                }
            }
        }
        Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = c.tertiaryText)
    }
}

/** One entry found by a search (iOS `KBHitRow`): its tile and name, then up to two places with the words in bold. */
@Composable
private fun HitRow(entry: KbEntry, named: Boolean, group: KbSearchGroup, query: String, onClick: () -> Unit) {
    val c = JTheme.colors
    val strong = JTheme.type.secondaryStrong.toSpanStyle().copy(color = c.label)
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(start = JSpace.l, end = JSpace.m, top = JSpace.m, bottom = JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Row(Modifier.weight(1f), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
            KbAvatar(entry, JSize.wellS)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    Text(if (named) entry.name else group.kind.title, style = JTheme.type.secondaryStrong, color = c.label, maxLines = 1,
                        overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f).alignByBaseline())
                    if (group.hits.size > 2) {
                        Text("${group.hits.size} 处", style = JTheme.type.label, color = c.tertiaryText, maxLines = 1, modifier = Modifier.alignByBaseline())
                    }
                }
                group.hits.take(2).forEach { h ->
                    Column(verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
                        Text(h.source, style = JTheme.type.label, color = c.tertiaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(marked(h.text, query, strong), style = JTheme.type.secondary, color = c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
        }
        Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = c.tertiaryText)
    }
}

/** [text] with every word of [query] (any case) in [strong]: the hub matches lines with all the words (iOS `marked`). */
internal fun marked(text: String, query: String, strong: SpanStyle): AnnotatedString {
    val words = query.split(Regex("\\s+")).filter { it.isNotEmpty() }
    if (words.isEmpty()) return AnnotatedString(text)
    val on = BooleanArray(text.length)
    for (w in words) {
        var from = 0
        while (true) {
            val i = text.indexOf(w, from, ignoreCase = true)
            if (i < 0) break
            for (j in i until i + w.length) on[j] = true
            from = i + w.length
        }
    }
    return buildAnnotatedString {
        var i = 0
        while (i < text.length) {
            var j = i
            while (j < text.length && on[j] == on[i]) j++
            if (on[i]) withStyle(strong) { append(text, i, j) } else append(text, i, j)
            i = j
        }
    }
}
