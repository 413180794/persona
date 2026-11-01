package jarvis.android.ui

import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.SwipeToDismissBox
import android.content.Context
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.FilterChip
import androidx.compose.material3.OutlinedTextField
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.saveable.rememberSaveable
import jarvis.android.data.QuickFilter
import jarvis.android.data.matches
import jarvis.android.data.underFilter
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.NotificationsOff
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Snackbar
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.data.AgentState
import jarvis.android.data.ConnState
import jarvis.android.data.Grouper
import jarvis.android.data.Grouping
import jarvis.android.data.Hub
import jarvis.android.data.Interaction
import jarvis.android.data.Rank
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.data.SpaceGroup
import kotlinx.coroutines.launch
import java.time.Duration
import java.time.Instant

/**
 * Every agent on every computer, grouped like the iOS 开发 list (ChatsView): 按项目 (default), 按状态 or 按机器, the
 * choice remembered. Above the groups: what needs you, then pinned agents (each shown once). The assistant's
 * conversations are not agents. Ordering lives in data/Grouping.kt so it can be tested against iOS's rules.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AgentsScreen(
    hub: Hub, onOpen: (SessionKey) -> Unit, onOpenSpace: (String) -> Unit, onSettings: () -> Unit,
    bottomBar: @Composable () -> Unit = {},
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
    var newSession by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val prefs = remember { ListPrefs(context) }
    var grouping by remember { mutableStateOf(prefs.grouping) }
    var folded by remember { mutableStateOf(prefs.folded) }
    var query by rememberSaveable { mutableStateOf("") }
    var searching by rememberSaveable { mutableStateOf(false) }
    var quick by rememberSaveable { mutableStateOf(QuickFilter.All) }
    var machineFilter by rememberSaveable { mutableStateOf(prefs.machineFilter) }
    // What the quick filter showed stays until it's chosen again (iOS `kept`).
    var kept by remember { mutableStateOf(emptySet<SessionKey>()) }
    var confirmReadAll by remember { mutableStateOf<List<Session>?>(null) }

    fun toggleFold(id: String) {
        folded = if (id in folded) folded - id else folded + id
        prefs.folded = folded
    }

    val names = machines.associate { it.id to it.displayName }
    // The assistant's questions are answered in the Jarvis tab (iOS filters them the same way).
    val assistantIds = byMachine.values.flatten().filter { it.assistant }.map { it.key }.toSet()
    val pending = allPending.filter { it.sessionKey !in assistantIds }
    val g = Grouper(pins, mutes)
    val base = byMachine.values.flatten().filter { it.matches(query, machineFilter) }
    val matching = base.filter { quick != QuickFilter.All && quick.matches(it) }.map { it.key }.toSet()
    LaunchedEffect(matching) { if (!kept.containsAll(matching)) kept = kept + matching }
    val all = base.underFilter(quick, kept)
    val searchingNow = query.isNotBlank()
    // Pinned agents show once, at the top, in pin order; every grouping is built from the rest (as on iOS).
    val pinnedAgents = all.filter { g.pinned(it) }.sortedBy { pins.sessions.indexOf(it.pinKeyOf()) }
    val rest = all.filterNot { g.pinned(it) }
    val (live, offline) = rest.partition { it.isLive }
    val row: @Composable (Session, Boolean) -> Unit = { s, showSpace ->
        val content: @Composable () -> Unit = {
            SessionRow(s, names[s.machineId], g.muted(s), showSpace, onClick = { onOpen(s.key) }, onLongClick = { menuFor = s })
        }
        // A running agent swipes left to 关闭 (iOS swipeActions): the row springs back and the confirmation opens.
        if (s.isLive) SwipeToClose(s.key.path, onClose = { closingFor = s }, content = content) else content()
    }

    Scaffold(
        bottomBar = bottomBar,
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("Agents", maxLines = 1)
                        Text(
                            when (conn) {
                                ConnState.Online -> "${machines.count { it.online }}/${machines.size} 台电脑在线"
                                ConnState.Connecting -> "正在连接…"
                                ConnState.Offline -> "hub 连不上，正在重试"
                                ConnState.Unauthorized -> "需要重新配对"
                            },
                            style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                actions = {
                    IconButton(onClick = { searching = !searching; if (!searching) query = "" }) {
                        Icon(if (searching) Icons.Default.Close else Icons.Default.Search, if (searching) "关闭搜索" else "搜索")
                    }
                    if (machines.size > 1) MachineMenu(machines, machineFilter) { machineFilter = it; prefs.machineFilter = it; kept = emptySet() }
                    IconButton(onClick = onSettings) { Icon(Icons.Default.Settings, "设置") }
                },
            )
        },
        floatingActionButton = {
            if (machines.any { it.online }) {
                FloatingActionButton(onClick = { newSession = true }) { Icon(Icons.Default.Add, "新开会话") }
            }
        },
        snackbarHost = { notice?.let { Snackbar(Modifier.padding(12.dp), action = { TextButton({ notice = null }) { Text("好", maxLines = 1) } }) { Text(it) } } },
    ) { pad ->
      // The search field sits above the list, not in it: as a list item it opened scrolled out of sight.
      Column(Modifier.padding(pad).fillMaxSize()) {
        if (searching) {
            val focus = remember { FocusRequester() }
            LaunchedEffect(Unit) { focus.requestFocus() }
            OutlinedTextField(
                query, { query = it }, placeholder = { Text("搜索标题、目录、空间、最后一句") }, singleLine = true,
                leadingIcon = { Icon(Icons.Default.Search, null) },
                trailingIcon = { if (query.isNotEmpty()) IconButton({ query = "" }) { Icon(Icons.Default.Close, "清空") } },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp).focusRequester(focus),
            )
        }
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; hub.refresh(); refreshing = false } },
            modifier = Modifier.fillMaxSize(),
        ) {
            LazyColumn(Modifier.fillMaxSize()) {
                item(key = "quick") {
                    val counts = base.groupingBy { it.agentState }.eachCount()
                    Row(
                        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically,
                    ) {
                        QuickFilter.entries.forEach { f ->
                            val n = f.state?.let { counts[it] ?: 0 }
                            FilterChip(
                                quick == f,
                                onClick = {
                                    // Choosing a filter (again) starts from what matches now.
                                    quick = f; kept = base.filter { f != QuickFilter.All && f.matches(it) }.map { it.key }.toSet()
                                },
                                label = { Text(if (n != null && n > 0) "${f.title} $n" else f.title, maxLines = 1) },
                            )
                        }
                        val unread = all.filter { it.agentState == jarvis.android.data.AgentState.Unread }
                        if (unread.size > 1) TextButton({ confirmReadAll = unread }) { Text("全部标为已读", maxLines = 1) }
                    }
                }
                item(key = "grouping") {
                    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
                        Grouping.entries.forEachIndexed { i, it ->
                            SegmentedButton(
                                selected = grouping == it, onClick = { grouping = it; prefs.grouping = it },
                                shape = SegmentedButtonDefaults.itemShape(i, Grouping.entries.size),
                            ) { Text(it.title, maxLines = 1) }
                        }
                    }
                }
                if (pending.isNotEmpty() && !searchingNow && quick == QuickFilter.All) {
                    item { SectionTitle("需要你处理 · ${pending.size}") }
                    items(pending, key = { "ix/" + it.id }) { ix -> PendingRow(ix, names) { onOpen(ix.sessionKey) } }
                }
                if (pinnedAgents.isNotEmpty()) {
                    item { SectionTitle("置顶") }
                    items(pinnedAgents, key = { "pin/" + it.key.path }) { s -> row(s, true) }
                }
                when (grouping) {
                    Grouping.Space -> spaceSections(
                        // While searching, every match shows (nothing stays folded); under a quick filter, too.
                        g, live, if (searchingNow || quick != QuickFilter.All) emptySet() else folded, names,
                        multiMachine = machines.size > 1 && machineFilter == null, ::toggleFold, row,
                        expandAll = searchingNow || quick != QuickFilter.All, onOpenSpace = onOpenSpace,
                        onSpaceAction = { space, act ->
                            when (act) {
                                SpaceAction.Rename -> renamingSpace = space
                                SpaceAction.Pin -> scope.launch { runCatching { hub.toggleSpacePin(space) }.onFailure { notice = "置顶没成功：${it.message}" } }
                                SpaceAction.Mute -> scope.launch { runCatching { hub.toggleSpaceMute(space) }.onFailure { notice = "屏蔽没成功：${it.message}" } }
                            }
                        },
                    )
                    Grouping.State -> g.byState(live).forEach { (rank, list) ->
                        val id = "state/" + rank.name
                        // 已看 is the long tail: folded unless the user opened it.
                        val isFolded = !(searchingNow || quick != QuickFilter.All) && (if (rank == Rank.Seen) id !in folded else id in folded)
                        item(key = id) { FoldHeader(rank.title, "${list.size} 个", isFolded) { toggleFold(id) } }
                        if (!isFolded) items(list, key = { "$id/" + it.key.path }) { s -> row(s, true) }
                    }
                    Grouping.Machine -> g.byMachine(live, machines).forEach { (m, list) ->
                        val id = "machine/" + m.id
                        val isFolded = !(searchingNow || quick != QuickFilter.All) && id in folded
                        val detail = (if (m.online) "在线" else "离线") + " · " + stateCounts(list)
                        item(key = id) { FoldHeader(m.displayName, detail, isFolded) { toggleFold(id) } }
                        if (!isFolded) items(list, key = { "$id/" + it.key.path }) { s -> row(s, true) }
                    }
                }
                if (offline.isNotEmpty()) {
                    val id = "offline"
                    item(key = id) { FoldHeader("最近 7 天", "${offline.size} 个", id in folded) { toggleFold(id) } }
                    if (id !in folded) items(g.ordered(offline), key = { "off/" + it.key.path }) { s -> row(s, true) }
                }
                if (all.isEmpty() && (pending.isEmpty() || searchingNow || quick != QuickFilter.All)) {
                    item {
                        Text(
                            when {
                                conn != ConnState.Online -> "等待连接 hub…"
                                searchingNow -> "没有找到「${query.trim()}」"
                                quick != QuickFilter.All -> "现在没有「${quick.title}」的 Agent"
                                machineFilter != null -> "这台电脑上没有 Agent"
                                else -> "各台电脑的 herdr 里还没有 Agent。点右下角的 + 新开一个，或在电脑上的 herdr 里启动 claude。"
                            },
                            Modifier.padding(24.dp), color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                item { Spacer(Modifier.height(88.dp)) } // room for the + button
            }
        }
      }
    }

    closingFor?.let { s ->
        CloseAgentDialog(hub, s, names[s.machineId], onDismiss = { closingFor = null }, onError = { notice = it })
    }
    menuFor?.let { s ->
        AgentActionsSheet(
            hub, s, pinned = g.pinned(s), muted = g.muted(s), machine = names[s.machineId],
            onDismiss = { menuFor = null }, onError = { notice = it },
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
                    runCatching { hub.renameSpace(space, name) }.onFailure { notice = "重命名没成功：${it.message}" }
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
    if (newSession) {
        NewSessionSheet(hub, machines.filter { it.online }, onDismiss = { newSession = false }, onStarted = { newSession = false; onOpen(it) })
    }
}

private fun Session.pinKeyOf() = "$machineId/$sessionId"

enum class SpaceAction { Pin, Mute, Rename }

/**
 * 按项目: pinned spaces, the other busy ones, muted ones with work running, then 空闲的空间 folded. Inside a space,
 * busy and pinned agents first; the idle ones fold into one row so a big workspace stays short.
 */
private fun LazyListScope.spaceSections(
    g: Grouper, live: List<Session>, folded: Set<String>, names: Map<String, String>, multiMachine: Boolean,
    toggleFold: (String) -> Unit, row: @Composable (Session, Boolean) -> Unit,
    expandAll: Boolean, onOpenSpace: (String) -> Unit, onSpaceAction: (SpaceGroup, SpaceAction) -> Unit,
) {
    val layout = g.spaceLayout(live)
    fun space(sg: SpaceGroup) {
        val isFolded = sg.id in folded
        item(key = "space/" + sg.id) {
            SpaceHeader(sg, if (multiMachine) names[sg.machineId] else null, g.pinned(sg), g.muted(sg), isFolded, g.ordered(sg.sessions),
                onFold = { toggleFold(sg.id) }, onOpen = { onOpenSpace(sg.id) }, onAction = { onSpaceAction(sg, it) })
        }
        if (isFolded) return
        val ordered = g.ordered(sg.sessions)
        val busy = if (expandAll) ordered else ordered.filter { g.stays(it) }
        val idle = if (expandAll) emptyList() else ordered.filterNot { g.stays(it) }
        items(busy, key = { "s/" + it.key.path }) { s -> row(s, false) }
        if (idle.isNotEmpty()) {
            val idleId = "idle/" + sg.id
            if (idleId in folded) {
                items(idle, key = { "s/" + it.key.path }) { s -> row(s, false) }
                item(key = "$idleId/close") { TextButton({ toggleFold(idleId) }, Modifier.padding(start = 28.dp)) { Text("收起空闲的 ${idle.size} 个", maxLines = 1) } }
            } else {
                item(key = idleId) {
                    Text(
                        "空闲 ${idle.size} 个：" + idle.joinToString("、") { it.displayTitle },
                        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1, overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.fillMaxWidth().clickable { toggleFold(idleId) }.padding(start = 38.dp, end = 16.dp, top = 10.dp, bottom = 10.dp),
                    )
                }
            }
        }
    }
    layout.active.forEach(::space)
    if (layout.quiet.isNotEmpty()) {
        // 空闲的空间 is folded by default ("open" is what gets remembered), like 已看 in 按状态; searching opens it.
        val id = "quiet-spaces"
        val isFolded = !expandAll && id !in folded
        item(key = id) { FoldHeader("空闲的空间", "${layout.quiet.size} 个", isFolded) { toggleFold(id) } }
        if (!isFolded) layout.quiet.forEach(::space)
    }
}

@Composable
private fun SpaceHeader(
    sg: SpaceGroup, machine: String?, pinned: Boolean, muted: Boolean, folded: Boolean, sessions: List<Session>,
    onFold: () -> Unit, onOpen: () -> Unit, onAction: (SpaceAction) -> Unit,
) {
    var menu by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onFold).alpha(if (muted) 0.8f else 1f).padding(start = 16.dp, top = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(if (folded) Icons.Default.ExpandMore else Icons.Default.ExpandLess, null, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.primary)
        Spacer(Modifier.width(6.dp))
        if (pinned) Icon(Icons.Default.PushPin, "已置顶", Modifier.size(14.dp).padding(end = 2.dp), tint = MaterialTheme.colorScheme.primary)
        // The name gets the free width (it's what tells spaces apart); the counts sit at the end, before the ⋯.
        Text(
            listOfNotNull(machine, sg.name).joinToString(" › "), style = MaterialTheme.typography.titleSmall,
            color = MaterialTheme.colorScheme.primary, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
        )
        if (muted) Icon(Icons.Default.NotificationsOff, "已屏蔽", Modifier.padding(start = 4.dp).size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.width(8.dp))
        Text(stateCounts(sessions), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        // A visible ⋯: a long press on a header is easy to miss (iOS keeps a ⋯ there for the same reason).
        Box {
            IconButton(onClick = { menu = true }) { Icon(Icons.Default.MoreVert, "空间操作") }
            DropdownMenu(menu, onDismissRequest = { menu = false }) {
                DropdownMenuItem({ Text("打开空间") }, { menu = false; onOpen() })
                DropdownMenuItem({ Text(if (pinned) "取消置顶空间" else "置顶空间") }, { menu = false; onAction(SpaceAction.Pin) })
                DropdownMenuItem({ Text(if (muted) "取消屏蔽空间" else "屏蔽这个空间") }, { menu = false; onAction(SpaceAction.Mute) })
                if (sg.workspaceId != null) DropdownMenuItem({ Text("重命名空间…") }, { menu = false; onAction(SpaceAction.Rename) })
            }
        }
    }
}

/** "1 需要你 · 2 工作中": the states worth counting on a header (seen ones aren't). */
private fun stateCounts(list: List<Session>): String {
    val by = list.groupingBy { it.agentState }.eachCount()
    val parts = listOf(AgentState.NeedsYou, AgentState.Unread, AgentState.Working).mapNotNull { st -> by[st]?.let { "$it ${st.label()}" } }
    return parts.joinToString(" · ").ifEmpty { "${list.size} 个" }
}

@Composable
private fun FoldHeader(title: String, detail: String, folded: Boolean, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(if (folded) Icons.Default.ExpandMore else Icons.Default.ExpandLess, null, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.primary)
        Spacer(Modifier.width(6.dp))
        Text(title, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, maxLines = 1)
        Spacer(Modifier.width(8.dp))
        Text(detail, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
    }
}

/** The list's grouping and folded sections, per phone (iOS keeps them in @AppStorage). */
private class ListPrefs(context: Context) {
    private val p = context.getSharedPreferences("agents_list", Context.MODE_PRIVATE)
    var grouping: Grouping
        get() = runCatching { Grouping.valueOf(p.getString("grouping", null) ?: "Space") }.getOrDefault(Grouping.Space)
        set(v) = p.edit().putString("grouping", v.name).apply()
    var folded: Set<String>
        get() = p.getStringSet("folded", emptySet())!!.toSet()
        set(v) = p.edit().putStringSet("folded", v).apply()
    /** One machine only, or null for all. */
    var machineFilter: String?
        get() = p.getString("machine", null)
        set(v) = p.edit().putString("machine", v).apply()
}

@Composable
private fun MachineMenu(machines: List<jarvis.android.data.Machine>, selected: String?, onPick: (String?) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        TextButton({ open = true }) {
            Text(machines.firstOrNull { it.id == selected }?.displayName ?: "全部电脑", maxLines = 1)
            Icon(Icons.Default.ExpandMore, null)
        }
        DropdownMenu(open, onDismissRequest = { open = false }) {
            DropdownMenuItem({ Text("全部电脑") }, { open = false; onPick(null) })
            machines.forEach { m ->
                DropdownMenuItem({ Text(m.displayName + if (m.online) "" else "（离线）") }, { open = false; onPick(m.id) })
            }
        }
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.padding(start = 16.dp, top = 16.dp, bottom = 4.dp),
    )
}

@Composable
private fun PendingRow(ix: Interaction, names: Map<String, String>, onClick: () -> Unit) {
    Card(
        onClick = onClick,
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer),
        modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp),
    ) {
        Column(Modifier.padding(12.dp)) {
            Text(
                listOfNotNull(names[ix.machineId], ix.sessionTitle?.takeIf { it.isNotBlank() } ?: ix.cwd?.substringAfterLast('/')).joinToString(" › "),
                style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            Text(ix.headline, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
fun StateDot(state: AgentState, size: Int = 10) {
    Box(Modifier.size(size.dp).clip(CircleShape).background(state.color()))
}

/** Swipe left reveals a red 关闭; letting go past the threshold asks [onClose] and the row springs back. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SwipeToClose(id: String, onClose: () -> Unit, content: @Composable () -> Unit) {
    val state = androidx.compose.runtime.key(id) {
        rememberSwipeToDismissBoxState(confirmValueChange = { v ->
            if (v == SwipeToDismissBoxValue.EndToStart) onClose()
            false
        })
    }
    SwipeToDismissBox(
        state, enableDismissFromStartToEnd = false,
        backgroundContent = {
            Row(
                Modifier.fillMaxSize().background(MaterialTheme.colorScheme.error).padding(end = 24.dp),
                horizontalArrangement = Arrangement.End, verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Default.Close, null, tint = MaterialTheme.colorScheme.onError)
                Spacer(Modifier.width(6.dp))
                Text("关闭", color = MaterialTheme.colorScheme.onError, maxLines = 1)
            }
        },
    ) {
        // Opaque, so the red only shows where the row has moved away.
        Box(Modifier.background(MaterialTheme.colorScheme.background)) { content() }
    }
}

@Composable
@OptIn(ExperimentalFoundationApi::class)
private fun SessionRow(
    s: Session, machine: String?, muted: Boolean, showSpace: Boolean, onClick: () -> Unit, onLongClick: () -> Unit,
) {
    // Muted agents read quieter (grey dot, slightly faded) but stay where their state puts them (iOS does the same).
    Row(
        Modifier.fillMaxWidth().combinedClickable(onClick = onClick, onLongClick = onLongClick)
            .alpha(if (muted) 0.8f else 1f).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (muted) Box(Modifier.size(10.dp).clip(CircleShape).background(AgentState.Idle.color())) else StateDot(s.agentState)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    s.displayTitle, style = MaterialTheme.typography.bodyLarge,
                    fontWeight = if (s.agentState.priority <= 1) FontWeight.SemiBold else FontWeight.Normal,
                    maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false),
                )
                if (muted) Icon(Icons.Default.NotificationsOff, "已屏蔽", Modifier.padding(start = 4.dp).size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.width(8.dp))
                Text(ago(s.updated), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            }
            // Inside a space section the space (and its machine) is already in the header.
            val where = listOfNotNull(
                machine.takeIf { showSpace }, s.spaceName?.takeIf { showSpace && it.isNotBlank() },
                s.slotName?.takeIf { !showSpace && it.isNotBlank() && it != s.slotIndex?.toString() }, s.agentLabel.takeIf { it != "Claude" },
            ).joinToString(" · ")
            val sub = buildString {
                if (s.agentState == AgentState.NeedsYou || s.agentState == AgentState.Working || s.agentState == AgentState.Unread) {
                    append(s.agentState.label()); if (where.isNotEmpty()) append(" · ")
                }
                append(where)
            }
            if (sub.isNotEmpty()) Text(sub, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            s.lastText?.takeIf { it.isNotBlank() }?.let {
                Text(
                    it.replace('\n', ' '), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2, overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
    HorizontalDivider(Modifier.padding(start = 38.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
}

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
