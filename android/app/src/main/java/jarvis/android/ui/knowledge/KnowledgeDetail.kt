package jarvis.android.ui.knowledge

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import jarvis.android.data.Hub
import jarvis.android.data.KbContext
import jarvis.android.data.KbDetail
import jarvis.android.data.KbDetailResponse
import jarvis.android.data.KbEntry
import jarvis.android.data.KbKind
import jarvis.android.data.KbLogLine
import jarvis.android.data.KbUpdate
import jarvis.android.data.SessionKey
import jarvis.android.data.kbDetail
import jarvis.android.data.kbUpdate
import jarvis.android.ui.AgentRow
import jarvis.android.ui.JTheme
import jarvis.android.ui.Markdown
import jarvis.android.ui.NewSessionSheet
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChoiceChip
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.GroupFooter
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSlider
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.PageLoading
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.tailOf
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

/** Timeline lines shown before 显示全部, related entries of one kind before 全部 (as iOS). */
private const val LOG_SHOWN = 6
private const val LINKS_SHOWN = 4

/**
 * One entry's page (iOS `KnowledgeDetailView`): the tile, name, badges, one line and tags with 问 Jarvis / 开 Agent;
 * then its Agents, the timeline, the profile, what it relates to, the recent messages and where it lives on disk.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun KnowledgeDetailScreen(
    hub: Hub, start: KbEntry, onBack: () -> Unit, onOpen: (KbEntry) -> Unit,
    onAsk: (String) -> Unit, onOpenSession: (SessionKey) -> Unit, onChanged: () -> Unit,
    /** What the hub already said about the entry (the screenshots load it first): shown at once, then loaded again. */
    preloaded: KbDetailResponse? = null,
    /** Where the page is scrolled to (the screenshots open it further down, like iOS's `-showKnowledgeLower`). */
    listState: LazyListState = rememberLazyListState(),
) {
    var detail by remember { mutableStateOf(preloaded) }
    var error by remember { mutableStateOf<String?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    var attempt by remember { mutableIntStateOf(0) }
    var editing by remember { mutableStateOf(false) }
    var newAgent by remember { mutableStateOf(false) }
    LaunchedEffect(attempt) {
        runCatching { hub.api.kbDetail(start.kind, start.id) }
            .onSuccess { detail = it; error = null }
            .onFailure { if (it is CancellationException) throw it; error = it.message ?: "出错了" }
        refreshing = false
    }
    val machines by hub.machines.collectAsState()
    val d = detail?.detail
    val e = d?.merged(start) ?: start
    val kindTitle = KbKind.of(start.kind)?.title.orEmpty()
    val isProject = start.kind == KbKind.PROJECTS.wire
    val failed = error

    Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
        PageHeader(kindTitle, onBack) { CircleButton(JIcons.tag, "打标", { editing = true }, enabled = d != null) }
        PullToRefreshBox(isRefreshing = refreshing, onRefresh = { refreshing = true; attempt++ }, modifier = Modifier.weight(1f).fillMaxWidth()) {
            LazyColumn(
                Modifier.fillMaxSize(), state = listState,
                contentPadding = PaddingValues(bottom = JSpace.xxl + WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()),
            ) {
                item(key = "head") {
                    Header(e, isProject, onAsk = { onAsk(KbContext.ask(e, d)) }, onAgent = { newAgent = true })
                }
                when {
                    d != null -> {
                        val agents = detail?.agents.orEmpty()
                        if (agents.isNotEmpty()) {
                            item(key = "agents:h") { SectionHeader("在做这个项目的 Agent", count = agents.size) }
                            item(key = "agents") {
                                GroupCard {
                                    agents.forEachIndexed { i, s ->
                                        if (i > 0) RowDivider(AgentTextColumn)
                                        AgentRow(
                                            s, machines.firstOrNull { it.id == s.machineId }?.displayName ?: s.machineId, muted = false, showWhere = true,
                                            onClick = { onOpenSession(it.key) }, onLongClick = {},
                                        )
                                    }
                                }
                            }
                        }
                        timeline(d.log.orEmpty())
                        // Empty once the header's parts are taken out (a summary that is just the one line): no blank card.
                        d.summary?.let { KbContext.profile(it, e.oneLine) }?.takeIf { it.isNotBlank() }?.let { profile ->
                            item(key = "summary:h") { SectionHeader("档案") }
                            item(key = "summary") {
                                GroupCard {
                                    Markdown(profile, Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m), color = JTheme.colors.label)
                                }
                            }
                        }
                        links(d, onOpen)
                        d.recent?.takeIf { it.isNotBlank() }?.let { recent -> item(key = "recent") { Recent(recent) } }
                        item(key = "path") {
                            Column {
                                SectionHeader("目录")
                                GroupCard {
                                    SelectionContainer {
                                        Text(d.path, style = JTheme.type.code, color = JTheme.colors.secondaryText,
                                            modifier = Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m))
                                    }
                                }
                                GroupFooter("Jarvis、AI 分身和各台电脑上的 Agent 都能直接读这个目录。")
                            }
                        }
                    }
                    failed != null -> item(key = "error") {
                        EmptyState(JIcons.errorOutline, "读不到这一项", failed, action = "重试" to { attempt++ })
                    }
                    else -> item(key = "loading") { PageLoading() }
                }
            }
        }
    }
    if (editing) KbTagEditor(hub, e, onDismiss = { editing = false }, onSaved = { editing = false; attempt++; onChanged() })
    if (newAgent) {
        val at = KbContext.firstDir(d?.dirs.orEmpty())
        NewSessionSheet(
            hub, machines.filter { it.online }, onDismiss = { newAgent = false },
            onStarted = { newAgent = false; onOpenSession(it) },
            initialPrompt = KbContext.context(e, d) + "\n\n", initialDir = at,
        )
    }
}

/** Where the words of the cards' rows start (after the state dot, the tile, the folder): their separators start there. */
private val AgentTextColumn: Dp = JSpace.l + JSize.dotL + JSpace.m
private val LinkTextColumn: Dp = JSpace.l + JSize.wellS + JSpace.m
private val DirTextColumn: Dp = JSpace.l + JSize.iconXS + JSpace.m

/** The top of the page, on the page itself: who or what it is, how it ranks, and the two ways to act on it. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Header(e: KbEntry, isProject: Boolean, onAsk: () -> Unit, onAgent: () -> Unit) {
    val c = JTheme.colors
    Column(
        Modifier.fillMaxWidth().padding(start = JSpace.page + JSpace.xs, end = JSpace.page + JSpace.xs, top = JSpace.s, bottom = JSpace.xs),
        verticalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
            KbAvatar(e, JSize.avatarXL)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                SelectionContainer {
                    Text(e.name.ifEmpty { " " }, style = JTheme.type.title, color = c.label, maxLines = 3, overflow = TextOverflow.Ellipsis)
                }
                FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.sm), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                    ImportanceBadge(e.importance)
                    e.projectKind?.let { ProjectKindBadge(it, "项目") }
                    e.projectStatusLabel?.let { Chip(it) }
                    if (e.relevance > 0) RelevanceBadge(e.relevance)
                }
            }
        }
        e.oneLine?.takeIf { it.isNotEmpty() }?.let { Text(it, style = JTheme.type.body, color = c.label) }
        e.tags?.takeIf { it.isNotEmpty() }?.let { tags ->
            FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.sm), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                tags.forEach { Chip("#$it") }
            }
        }
        Row(Modifier.padding(top = JSpace.xxs), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
            PillButton("问 Jarvis", onAsk, Modifier.weight(1f), kind = ButtonKind.Prominent, icon = JIcons.ai, enabled = e.name.isNotEmpty(), large = true)
            if (isProject) PillButton("开 Agent", onAgent, Modifier.weight(1f), kind = ButtonKind.Tinted, icon = JIcons.compose, enabled = e.name.isNotEmpty(), large = true)
        }
    }
}

/** Newest first, a dot and a rail down the left, when and from where above each line; 显示全部 past the first few. */
private fun LazyListScope.timeline(log: List<String>) {
    if (log.isEmpty()) return
    val lines = log.asReversed().map(KbLogLine::parse)
    item(key = "log:h") { SectionHeader("时间线", count = lines.size) }
    item(key = "log") {
        var all by rememberSaveable { mutableStateOf(false) }
        val shown = if (all) lines else lines.take(LOG_SHOWN)
        GroupCard {
            Column(Modifier.padding(horizontal = JSpace.l, vertical = JSpace.xs)) {
                shown.forEachIndexed { i, l ->
                    TimelineRow(l, first = i == 0, last = i == shown.lastIndex && (all || lines.size <= LOG_SHOWN))
                }
            }
            if (lines.size > LOG_SHOWN) {
                RowDivider()
                MoreRow(if (all) "收起" else "显示全部 ${lines.size} 条") { all = !all }
            }
        }
    }
}

/** One timeline line (iOS `KBTimelineRow`): a dot on the rail, when and from where in small type, then what happened. */
@Composable
private fun TimelineRow(line: KbLogLine, first: Boolean, last: Boolean) {
    val c = JTheme.colors
    val rail = c.fillChip
    Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
        // The rail joins the dots: none above the newest, none below the oldest shown.
        Box(Modifier.width(JSize.markXS).fillMaxHeight(), contentAlignment = Alignment.TopCenter) {
            Column(Modifier.fillMaxHeight(), horizontalAlignment = Alignment.CenterHorizontally) {
                Box(Modifier.width(JSize.rule).height(JSize.markS).background(if (first) Color.Transparent else rail))
                Box(Modifier.width(JSize.rule).weight(1f).background(if (last) Color.Transparent else rail))
            }
            Box(Modifier.padding(top = JSpace.m).size(JSize.dotL).clip(CircleShape).background(if (first) c.accent else c.tertiaryText))
        }
        Column(Modifier.weight(1f).padding(vertical = JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            if (line.whenText.isNotEmpty() || line.source.isNotEmpty()) {
                Row(horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    if (line.whenText.isNotEmpty()) Text(line.shortWhen(), style = JTheme.type.labelStrong, color = if (first) c.accent else c.secondaryText, maxLines = 1)
                    if (line.source.isNotEmpty()) Text(line.source, style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
            Text(line.text, style = JTheme.type.secondary, color = c.label)
        }
    }
}

/** 显示全部 / 收起 at the foot of a card, in the accent. */
@Composable
private fun MoreRow(text: String, onClick: () -> Unit) {
    Text(
        text, style = JTheme.type.secondaryMedium, color = JTheme.colors.accent, maxLines = 1,
        modifier = Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = JSpace.l, vertical = JSpace.m),
    )
}

/** Related entries by kind, a few each with 全部 for the rest; then the code directories. */
private fun LazyListScope.links(d: KbDetail, onOpen: (KbEntry) -> Unit) {
    val byKind = d.links.orEmpty().groupBy { it.kind }
    for (k in KbKind.entries) {
        val group = byKind[k.wire].orEmpty()
        if (group.isEmpty()) continue
        item(key = "links:h:" + k.wire) { SectionHeader("关联的" + k.title, count = group.size) }
        item(key = "links:" + k.wire) {
            var open by rememberSaveable { mutableStateOf(false) }
            GroupCard {
                (if (open) group else group.take(LINKS_SHOWN)).forEachIndexed { i, l ->
                    if (i > 0) RowDivider(LinkTextColumn)
                    LinkRow(l) { onOpen(l) }
                }
                if (group.size > LINKS_SHOWN) {
                    RowDivider()
                    MoreRow(if (open) "收起" else "全部 ${group.size} 个") { open = !open }
                }
            }
        }
    }
    if (d.dirs.isNotEmpty()) {
        item(key = "dirs:h") { SectionHeader("代码目录", count = d.dirs.size) }
        item(key = "dirs") {
            GroupCard {
                d.dirs.forEachIndexed { i, dir ->
                    if (i > 0) RowDivider(DirTextColumn)
                    Row(
                        Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget).padding(horizontal = JSpace.l, vertical = JSpace.s),
                        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
                    ) {
                        Icon(JIcons.folder, null, Modifier.size(JSize.iconXS), tint = JTheme.colors.secondaryText)
                        Text(dirText(dir), style = JTheme.type.code, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
        }
    }
}

/** `<machine>:<path>` with the path's last folders, which tell projects apart (iOS truncates the head). */
private fun dirText(dir: String): String {
    val colon = dir.indexOf(':')
    return if (colon <= 0) tailOf(dir) else dir.substring(0, colon + 1) + tailOf(dir.substring(colon + 1))
}

/** A related entry (iOS `KBLinkRow`): its tile, name and one line. */
@Composable
private fun LinkRow(entry: KbEntry, onClick: () -> Unit) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).heightIn(min = JSize.box50).padding(start = JSpace.l, end = JSpace.m, top = JSpace.s, bottom = JSpace.s),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        KbAvatar(entry, JSize.wellS)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(entry.name, style = JTheme.type.secondaryMedium, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            entry.oneLine?.takeIf { it.isNotEmpty() }?.let {
                Text(it, style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = c.tertiaryText)
    }
}

/** 最近 7 天的消息, folded: its row opens the archive's text inside the card. */
@Composable
private fun Recent(recent: String) {
    var open by rememberSaveable { mutableStateOf(false) }
    val c = JTheme.colors
    GroupCard(Modifier.padding(top = JSpace.section)) {
        Row(
            Modifier.fillMaxWidth().clickable { open = !open }.heightIn(min = JSize.box50).padding(horizontal = JSpace.l, vertical = JSpace.s)
                .semantics { contentDescription = "最近 7 天的消息，" + if (open) "收起" else "展开" },
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
        ) {
            Icon(JIcons.conversation, null, Modifier.size(JSize.iconXS), tint = c.accent)
            Text("最近 7 天的消息", style = JTheme.type.body, color = c.label, maxLines = 1, modifier = Modifier.weight(1f))
            Icon(if (open) JIcons.expand else JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = c.tertiaryText)
        }
        if (open) {
            RowDivider()
            SelectionContainer {
                Text(recent, style = JTheme.type.secondary, color = c.label, modifier = Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m))
            }
        }
    }
}

private val projectKinds = listOf("short" to "短期（需求）", "long" to "长期（使命）")
private val projectStatuses = listOf("active" to "进行中", "paused" to "暂停", "done" to "已完成", "ignored" to "不是项目")

/** Importance, relevance, tags (and a project's kind / status) set by hand; the hub keeps them over the automatic ones. */
@Composable
private fun KbTagEditor(hub: Hub, entry: KbEntry, onDismiss: () -> Unit, onSaved: () -> Unit) {
    JSheet(onDismiss, title = "打标 · " + entry.name, onClose = onDismiss) { KbTagForm(hub, entry, onSaved) }
}

/** The 打标 sheet's body (drawn alone by the screenshots, which can't show a sheet's own window). */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun KbTagForm(hub: Hub, entry: KbEntry, onSaved: () -> Unit) {
    val scope = rememberCoroutineScope()
    val isProject = entry.kind == KbKind.PROJECTS.wire
    var importance by remember { mutableStateOf(entry.importance?.takeIf { it.isNotEmpty() } ?: "P2") }
    var relevance by remember { mutableFloatStateOf(entry.relevance.coerceIn(0, 100).toFloat()) }
    var tags by remember { mutableStateOf(entry.tags.orEmpty().joinToString(" ")) }
    var projectKind by remember { mutableStateOf(entry.projectKindStatus.first) }
    var status by remember { mutableStateOf(entry.projectKindStatus.second) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val c = JTheme.colors

    fun save() {
        saving = true; error = null
        scope.launch {
            val u = KbUpdate(importance, relevance.roundToInt(), KbContext.parseTags(tags),
                status = if (isProject) status else null, projectKind = if (isProject) projectKind else null)
            runCatching { hub.api.kbUpdate(entry.kind, entry.id, u) }.onSuccess { onSaved() }.onFailure { error = "没有保存：${it.message}" }
            saving = false
        }
    }

    Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).imePadding()) {
        SectionHeader("重要级")
        FlowRow(Modifier.padding(horizontal = JSpace.page), horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
            listOf("P0", "P1", "P2", "P3").forEach { p ->
                val color = importanceColor(p)
                ChoiceChip(p, importance == p, { importance = p }, lead = { Box(Modifier.size(JSize.dotS).clip(CircleShape).background(color)) })
            }
        }
        SectionHeader("与我相关 ${relevance.roundToInt()}")
        JSlider(relevance, { relevance = (it / 5f).roundToInt() * 5f }, Modifier.padding(horizontal = JSpace.page), range = 0f..100f)
        SectionHeader("标签")
        JTextField(tags, { tags = it }, "用空格分隔", Modifier.padding(horizontal = JSpace.page), singleLine = true, fill = c.card)
        if (isProject) {
            SectionHeader("类型")
            Choices(projectKinds, projectKind) { projectKind = it }
            SectionHeader("状态")
            Choices(projectStatuses, status) { status = it }
        }
        GroupFooter("手动打的标会锁住，hub 自动整理时不再覆盖。", Modifier.padding(top = JSpace.s))
        error?.let { ErrorLine(it, Modifier.padding(start = JSpace.page + JSpace.xs, end = JSpace.page, top = JSpace.m)) }
        PillButton(
            if (saving) "正在保存…" else "保存", ::save, Modifier.fillMaxWidth().padding(start = JSpace.page, end = JSpace.page, top = JSpace.xl),
            kind = ButtonKind.Prominent, large = true, enabled = !saving,
        )
    }
}

/** One of a few choices (a project's kind, its status), all in sight; labels stay on one line. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Choices(options: List<Pair<String, String>>, value: String, onPick: (String) -> Unit) {
    FlowRow(Modifier.padding(horizontal = JSpace.page), horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
        options.forEach { (k, label) -> ChoiceChip(label, k == value, { onPick(k) }) }
    }
}
