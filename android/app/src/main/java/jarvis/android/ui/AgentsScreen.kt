package jarvis.android.ui

import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.SwipeToDismissBox
import android.content.Context
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import jarvis.android.data.AgentState
import jarvis.android.data.ConnState
import jarvis.android.data.Grouper
import jarvis.android.data.Grouping
import jarvis.android.data.Hub
import jarvis.android.data.Interaction
import jarvis.android.data.QuickFilter
import jarvis.android.data.Rank
import jarvis.android.data.Resolution
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.data.SpaceGroup
import jarvis.android.data.isWaiting
import jarvis.android.data.matches
import jarvis.android.data.parseTime
import jarvis.android.data.underFilter
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.ChoiceChip
import jarvis.android.ui.design.DOT_SEP
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.Haptics
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SearchField
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.design.StatusDot
import jarvis.android.ui.design.hapticView
import jarvis.android.ui.design.surface
import jarvis.android.ui.design.cardSlice
import kotlinx.coroutines.launch
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * The list's options, kept on this phone (iOS @AppStorage `dev.grouping`, `dev.machineFilter`, `dev.folded`): the
 * grouping, one machine or all, the folded sections. The shell's menu (tapping Agents again) changes them too.
 */
@Stable
class AgentsListOptions(context: Context?) {
    private val p = context?.applicationContext?.getSharedPreferences("agents_list", Context.MODE_PRIVATE)
    var grouping by mutableStateOf(runCatching { Grouping.valueOf(p?.getString("grouping", null) ?: "Space") }.getOrDefault(Grouping.Space))
        private set
    /** One machine only, or null for all. */
    var machineFilter by mutableStateOf(p?.getString("machine", null))
        private set
    var folded by mutableStateOf(p?.getStringSet("folded", emptySet()).orEmpty().toSet())
        private set
    /** The sections the list shows now (set by the list each time it draws), for 全部折叠. */
    internal var sections: List<String> = emptyList()

    fun pick(g: Grouping) { grouping = g; p?.edit()?.putString("grouping", g.name)?.apply() }
    fun pickMachine(id: String?) { machineFilter = id; p?.edit()?.putString("machine", id)?.apply() }
    fun toggle(id: String) = saveFolds(if (id in folded) folded - id else folded + id)
    /** Sections folded by default (已看, 空闲的空间) are folded by leaving them out. */
    fun foldAll() = saveFolds(folded - DEFAULT_FOLDED.toSet() + sections.filter { it !in DEFAULT_FOLDED })
    fun unfoldAll() = saveFolds(DEFAULT_FOLDED.toSet())
    private fun saveFolds(v: Set<String>) { folded = v; p?.edit()?.putStringSet("folded", v)?.apply() }

    companion object {
        /** Folded unless opened: their id in [folded] means open. */
        val DEFAULT_FOLDED = listOf("state/Seen", "quiet-spaces")
    }
}

/**
 * Every agent on every computer (iOS `ChatsView`), under the shell's top bar: the search field and quick filters stay
 * on top; below, what needs you, pinned agents (each shown once), then the groups — 按项目 (default), 按状态 or 按机器 —
 * as cards. Ordering lives in data/Grouping.kt (tested against iOS's rules).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AgentsScreen(
    hub: Hub, onOpen: (SessionKey) -> Unit, onOpenSpace: (String) -> Unit,
    options: AgentsListOptions, inShell: Boolean = true, onOpenMonitor: () -> Unit = {},
    /** The agent whose chat is open beside the list (a wide window): its row is marked. */
    selected: SessionKey? = null,
) {
    val conn by hub.conn.collectAsState()
    val machines by hub.machines.collectAsState()
    val byMachine by hub.sessions.collectAsState()
    val allPending by hub.interactions.collectAsState()
    val pins by hub.pins.collectAsState()
    val mutes by hub.mutes.collectAsState()
    var refreshing by remember { mutableStateOf(false) }
    var menuFor by remember { mutableStateOf<Session?>(null) }
    var closingFor by remember { mutableStateOf<Session?>(null) }
    var renamingSpace by remember { mutableStateOf<SpaceGroup?>(null) }
    val scope = rememberCoroutineScope()
    var query by rememberSaveable { mutableStateOf("") }
    var quick by rememberSaveable { mutableStateOf(QuickFilter.All) }
    // What the quick filter showed stays until it's chosen again (iOS `kept`).
    var kept by remember { mutableStateOf(emptySet<SessionKey>()) }
    var confirmReadAll by remember { mutableStateOf<List<Session>?>(null) }
    val machineFilter = options.machineFilter
    LaunchedEffect(machineFilter) { kept = emptySet() }

    val names = machines.associate { it.id to it.displayName }
    // The assistant's questions are answered on 助手 (iOS filters them the same way).
    val assistantIds = byMachine.values.flatten().filter { it.assistant }.map { it.key }.toSet()
    val pending = allPending.filter { it.sessionKey !in assistantIds && (machineFilter == null || it.machineId == machineFilter) }
    val g = Grouper(pins, mutes)
    val base = byMachine.values.flatten().filter { it.matches(query, machineFilter) }
    val matching = base.filter { quick != QuickFilter.All && quick.matches(it) }.map { it.key }.toSet()
    LaunchedEffect(matching) { if (!kept.containsAll(matching)) kept = kept + matching }
    val all = base.underFilter(quick, kept)
    val searching = query.isNotBlank()
    val expandAll = searching || quick != QuickFilter.All
    // Pinned agents show once, at the top, in pin order; every grouping is built from the rest (as on iOS).
    val pinnedAgents = all.filter { g.pinned(it) }.sortedBy { pins.sessions.indexOf("${it.machineId}/${it.sessionId}") }
    val rest = all.filterNot { g.pinned(it) }
    val (live, offline) = rest.partition { it.isLive }
    val multiMachine = machines.size > 1 && machineFilter == null
    val sections = mutableListOf<String>()
    val onRow: (Session) -> Unit = { onOpen(it.key) }
    val onLong: (Session) -> Unit = { menuFor = it }

    val top = if (inShell) WindowInsets.statusBars.asPaddingValues().calculateTopPadding() + JSize.shellBar else JSpace.s
    androidx.compose.runtime.CompositionLocalProvider(LocalSelectedAgent provides selected, LocalCloseAgent provides { closingFor = it }) {
    Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
        // The search field and the quick filters stay on top (iOS keeps them out of the scroll).
        Column(Modifier.fillMaxWidth().padding(top = top, bottom = JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
            SearchField(query, { query = it }, "搜索空间、标签、会话、内容", Modifier.padding(horizontal = JSpace.page))
            val counts = base.groupingBy { it.agentState }.eachCount()
            Row(
                Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = JSpace.page),
                horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalAlignment = Alignment.CenterVertically,
            ) {
                QuickFilter.entries.forEach { f ->
                    val n = f.state?.let { counts[it] ?: 0 }
                    ChoiceChip(
                        f.title, quick == f,
                        onClick = { quick = f; kept = base.filter { f != QuickFilter.All && f.matches(it) }.map { it.key }.toSet() },
                        lead = f.state?.let { st -> { Box(Modifier.size(JSize.dotS).clip(CircleShape).background(JTheme.tone(st).dot)) } },
                        count = n,
                    )
                }
            }
        }
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; hub.refresh(); refreshing = false } },
            modifier = Modifier.weight(1f).fillMaxWidth(),
        ) {
            LazyColumn(
                Modifier.fillMaxSize(),
                contentPadding = PaddingValues(bottom = JSpace.xxl + WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()),
            ) {
                val unread = all.filter { it.agentState == AgentState.Unread }
                if (quick == QuickFilter.Unread && unread.size > 1) item(key = "read-all") {
                    Row(Modifier.fillMaxWidth().padding(horizontal = JSpace.page), horizontalArrangement = Arrangement.End) {
                        TextButton({ confirmReadAll = unread }) { Text("全部标为已读", style = JTheme.type.secondaryStrong, maxLines = 1) }
                    }
                }
                if (pending.isNotEmpty() && (quick == QuickFilter.All || quick == QuickFilter.NeedsYou) && !searching) {
                    item(key = "h-pending") { SectionHeader("需要你处理", count = pending.size, countColor = JTheme.needsYou.text, modifier = Modifier.padding(top = 0.dp)) }
                    item(key = "pending") {
                        GroupCard {
                            pending.forEachIndexed { i, ix ->
                                if (i > 0) RowDivider(JSpace.l + JSize.iconS + JSpace.m)
                                PendingRow(hub, ix, names[ix.machineId]) { onOpen(ix.sessionKey) }
                            }
                        }
                    }
                }
                if (pinnedAgents.isNotEmpty()) {
                    item(key = "h-pinned") { SectionHeader("置顶") }
                    card("pinned", pinnedAgents) { s -> AgentRow(s, names[s.machineId], g.muted(s), showWhere = true, onRow, onLong) }
                }
                when (options.grouping) {
                    Grouping.Space -> spaceSections(
                        g, live, if (expandAll) emptySet() else options.folded, names, multiMachine, options::toggle,
                        expandAll = expandAll, sections = sections,
                        onSpaceAction = { space, act ->
                            when (act) {
                                SpaceAction.Open -> onOpenSpace(space.id)
                                SpaceAction.Rename -> renamingSpace = space
                                SpaceAction.Pin -> scope.launch { runCatching { hub.toggleSpacePin(space) }.onFailure { Toasts.show("置顶没成功：${it.message}", ToastStyle.ERROR) } }
                                SpaceAction.Mute -> scope.launch { runCatching { hub.toggleSpaceMute(space) }.onFailure { Toasts.show("屏蔽没成功：${it.message}", ToastStyle.ERROR) } }
                            }
                        },
                        row = { s -> AgentRow(s, names[s.machineId], g.muted(s), showWhere = false, onRow, onLong) },
                    )
                    Grouping.State -> g.byState(live).forEach { (rank, list) ->
                        val id = "state/" + rank.name
                        sections += id
                        // 已看 is the long tail: folded unless the user opened it.
                        val isFolded = !expandAll && (if (rank == Rank.Seen) id !in options.folded else id in options.folded)
                        item(key = "h-$id") {
                            SectionHeader(rank.title, count = list.size, folded = isFolded, onClick = { options.toggle(id) },
                                countColor = if (rank == Rank.NeedsYou) JTheme.needsYou.text else null)
                        }
                        if (!isFolded) card(id, list) { s -> AgentRow(s, names[s.machineId], g.muted(s), showWhere = true, onRow, onLong, showMachine = multiMachine) }
                    }
                    Grouping.Machine -> g.byMachine(live, machines).forEach { (m, list) ->
                        val id = "machine/" + m.id
                        sections += id
                        val isFolded = !expandAll && id in options.folded
                        item(key = "h-$id") {
                            SectionHeader(m.displayName, folded = isFolded, onClick = { options.toggle(id) }) {
                                StateCounts(list)
                                Spacer(Modifier.width(JSpace.s))
                                Text(if (m.online) "在线" else "离线", style = JTheme.type.caption, color = if (m.online) JTheme.colors.positive else JTheme.colors.secondaryText, maxLines = 1)
                            }
                        }
                        if (!isFolded) card(id, list) { s -> AgentRow(s, names[s.machineId], g.muted(s), showWhere = true, onRow, onLong, showMachine = false) }
                    }
                }
                if (offline.isNotEmpty()) {
                    val id = "offline"
                    sections += id
                    val isFolded = !expandAll && id in options.folded
                    item(key = "h-offline") { SectionHeader("最近 7 天", count = offline.size, folded = isFolded, onClick = { options.toggle(id) }) }
                    if (!isFolded) card("off", g.ordered(offline)) { s -> AgentRow(s, names[s.machineId], g.muted(s), showWhere = true, onRow, onLong, showMachine = multiMachine) }
                }
                if (all.isEmpty() && (pending.isEmpty() || searching || quick != QuickFilter.All)) {
                    item(key = "empty") {
                        when {
                            conn != ConnState.Online -> EmptyState(JIcons.offline, "正在连接 hub", "连上以后，各台电脑上的 Agent 会出现在这里。")
                            searching -> EmptyState(JIcons.search, "没有找到", "没有和「${query.trim()}」匹配的 Agent。")
                            quick != QuickFilter.All -> EmptyState(JIcons.done, "都处理完了", "现在没有「${quick.title}」的 Agent。")
                            machineFilter != null -> EmptyState(JIcons.machine, "这台电脑上没有 Agent", "换一台电脑看看，或者在这台上新开一个。")
                            else -> EmptyState(JIcons.agents, "还没有 Agent", "点右上角新开一个，或在电脑上的 herdr 里启动 claude。")
                        }
                    }
                }
            }
        }
    }
    }
    SideEffect { options.sections = sections.toList() }

    closingFor?.let { s ->
        CloseAgentDialog(hub, s, names[s.machineId], onDismiss = { closingFor = null }, onError = { Toasts.show(it, ToastStyle.ERROR) })
    }
    menuFor?.let { s ->
        AgentActionsSheet(
            hub, s, pinned = g.pinned(s), muted = g.muted(s), machine = names[s.machineId],
            onDismiss = { menuFor = null }, onError = { Toasts.show(it, ToastStyle.ERROR) },
        )
    }
    renamingSpace?.let { space ->
        RenameDialog(
            title = "重命名空间", current = space.name,
            note = "改的是 ${names[space.machineId] ?: "电脑"} 上 herdr 的 workspace 名，置顶和屏蔽会跟着新名字走",
            onDismiss = { renamingSpace = null },
            onSave = { name ->
                renamingSpace = null
                jarvis.android.JarvisApp.instance.scope.launch {
                    runCatching { hub.renameSpace(space, name) }.onFailure { Toasts.show("重命名没成功：${it.message}", ToastStyle.ERROR) }
                }
            },
        )
    }
    confirmReadAll?.let { list ->
        AlertDialog(
            onDismissRequest = { confirmReadAll = null },
            title = { Text("全部标为已读？") },
            text = { Text("${list.size} 个完成未看的 Agent 会标为已看，不再算「完成未看」") },
            confirmButton = {
                TextButton({ confirmReadAll = null; jarvis.android.JarvisApp.instance.scope.launch { hub.markSeen(list) } }) { Text("标为已读", maxLines = 1) }
            },
            dismissButton = { TextButton({ confirmReadAll = null }) { Text("取消", maxLines = 1) } },
        )
    }
}

enum class SpaceAction { Open, Pin, Mute, Rename }

/** The agent open beside the list on a wide window (its row is marked); null on a phone. */
private val LocalSelectedAgent = androidx.compose.runtime.staticCompositionLocalOf<SessionKey?> { null }

/** The list's 关闭 Agent (asks first): a running agent's row swipes left to it, as on iOS. */
private val LocalCloseAgent = androidx.compose.runtime.staticCompositionLocalOf<((Session) -> Unit)?> { null }

/**
 * A group of agents as one card: each row is its own lazy item drawing its part of the card (rounded on top for
 * the first, at the bottom for the last), so a long group stays a lazy list.
 */
private fun LazyListScope.card(id: String, list: List<Session>, row: @Composable (Session) -> Unit) {
    itemsIndexed(list, key = { _, s -> "$id/" + s.key.path }) { i, s ->
        CardSlice(first = i == 0, last = i == list.lastIndex) { row(s) }
    }
}

/** One row's slice of a group card: its corners where the card starts and ends, the hairline above it inside. */
@Composable
internal fun CardSlice(first: Boolean, last: Boolean, divider: Boolean = !first, dividerStart: Dp = JSpace.l + JSize.dotL + JSpace.m, content: @Composable () -> Unit) {
    Column(Modifier.padding(horizontal = JSpace.page).cardSlice(first, last)) {
        if (divider) RowDivider(dividerStart)
        content()
    }
}

/**
 * 按项目: pinned spaces, the other busy ones, muted ones with work running, then 空闲的空间 folded. Inside a space,
 * busy and pinned agents first; the idle ones fold into one row so a big workspace stays short.
 */
private fun LazyListScope.spaceSections(
    g: Grouper, live: List<Session>, folded: Set<String>, names: Map<String, String>, multiMachine: Boolean,
    toggleFold: (String) -> Unit, expandAll: Boolean, sections: MutableList<String>,
    onSpaceAction: (SpaceGroup, SpaceAction) -> Unit, row: @Composable (Session) -> Unit,
) {
    val layout = g.spaceLayout(live)
    fun space(sg: SpaceGroup) {
        sections += sg.id
        val isFolded = sg.id in folded
        val ordered = g.ordered(sg.sessions)
        item(key = "space/" + sg.id) {
            SpaceHeader(sg, if (multiMachine) names[sg.machineId] else null, g.pinned(sg), g.muted(sg), isFolded, ordered,
                onFold = { toggleFold(sg.id) }, onAction = { onSpaceAction(sg, it) })
        }
        if (isFolded) return
        val busy = if (expandAll) ordered else ordered.filter { g.stays(it) }
        val idle = if (expandAll) emptyList() else ordered.filterNot { g.stays(it) }
        val idleId = "idle/" + sg.id
        val idleOpen = idleId in folded
        val shown = busy + if (idleOpen) idle else emptyList()
        val foot = idle.isNotEmpty()
        itemsIndexed(shown, key = { _, s -> "s/" + s.key.path }) { i, s ->
            CardSlice(first = i == 0, last = i == shown.lastIndex && !foot) { row(s) }
        }
        if (foot) item(key = "$idleId/foot") {
            CardSlice(first = shown.isEmpty(), last = true) { IdleFoldRow(idle, open = idleOpen) { toggleFold(idleId) } }
        }
    }
    layout.active.forEach(::space)
    if (layout.quiet.isNotEmpty()) {
        // 空闲的空间 is folded by default ("open" is what gets remembered), like 已看 in 按状态; searching opens it.
        val id = "quiet-spaces"
        sections += id
        val isFolded = !expandAll && id !in folded
        item(key = id) { SectionHeader("空闲的空间", count = layout.quiet.size, folded = isFolded, onClick = { toggleFold(id) }) }
        if (!isFolded) layout.quiet.forEach(::space)
    }
}

/** 「还有 N 个空闲」 with their tab names in small type; opened, 「收起空闲的 N 个」. */
@Composable
private fun IdleFoldRow(idle: List<Session>, open: Boolean, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.tapTarget).clickable(onClick = onClick).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Box(Modifier.size(JSize.dotL), contentAlignment = Alignment.Center) {
            Icon(if (open) JIcons.collapse else JIcons.expand, null, Modifier.size(JSize.markM), tint = JTheme.colors.tertiaryText)
        }
        Text(if (open) "收起空闲的 ${idle.size} 个" else "还有 ${idle.size} 个空闲", style = JTheme.type.secondary, color = JTheme.colors.secondaryText, maxLines = 1)
        if (!open) Text(idle.joinToString("、") { it.displayTitle }, style = JTheme.type.caption, color = JTheme.colors.tertiaryText,
            maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
    }
}

/**
 * A space's header (iOS's): the name quiet and small, the pin, its machine; the states' counts as colored dots; a
 * fold chevron; ⋯ (打开空间, 置顶, 屏蔽, 重命名). Tapping the name folds it.
 */
@Composable
private fun SpaceHeader(
    sg: SpaceGroup, machine: String?, pinned: Boolean, muted: Boolean, folded: Boolean, sessions: List<Session>,
    onFold: () -> Unit, onAction: (SpaceAction) -> Unit,
) {
    var menu by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onFold).alpha(if (muted) 0.8f else 1f)
            .padding(start = JSpace.page + JSpace.xs, end = JSpace.s, top = JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
    ) {
        // The name first: a narrow list (a foldable's left pane) gives what is left to the pin and the machine.
        Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            Text(sg.name, style = JTheme.type.sectionHeader, color = JTheme.colors.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (pinned) Icon(JIcons.pinned, "已置顶", Modifier.size(JSize.markS), tint = JTheme.colors.accent)
            if (muted) Icon(JIcons.muted, "已屏蔽", Modifier.size(JSize.markS), tint = JTheme.colors.tertiaryText)
            machine?.let { Text(it, style = JTheme.type.caption, color = JTheme.colors.tertiaryText, maxLines = 1, overflow = TextOverflow.Ellipsis) }
        }
        StateCounts(sessions)
        Icon(if (folded) JIcons.disclosure else JIcons.expand, null, Modifier.size(JSize.markM), tint = JTheme.colors.tertiaryText)
        // A visible ⋯: a long press on a header is easy to miss (iOS keeps a ⋯ there for the same reason).
        Box {
            Box(Modifier.size(JSize.tapTarget).clip(CircleShape).clickable { menu = true }, contentAlignment = Alignment.Center) {
                Box(Modifier.size(JSize.iconS).border(JSize.lineMedium, JTheme.colors.accent, CircleShape), contentAlignment = Alignment.Center) {
                    Icon(JIcons.more, "空间操作", Modifier.size(JSize.markM), tint = JTheme.colors.accent)
                }
            }
            DropdownMenu(menu, onDismissRequest = { menu = false }) {
                DropdownMenuItem({ Text("打开空间", maxLines = 1) }, { menu = false; onAction(SpaceAction.Open) }, leadingIcon = { Icon(JIcons.space, null) })
                DropdownMenuItem({ Text(if (pinned) "取消置顶空间" else "置顶空间", maxLines = 1) }, { menu = false; onAction(SpaceAction.Pin) }, leadingIcon = { Icon(JIcons.pin, null) })
                DropdownMenuItem({ Text(if (muted) "取消屏蔽空间" else "屏蔽这个空间", maxLines = 1) }, { menu = false; onAction(SpaceAction.Mute) }, leadingIcon = { Icon(if (muted) JIcons.unmute else JIcons.mute, null) })
                if (sg.workspaceId != null) DropdownMenuItem({ Text("重命名空间…", maxLines = 1) }, { menu = false; onAction(SpaceAction.Rename) }, leadingIcon = { Icon(JIcons.edit, null) })
            }
        }
    }
}

/** The states worth counting on a header: colored dots and quiet digits (seen ones as a gray dot). */
@Composable
internal fun StateCounts(list: List<Session>) {
    val by = list.groupingBy { it.agentState }.eachCount()
    Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalAlignment = Alignment.CenterVertically) {
        listOf(AgentState.NeedsYou, AgentState.Working, AgentState.Unread, AgentState.Idle).forEach { st ->
            val n = by[st] ?: 0
            if (n > 0) Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                Box(Modifier.size(JSize.dotXS).clip(CircleShape).background(JTheme.tone(st).dot))
                Text("$n", style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1)
            }
        }
    }
}

/**
 * Something an agent waits on, as a row of the 需要你处理 card (iOS's card mode): the red kind symbol, what it asks
 * (计划等你审批 / Claude 在问你…), whose and where, the gist; a single-choice question's options are buttons that
 * answer right here. A tap elsewhere opens the chat to answer there.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun PendingRow(hub: Hub, ix: Interaction, machine: String?, onClick: () -> Unit) {
    val scope = rememberCoroutineScope()
    val view = hapticView()
    var busy by remember(ix.id) { mutableStateOf(false) }
    val session = hub.session(ix.sessionKey)
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(start = JSpace.l, end = JSpace.m, top = JSpace.m, bottom = JSpace.m),
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Icon(interactionIcon(ix), null, Modifier.size(JSize.iconS), tint = JTheme.needsYou.dot)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
                    Text(ix.kindLabel(session?.agentLabel ?: "Claude"), style = JTheme.type.heading, color = JTheme.colors.label, maxLines = 1)
                    Text(
                        listOfNotNull(ix.sessionTitle?.takeIf { it.isNotBlank() } ?: session?.displayTitle ?: ix.cwd?.substringAfterLast('/'), machine).joinToString(DOT_SEP),
                        style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    )
                }
                Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = JTheme.colors.tertiaryText)
            }
            Text(pendingGist(ix), style = JTheme.type.body, color = JTheme.colors.label, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = JSpace.xs))
            val q = ix.questions?.singleOrNull()
            if (ix.kind == "question" && q != null && !q.multiSelect && q.options.isNotEmpty()) {
                FlowRow(Modifier.padding(top = JSpace.xs), horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    q.options.take(4).forEach { opt ->
                        Row(
                            Modifier.heightIn(min = JSize.wellL).surface(CircleShape, floating = true)
                                .clickable(enabled = !busy) {
                                    busy = true
                                    scope.launch {
                                        runCatching { hub.resolve(ix, Resolution(answers = mapOf(q.question to opt.label))) }
                                            .onSuccess { Haptics.success(view) }
                                            .onFailure { Haptics.error(view); Toasts.show("没答上：${it.message}", ToastStyle.ERROR) }
                                        busy = false
                                    }
                                }
                                .padding(horizontal = JSpace.m),
                            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
                        ) {
                            Text(opt.displayLabel, style = JTheme.type.secondaryMedium, color = JTheme.colors.accent, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            if (opt.recommended) Chip("推荐", look = ChipLook.Tint(JTheme.colors.ai))
                        }
                    }
                }
            }
        }
    }
}

/** What a pending row says under its title: the question, the plan's title, the tool and what it wants. */
private fun pendingGist(ix: Interaction): String = when (ix.kind) {
    "plan" -> planTitle(ix.plan.orEmpty())
    else -> ix.headline
}

/**
 * One agent (iOS `SlotRow`): the state dot, the title (heavier when it is yours to look at) and its time; the last
 * thing it said; a meta line — 需要你 in red (the only state written out), the agent kind, where it runs, a loop's
 * next wake, background work. Muted agents read quieter (gray dot, 🔕) but stay where their state puts them.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun AgentRow(
    s: Session, machine: String?, muted: Boolean, showWhere: Boolean, onClick: (Session) -> Unit, onLongClick: (Session) -> Unit,
    showMachine: Boolean = showWhere,
) {
    val close = LocalCloseAgent.current
    if (close != null && s.isLive && s.canTerminal) {
        SwipeToClose(s.key.path, onClose = { close(s) }) { AgentRowBody(s, machine, muted, showWhere, onClick, onLongClick, showMachine) }
    } else AgentRowBody(s, machine, muted, showWhere, onClick, onLongClick, showMachine)
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun AgentRowBody(
    s: Session, machine: String?, muted: Boolean, showWhere: Boolean, onClick: (Session) -> Unit, onLongClick: (Session) -> Unit,
    showMachine: Boolean,
) {
    val c = JTheme.colors
    val strong = s.agentState == AgentState.NeedsYou || s.agentState == AgentState.Unread
    val selected = LocalSelectedAgent.current == s.key
    Row(
        Modifier.fillMaxWidth().then(if (selected) Modifier.background(c.fillChip) else Modifier)
            .combinedClickable(onClick = { onClick(s) }, onLongClick = { onLongClick(s) })
            .alpha(if (muted) 0.8f else 1f).padding(start = JSpace.l, end = JSpace.m, top = JSpace.m, bottom = JSpace.m),
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Box(Modifier.height(JSize.iconS), contentAlignment = Alignment.Center) { StatusDot(s.agentState, muted = muted) }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                    Text(s.displayTitle, style = if (strong) JTheme.type.heading else JTheme.type.body, color = c.label,
                        maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                    if (muted) Icon(JIcons.muted, "已屏蔽", Modifier.size(JSize.markS), tint = c.tertiaryText)
                }
                Text(listTime(s.updated), style = JTheme.type.caption, color = c.tertiaryText, maxLines = 1)
            }
            s.lastText?.takeIf { it.isNotBlank() }?.let {
                Text(it.replace('\n', ' '), style = JTheme.type.secondary, color = c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            AgentMeta(s, machine, showWhere, showMachine)
        }
        Box(Modifier.height(JSize.iconS), contentAlignment = Alignment.Center) {
            Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = c.tertiaryText)
        }
    }
}

/** Swipe left reveals a red 关闭 (iOS swipeActions); let go past the threshold, [onClose] asks and the row springs back. */
@OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
@Composable
private fun SwipeToClose(id: String, onClose: () -> Unit, content: @Composable () -> Unit) {
    val state = androidx.compose.runtime.key(id) {
        androidx.compose.material3.rememberSwipeToDismissBoxState(confirmValueChange = { v ->
            if (v == androidx.compose.material3.SwipeToDismissBoxValue.EndToStart) onClose()
            false
        })
    }
    androidx.compose.material3.SwipeToDismissBox(
        state, enableDismissFromStartToEnd = false,
        backgroundContent = {
            Row(
                Modifier.fillMaxSize().background(JTheme.colors.danger).padding(end = JSpace.xl),
                horizontalArrangement = Arrangement.spacedBy(JSpace.sm, Alignment.End), verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(JIcons.close, null, Modifier.size(JSize.iconXS), tint = androidx.compose.ui.graphics.Color.White)
                Text("关闭", style = JTheme.type.bodyStrong, color = androidx.compose.ui.graphics.Color.White, maxLines = 1)
            }
        },
    ) {
        // Opaque, so the red shows only where the row has moved away.
        Box(Modifier.background(JTheme.colors.card)) { content() }
    }
}

/** 「需要你 · Claude · weather › tests · MacBook」 plus the loop / background marks. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun AgentMeta(s: Session, machine: String?, showWhere: Boolean, showMachine: Boolean) {
    val c = JTheme.colors
    val tab = s.slotName?.takeIf { it.isNotBlank() && it != s.slotIndex?.toString() }
    val where = listOfNotNull(
        // Where an agent runtime's session comes from (DSH…), else the agent kind.
        s.sourceSubtitle ?: s.agentLabel,
        if (showWhere) listOfNotNull(s.spaceName?.takeIf { it.isNotBlank() }, tab).joinToString(" › ").ifEmpty { null }
        else tab ?: s.folderName.ifEmpty { null },
        machine.takeIf { showMachine },
    ).joinToString(DOT_SEP)
    val needs = JTheme.needsYou.text
    val working = JTheme.tone(AgentState.Working)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
        Text(
            buildAnnotatedString {
                if (s.agentState == AgentState.NeedsYou) {
                    pushStyle(SpanStyle(color = needs)); append("需要你"); pop()
                    if (where.isNotEmpty()) append(DOT_SEP)
                }
                append(where)
            },
            style = JTheme.type.caption, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis,
        )
        s.loop?.let { loop ->
            val next = parseTime(loop.nextAt)?.let { "下次 " + DateTimeFormatter.ofPattern("HH:mm").withZone(ZoneId.systemDefault()).format(it) } ?: "循环中"
            Chip(next, icon = JIcons.recurring, look = ChipLook.Tint(c.loop))
        }
        val bg = s.background.orEmpty().size
        if (bg > 0) Chip("后台 $bg", icon = JIcons.settings, look = ChipLook.Tint(working.tint, working.text))
        val subs = s.subagents.orEmpty().count { it.running }
        if (subs > 0) Chip("子 Agent $subs", icon = JIcons.person)
        if (s.isWaiting && s.loop == null && bg == 0 && subs == 0) Chip("待唤醒", look = ChipLook.Tint(c.loop))
    }
}

/** 「刚刚」「5 分钟前」「14:20」「昨天」「周三」「10月3日」 (iOS `Fmt.relative`). */
fun listTime(t: Instant?, now: Instant = Instant.now()): String {
    t ?: return ""
    val s = Duration.between(t, now).seconds
    if (s < 45) return "刚刚"
    if (s < 3600) return "${maxOf(1, s / 60)} 分钟前"
    val z = ZoneId.systemDefault()
    val d = t.atZone(z).toLocalDate()
    val today = now.atZone(z).toLocalDate()
    return when {
        d == today -> DateTimeFormatter.ofPattern("HH:mm").withZone(z).format(t)
        d == today.minusDays(1) -> "昨天"
        s < 6 * 86400 -> listOf("周一", "周二", "周三", "周四", "周五", "周六", "周日")[d.dayOfWeek.value - 1]
        else -> "${d.monthValue}月${d.dayOfMonth}日"
    }
}

/** The older relative time (pages that say 「3 小时前」). */
fun ago(t: Instant?): String {
    t ?: return ""
    val d = Duration.between(t, Instant.now())
    return when {
        d.seconds < 60 -> "刚刚"
        d.toMinutes() < 60 -> "${d.toMinutes()} 分钟前"
        d.toHours() < 24 -> "${d.toHours()} 小时前"
        else -> "${d.toDays()} 天前"
    }
}

/** The state dot for pages that pass a size in dp as a number. */
@Composable
fun StateDot(state: AgentState, size: Int = 10) = StatusDot(state, Dp(size.toFloat()))

/** The red symbol of what an interaction asks (iOS `InteractionKind.symbol`). */
fun interactionIcon(ix: Interaction) = when (ix.kind) {
    "question" -> JIcons.question
    "plan" -> JIcons.plan
    "permission" -> JIcons.permission
    "screen" -> JIcons.terminal
    else -> JIcons.notifications
}
