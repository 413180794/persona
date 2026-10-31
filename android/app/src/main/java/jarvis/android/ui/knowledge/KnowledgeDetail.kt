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
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.AddComment
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Sell
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberModalBottomSheetState
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.Hub
import jarvis.android.data.KbContext
import jarvis.android.data.KbDetail
import jarvis.android.data.KbDetailResponse
import jarvis.android.data.KbEntry
import jarvis.android.data.KbKind
import jarvis.android.data.KbLogLine
import jarvis.android.data.KbUpdate
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.data.kbDetail
import jarvis.android.data.kbUpdate
import jarvis.android.ui.Markdown
import jarvis.android.ui.NewSessionSheet
import jarvis.android.ui.StateDot
import jarvis.android.ui.ago
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

/** Timeline lines shown before 显示全部, related entries of one kind before 全部 (as iOS). */
private const val LOG_SHOWN = 6
private const val LINKS_SHOWN = 4

/** One entry's page (iOS `KnowledgeDetailView`): what it is, its Agents, timeline, profile, relations, recent messages. */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
internal fun KnowledgeDetailScreen(
    hub: Hub, start: KbEntry, onBack: () -> Unit, onOpen: (KbEntry) -> Unit,
    onAsk: (String) -> Unit, onOpenSession: (SessionKey) -> Unit, onChanged: () -> Unit,
) {
    var detail by remember { mutableStateOf<KbDetailResponse?>(null) }
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
    val d = detail?.detail
    val e = d?.merged(start) ?: start
    val kindTitle = KbKind.of(start.kind)?.title.orEmpty()
    val isProject = start.kind == KbKind.PROJECTS.wire

    Scaffold(topBar = {
        TopAppBar(
            title = { Text(kindTitle, maxLines = 1) },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
            actions = { IconButton(onClick = { editing = true }, enabled = d != null) { Icon(Icons.Outlined.Sell, "打标") } },
        )
    }) { pad ->
        PullToRefreshBox(isRefreshing = refreshing, onRefresh = { refreshing = true; attempt++ }, modifier = Modifier.fillMaxSize().padding(pad)) {
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 32.dp)) {
                item(key = "head") {
                    Header(e, isProject, onAsk = { onAsk(KbContext.ask(e, d)) }, onAgent = { newAgent = true })
                }
                when {
                    d != null -> {
                        val agents = detail?.agents.orEmpty()
                        if (agents.isNotEmpty()) {
                            item(key = "agents:h") { SectionTitle("在做这个项目的 Agent", agents.size) }
                            items(agents, key = { "a:" + it.machineId + "/" + it.sessionId }) { s -> AgentRow(hub, s) { onOpenSession(s.key) } }
                        }
                        timeline(d.log.orEmpty())
                        d.summary?.takeIf { it.isNotBlank() }?.let { sum ->
                            item(key = "summary:h") { SectionTitle("档案") }
                            item(key = "summary") {
                                SelectionContainer {
                                    Markdown(KbContext.profile(sum, e.oneLine), Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp))
                                }
                            }
                        }
                        links(d, onOpen)
                        d.recent?.takeIf { it.isNotBlank() }?.let { recent -> item(key = "recent") { Recent(recent) } }
                        item(key = "path") {
                            Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp)) {
                                SectionTitleBare("目录")
                                SelectionContainer { Text(d.path, fontSize = 12.sp, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                                Text("Jarvis、AI 分身和各台电脑上的 Agent 都能直接读这个目录。", style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.outline, modifier = Modifier.padding(top = 6.dp))
                            }
                        }
                    }
                    error != null -> item(key = "error") {
                        Column(Modifier.fillMaxWidth().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                            Text("读不到这一项：$error", color = MaterialTheme.colorScheme.error)
                            TextButton(onClick = { attempt++ }) { Text("重试", maxLines = 1) }
                        }
                    }
                    else -> item(key = "loading") { Loading() }
                }
            }
        }
    }
    if (editing) KbTagEditor(hub, e, onDismiss = { editing = false }, onSaved = { editing = false; attempt++; onChanged() })
    if (newAgent) {
        val machines by hub.machines.collectAsState()
        val at = KbContext.firstDir(d?.dirs.orEmpty())
        NewSessionSheet(
            hub, machines.filter { it.online }, onDismiss = { newAgent = false },
            onStarted = { newAgent = false; onOpenSession(it) },
            initialPrompt = KbContext.context(e, d) + "\n\n", initialDir = at,
        )
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Header(e: KbEntry, isProject: Boolean, onAsk: () -> Unit, onAgent: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            KbAvatar(e, 52.dp)
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                SelectionContainer { Text(e.name.ifEmpty { " " }, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.SemiBold, maxLines = 3) }
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                    ImportanceBadge(e.importance)
                    e.projectKind?.let { KbPill(it + "项目", if (it == "长期") Color(0xFF0A84FF) else null) }
                    e.projectStatusLabel?.let { KbPill(it) }
                    if (e.relevance > 0) KbPill("相关 ${e.relevance}", relevanceTint(e.relevance))
                }
            }
        }
        e.oneLine?.takeIf { it.isNotEmpty() }?.let { Text(it, style = MaterialTheme.typography.bodyLarge) }
        e.tags?.takeIf { it.isNotEmpty() }?.let { tags ->
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                tags.forEach { KbPill("#$it") }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Button(onClick = onAsk, enabled = e.name.isNotEmpty(), modifier = Modifier.weight(1f)) {
                Icon(Icons.Default.AutoAwesome, null, Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text("问 Jarvis", maxLines = 1)
            }
            if (isProject) OutlinedButton(onClick = onAgent, enabled = e.name.isNotEmpty(), modifier = Modifier.weight(1f)) {
                Icon(Icons.Default.AddComment, null, Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text("开 Agent", maxLines = 1)
            }
        }
    }
}

@Composable
private fun SectionTitleBare(text: String) {
    Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, maxLines = 1, modifier = Modifier.padding(bottom = 6.dp))
}

/** Newest first, a dot and a rail down the left, when and from where above each line; 显示全部 past the first few. */
private fun LazyListScope.timeline(log: List<String>) {
    if (log.isEmpty()) return
    val lines = log.asReversed().map(KbLogLine::parse)
    item(key = "log") {
        var all by rememberSaveable { mutableStateOf(false) }
        val shown = if (all) lines else lines.take(LOG_SHOWN)
        Column {
            SectionTitle("时间线", lines.size)
            shown.forEachIndexed { i, l ->
                TimelineRow(l, first = i == 0, last = i == shown.lastIndex && (all || lines.size <= LOG_SHOWN))
            }
            if (lines.size > LOG_SHOWN) TextButton(onClick = { all = !all }, modifier = Modifier.padding(start = 8.dp)) {
                Text(if (all) "收起" else "显示全部 ${lines.size} 条", maxLines = 1)
            }
        }
    }
}

@Composable
private fun TimelineRow(line: KbLogLine, first: Boolean, last: Boolean) {
    val cs = MaterialTheme.colorScheme
    val rail = cs.onSurface.copy(alpha = 0.12f)
    Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min).padding(horizontal = 16.dp)) {
        // The rail joins the dots: none above the newest, none below the oldest shown.
        Box(Modifier.width(14.dp).fillMaxHeight(), contentAlignment = Alignment.TopCenter) {
            Column(Modifier.fillMaxHeight(), horizontalAlignment = Alignment.CenterHorizontally) {
                Box(Modifier.width(1.5.dp).height(14.dp).background(if (first) Color.Transparent else rail))
                Box(Modifier.width(1.5.dp).weight(1f).background(if (last) Color.Transparent else rail))
            }
            Box(Modifier.padding(top = 10.dp).size(9.dp).background(if (first) cs.primary else cs.outline.copy(alpha = 0.6f), CircleShape))
        }
        Column(Modifier.weight(1f).padding(start = 10.dp, top = 6.dp, bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                val c = if (first) cs.primary else cs.onSurfaceVariant
                if (line.whenText.isNotEmpty()) Text(line.shortWhen(), fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = c, maxLines = 1)
                if (line.source.isNotEmpty()) Text(line.source, fontSize = 12.sp, color = c, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Text(line.text, style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/** Related entries by kind, a few each with 全部 for the rest; then the code directories. */
private fun LazyListScope.links(d: KbDetail, onOpen: (KbEntry) -> Unit) {
    val byKind = d.links.orEmpty().groupBy { it.kind }
    for (k in KbKind.entries) {
        val group = byKind[k.wire].orEmpty()
        if (group.isEmpty()) continue
        item(key = "links:" + k.wire) {
            var open by rememberSaveable { mutableStateOf(false) }
            Column {
                SectionTitle("关联的" + k.title, group.size)
                (if (open) group else group.take(LINKS_SHOWN)).forEach { l -> LinkRow(l) { onOpen(l) } }
                if (group.size > LINKS_SHOWN) TextButton(onClick = { open = !open }, modifier = Modifier.padding(start = 8.dp)) {
                    Text(if (open) "收起" else "全部 ${group.size} 个", maxLines = 1)
                }
            }
        }
    }
    if (d.dirs.isNotEmpty()) {
        item(key = "dirs:h") { SectionTitle("代码目录", d.dirs.size) }
        items(d.dirs, key = { "dir:$it" }) { dir ->
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.Folder, null, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                                Text(dir, fontFamily = FontFamily.Monospace, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(start = 10.dp))
            }
        }
    }
}

@Composable
private fun LinkRow(entry: KbEntry, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        KbAvatar(entry, 32.dp)
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Text(entry.name, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            entry.oneLine?.takeIf { it.isNotEmpty() }?.let {
                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = MaterialTheme.colorScheme.outline)
    }
}

@Composable
private fun Recent(recent: String) {
    var open by rememberSaveable { mutableStateOf(false) }
    Column {
        Row(
            Modifier.fillMaxWidth().clickable { open = !open }.padding(horizontal = 16.dp, vertical = 14.dp)
                .semantics { contentDescription = "最近 7 天的消息，" + if (open) "收起" else "展开" },
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("最近 7 天的消息", style = MaterialTheme.typography.bodyLarge, maxLines = 1, modifier = Modifier.weight(1f))
            Icon(if (open) Icons.Default.KeyboardArrowDown else Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = MaterialTheme.colorScheme.outline)
        }
        if (open) SelectionContainer {
            Text(recent, fontSize = 13.sp, lineHeight = 19.sp, modifier = Modifier.padding(horizontal = 16.dp))
        }
    }
}

/** An Agent working on the project: its state, title, where it runs, and its last line. */
@Composable
private fun AgentRow(hub: Hub, s: Session, onClick: () -> Unit) {
    val machines by hub.machines.collectAsState()
    val machine = machines.firstOrNull { it.id == s.machineId }?.displayName ?: s.machineId
    val cs = MaterialTheme.colorScheme
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp)) {
        Box(Modifier.padding(top = 6.dp)) { StateDot(s.agentState) }
        Column(Modifier.weight(1f).padding(start = 12.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(s.displayTitle, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                Text(ago(s.updated), fontSize = 11.sp, color = cs.outline, maxLines = 1, modifier = Modifier.padding(start = 8.dp))
            }
            s.lastText?.replace('\n', ' ')?.trim()?.takeIf { it.isNotEmpty() }?.let {
                Text(it, style = MaterialTheme.typography.bodyMedium, color = cs.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            Text(listOfNotNull(s.agentLabel, s.spaceName ?: s.folderName.takeIf { it.isNotEmpty() }, machine).joinToString(" · "),
                fontSize = 11.sp, color = cs.outline, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

private val projectKinds = listOf("short" to "短期", "long" to "长期")
private val projectStatuses = listOf("active" to "进行中", "paused" to "暂停", "done" to "已完成", "ignored" to "不是项目")

/** Importance, relevance, tags (and a project's kind / status) set by hand; the hub keeps them over the automatic ones. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun KbTagEditor(hub: Hub, entry: KbEntry, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val scope = rememberCoroutineScope()
    val isProject = entry.kind == KbKind.PROJECTS.wire
    var importance by remember { mutableStateOf(entry.importance?.takeIf { it.isNotEmpty() } ?: "P2") }
    var relevance by remember { mutableFloatStateOf(entry.relevance.coerceIn(0, 100).toFloat()) }
    var tags by remember { mutableStateOf(entry.tags.orEmpty().joinToString(" ")) }
    var projectKind by remember { mutableStateOf(entry.projectKindStatus.first) }
    var status by remember { mutableStateOf(entry.projectKindStatus.second) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    fun save() {
        saving = true; error = null
        scope.launch {
            val u = KbUpdate(importance, relevance.roundToInt(), KbContext.parseTags(tags),
                status = if (isProject) status else null, projectKind = if (isProject) projectKind else null)
            runCatching { hub.api.kbUpdate(entry.kind, entry.id, u) }.onSuccess { onSaved() }.onFailure { error = "没有保存：${it.message}" }
            saving = false
        }
    }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxWidth().navigationBarsPadding().imePadding()) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                TextButton(onClick = onDismiss) { Text("取消", maxLines = 1) }
                Text("打标 · " + entry.name, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f), textAlign = androidx.compose.ui.text.style.TextAlign.Center)
                if (saving) Box(Modifier.width(64.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) }
                else TextButton(onClick = ::save) { Text("保存", maxLines = 1, fontWeight = FontWeight.SemiBold) }
            }
            Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).padding(bottom = 24.dp)) {
                EditorTitle("重要级")
                Choice(listOf("P0", "P1", "P2", "P3").map { it to it }, importance) { importance = it }
                EditorTitle("与我相关 ${relevance.roundToInt()}")
                Slider(value = relevance, onValueChange = { relevance = (it / 5f).roundToInt() * 5f }, valueRange = 0f..100f)
                EditorTitle("标签")
                OutlinedTextField(tags, { tags = it }, singleLine = true, placeholder = { Text("用空格分隔") }, modifier = Modifier.fillMaxWidth())
                if (isProject) {
                    EditorTitle("类型")
                    Choice(projectKinds, projectKind) { projectKind = it }
                    EditorTitle("状态")
                    Choice(projectStatuses, status) { status = it }
                }
                Text("手动打的标会锁住，hub 自动整理时不再覆盖。", style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.outline, modifier = Modifier.padding(top = 16.dp))
                error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(top = 8.dp)) }
            }
        }
    }
}

@Composable
private fun EditorTitle(text: String) {
    Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, maxLines = 1,
        modifier = Modifier.padding(top = 18.dp, bottom = 8.dp))
}

/** A segmented choice; labels stay on one line. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Choice(options: List<Pair<String, String>>, value: String, onPick: (String) -> Unit) {
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        options.forEachIndexed { i, (k, label) ->
            SegmentedButton(selected = k == value, onClick = { onPick(k) }, shape = SegmentedButtonDefaults.itemShape(i, options.size), icon = {}) {
                Text(label, maxLines = 1, softWrap = false, fontSize = 13.sp)
            }
        }
    }
}
