package persona.feature.inbox

import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.api.ConnectionState
import persona.core.domain.inbox.*
import persona.core.navigation.ChatRoute
import persona.core.navigation.ConnectionSettingsRoute
import persona.core.navigation.Navigator
import persona.core.navigation.LocalDetailKey
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

@Composable
fun InboxScreen(repository: InboxRepository, navigator: Navigator) {
    val snapshot by repository.snapshot.collectAsStateWithLifecycle()
    val connection by repository.connectionState.collectAsStateWithLifecycle(ConnectionState.Connecting)
    val savedPreferences by repository.preferences.collectAsStateWithLifecycle(InboxPreferences())
    val feedback = persona.core.designsystem.LocalAppFeedback.current
    val scope = rememberCoroutineScope()
    val composers = rememberReplyComposers(repository)
    var filters by remember(repository) { mutableStateOf<InboxPreferences?>(null) }
    val currentFilters = filters ?: savedPreferences
    val query = currentFilters.query
    val source = currentFilters.source
    val kind = currentFilters.kind
    val section = currentFilters.section
    val expanded = currentFilters.expanded
    var preferenceError by remember { mutableStateOf(false) }
    val preferenceWrites = remember(repository) { kotlinx.coroutines.sync.Mutex() }
    fun filter(value: InboxPreferences) {
        filters = value
        scope.launch(start = kotlinx.coroutines.CoroutineStart.UNDISPATCHED) {
            kotlinx.coroutines.withContext(kotlinx.coroutines.NonCancellable) {
                preferenceWrites.lock()
                try { repository.savePreferences(value); preferenceError = false }
                catch (_: Exception) { preferenceError = true }
                finally { preferenceWrites.unlock() }
            }
        }
    }
    val listStates = rememberSaveableStateHolder()
    var error by remember { mutableStateOf<String?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val pending = remember { mutableStateMapOf<String, Boolean>() }
    val records = snapshot.records.associateBy { it.item.id }
    val interactions = snapshot.interactions.associateBy { it.id }
    val result = buildInboxList(
        items = orderHubInbox(snapshot.records.map { record ->
            pending[record.item.id]?.let { done ->
                record.item.copy(doneAt = if (done) java.time.Instant.now().toString() else null)
            } ?: record.item
        }),
        interactions = snapshot.interactions,
        sessions = snapshot.sessions,
        mutes = snapshot.mutes,
        settings = InboxListSettings(source, kind, query, expanded.toSet() + section),
    )
    fun working(row: InboxRow) = row.type == InboxRowType.Item &&
        records[row.id]?.agent?.state in setOf("running", "queued")
    val workingRows = result.groups.filter { it.id != "done" }.flatMap { it.rows }.filter(::working)
    val triageGroups = result.groups.mapNotNull { group ->
        if (group.id == "done") group else {
            val rows = group.rows.filterNot(::working)
            if (rows.isEmpty()) null else group.copy(rows = rows, visibleRows = group.visibleRows.filterNot(::working))
        }
    }
    val displayGroups = buildList {
        triageGroups.firstOrNull { it.id == "action" }?.let { add(it) }
        if (workingRows.isNotEmpty()) add(InboxGroup("working", "Jarvis 在办", workingRows, workingRows, false))
        addAll(triageGroups.filter { it.id != "action" })
    }
    val groups = displayGroups.filter { section.isEmpty() || it.id == section }
    val needsCount = displayGroups.firstOrNull { it.id == "action" }?.count ?: 0
    val workingCount = workingRows.size
    val doneCount = result.groups.firstOrNull { it.id == "done" }?.count ?: 0
    val allCount = result.groups.sumOf { it.count }
    val sources = listOf("" to "所有来源") + snapshot.records.map { record ->
        record.item.account to record.sourceLabel.ifBlank { if (record.feedId != null) "飞书" else record.item.account.ifBlank { "邮件" } }
    }.filter { it.first.isNotEmpty() }.distinctBy { it.first }
    var showFilters by remember { mutableStateOf(false) }
    var filterPage by remember { mutableStateOf<String?>(null) }

    fun refresh(collectNow: Boolean = true) {
        if (refreshing) return
        refreshing = true
        scope.launch {
            try { if (collectNow) repository.refreshNow() else repository.refresh(); error = null }
            catch (e: CancellationException) { throw e }
            // Background refreshes stay quiet; a pull or tap reports its result.
            catch (e: Exception) { if (feedback?.failure(e, operation = collectNow) != true) error = "收件箱刷新失败，请重试" }
            finally { refreshing = false }
        }
    }
    fun markDone(id: String, done: Boolean) {
        if (id in pending) return
        pending[id] = done
        scope.launch {
            try { repository.setDone(id, done); error = null }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) { if (feedback?.failure(e) != true) error = "操作失败，已恢复原来的状态" }
            finally { pending.remove(id) }
        }
    }
    LaunchedEffect(repository) { refresh(collectNow = false) }
    if (showFilters) {
        JActionSheet(title = "筛选收件箱", onDismiss = { showFilters = false }, actions = listOf(
            JAction("来源：${sources.firstOrNull { it.first == source }?.second ?: "所有来源"}", PhIcons.Tray) { filterPage = "source" },
            JAction("类型：${kindLabel(kind).ifBlank { if (kind == "agent") "Agent" else "所有类型" }}", PhIcons.FunnelSimple) { filterPage = "kind" },
            JAction("清除筛选", enabled = source.isNotEmpty() || kind.isNotEmpty()) {
                filter(currentFilters.copy(source = "", kind = ""))
            },
        ))
    }
    if (filterPage == "source") JSheet(onDismiss = { filterPage = null }, title = "来源") {
        // The source list can grow with connected accounts, so keep the choices scrollable.
        androidx.compose.foundation.lazy.LazyColumn {
            itemsIndexed(sources) { index, option ->
                JGroupItem(GroupPosition.of(index, sources.size)) {
                    JRow(option.second, onClick = { filter(currentFilters.copy(source = option.first)); filterPage = null },
                        trailing = { JRadio(source == option.first, null) })
                }
            }
        }
    }
    if (filterPage == "kind") JActionSheet(title = "类型", onDismiss = { filterPage = null }, actions =
        listOf("" to "所有类型", "mention" to "@我", "dm" to "私聊", "group" to "群聊", "mail" to "邮件", "agent" to "Agent").map { (value, label) ->
            JAction(label, checked = kind == value) { filter(currentFilters.copy(kind = value)) }
        })
    val failedEmpty = snapshot.records.isEmpty() && !snapshot.loading && (error ?: snapshot.error) != null
    listStates.SaveableStateProvider(listOf(query, source, kind, section, expanded.joinToString()).toString()) {
        val scroll = rememberLazyListState()
        JListPage(
            title = "收件箱",
            summary = if (needsCount == 0) "都处理完了" else "$needsCount 条要回，Jarvis 在办 $workingCount 件",
            subtitle = inboxTitle(connection).takeIf { connection != ConnectionState.Online },
            titleBusy = connection in setOf(ConnectionState.Connecting, ConnectionState.Updating),
            onTitleClick = if (connection == ConnectionState.Online) null else ({ navigator.navigate(ConnectionSettingsRoute) }),
            state = scroll, refreshing = refreshing, onRefresh = { refresh() },
            actions = {
                JIconButton(PhIcons.FunnelSimple, "筛选", { showFilters = true }, badge = source.isNotEmpty() || kind.isNotEmpty())
            },
        ) {
            item("filters") {
                QuickFilterBar(listOf(Bay("需要你", needsCount, Status.NeedsYou), Bay("全部", allCount), Bay("已处理", doneCount)),
                    selected = when (section) { "action" -> 0; "done" -> 2; else -> 1 },
                    onSelect = { filter(currentFilters.copy(section = listOf("action", "", "done")[it])) })
                Spacer(Modifier.height(Tokens.Space.s))
                JSearchField(query, { filter(currentFilters.copy(query = it)) }, "搜索标题、发件人和内容")
            }
            if (snapshot.loading) item("loading") { JLoading(text = "正在读取收件箱") }
            if (!failedEmpty) (error ?: snapshot.error?.takeIf { feedback == null || connection == ConnectionState.Online })?.let { message ->
                item("error") { StatusText(message, isError = true) }
            }
            if (preferenceError) item("preference-error") { StatusText("筛选条件未能保存，请重试", isError = true) }
            if (groups.isEmpty() && !snapshot.loading) item("empty") {
                val filtered = query.isNotEmpty() || source.isNotEmpty() || kind.isNotEmpty() || section.isNotEmpty()
                if (!filtered && failedEmpty) JEmptyState("收件箱没能加载", "检查连接后再试一次。", action = {
                    JButton("重试", { refresh() }, enabled = !refreshing, kind = JButtonKind.Secondary)
                }) else JEmptyState(if (filtered) "没有符合条件的事项" else "暂时没有新事项", "可以切换筛选，或下拉刷新。")
            }
            groups.forEach { group ->
                item("section:${group.id}", contentType = "section") {
                    JSectionHeader(
                        title = if (group.id == "fyi") "看看就好" else group.title,
                        count = group.count,
                        status = when (group.id) { "action" -> Status.NeedsYou; "working" -> Status.Working; else -> null },
                        trailing = if (group.id == "working") null else if (group.folded) "展开" else "收起",
                        onClick = if (group.id == "working") null else ({
                            filter(currentFilters.copy(section = if (section == group.id) "" else section,
                                expanded = if (group.id in expanded) expanded - group.id else expanded + group.id))
                        }),
                        action = if (group.id in setOf("fyi", "noise")) ({
                            JTextButton(if (group.id == "fyi") "全部看过" else "全部清除", {
                                group.rows.filter { it.type == InboxRowType.Item }.forEach { markDone(it.id, true) }
                            })
                        }) else null,
                    )
                }
                itemsIndexed(group.visibleRows, key = { _, row -> "${row.type}:${row.id}" }, contentType = { _, row -> row.type }) { index, row ->
                    val position = GroupPosition.of(index, group.visibleRows.size)
                    if (row.type == InboxRowType.Interaction) {
                        interactions[row.id]?.let { interaction ->
                            MessageItem(sender = "Jarvis", title = interaction.sessionTitle.ifBlank { interaction.title },
                                preview = interaction.summary ?: interaction.title, position = position, status = Status.NeedsYou,
                                onClick = { navigator.navigate(ChatRoute(interaction.machineId, interaction.sessionId)) })
                        }
                    } else records[row.id]?.let { record ->
                        val done = pending[record.item.id] ?: record.item.isDone
                        InboxItemRow(record, repository, navigator, composers, position, done, record.item.id in pending,
                            onDone = { markDone(record.item.id, !done) })
                    }
                }
            }
        }
    }
}

@Composable
private fun InboxItemRow(record: InboxRecord, repository: InboxRepository, navigator: Navigator,
    composers: ReplyComposers, position: GroupPosition, done: Boolean, busy: Boolean, onDone: () -> Unit,
) {
    val item = record.item
    var menu by remember { mutableStateOf(false) }
    val handoff = rememberInboxAiButton(repository, item.id, InboxAiAction.Handoff, "交给 Jarvis", null, navigator)
    if (menu) JActionSheet(title = item.title, onDismiss = { menu = false }, actions = listOf(
        JAction(if (done) "移回收件箱" else "已处理", PhIcons.Check, enabled = !busy, onClick = onDone),
        JAction("交给 Jarvis", PhIcons.Sparkle, enabled = handoff.enabled, onClick = handoff.onClick),
    ))
    JSwipeActions(
        start = if (busy) null else SwipeAction(if (done) "移回收件箱" else "已处理", PhIcons.Check, Status.Unread, onDone),
        end = if (handoff.enabled) SwipeAction("交给 Jarvis", PhIcons.Sparkle, Status.Working, handoff.onClick) else null,
    ) {
        MessageItem(sender = item.sender.orEmpty().ifBlank { item.title }, title = item.title,
            selected = isInboxItemSelected(LocalDetailKey.current, item.id, record.feedId),
            position = position,
            source = if (record.feedId != null) MessageSource.Lark else MessageSource.Mail,
            sourceLabel = if (record.feedId != null) "飞书" else "邮件",
            group = item.title.takeIf { record.feedId != null && item.kind in setOf("group", "mention", "bot") },
            time = item.updatedAt?.let(::displayTime), preview = item.summary?.takeIf { it.isNotBlank() } ?: item.preview,
            status = when { done -> null; record.agent?.state in setOf("running", "queued") -> Status.Working; item.triage == InboxTier.Action -> Status.NeedsYou; else -> null },
            tags = listOfNotNull(kindLabel(item.kind).takeIf { it.isNotBlank() }, record.sourceLabel.takeIf { it.isNotBlank() && it !in setOf("飞书", "邮件") },
                "${record.count} 条消息".takeIf { record.count > 0 }),
            onClick = {
                record.feedId?.let { navigator.navigate(LarkRoute(it, item.id, record.startPosition)) }
                    ?: navigator.navigate(MailRoute(item.id))
            }, onLongClick = { menu = true }, extra = {
                record.localDelivery?.pendingStatus()?.let { PendingBadge(it) }
                    ?: record.localStatus?.let { InboxLocalStatus(it) }
                handoff.pending?.let { JNotice(if (handoff.failed) "请求保存失败，请重试" else it.label, status = if (handoff.failed) Status.NeedsYou else Status.Working) }
                if (!done) AgentDraftCard(record, composers, navigator)
            })
    }
}

@Composable
internal fun Avatar(text: String, needsYou: Boolean = false) {
    JAvatar(text, badge = if (needsYou) PhIcons.Sparkle else null)
}

@Composable
internal fun StatusText(text: String, isError: Boolean = false) {
    JNotice(text, Modifier.padding(vertical = Tokens.Space.s), status = if (isError) Status.NeedsYou else Status.Unknown)
}

private fun kindLabel(kind: String) = when (kind) {
    "mention" -> "@我"; "dm" -> "私聊"; "group" -> "群聊"; "bot" -> "机器人"; "mail" -> "邮件"; else -> ""
}

private fun displayTime(value: String): String = runCatching {
    java.time.Instant.parse(value).atZone(java.time.ZoneId.systemDefault()).format(java.time.format.DateTimeFormatter.ofPattern("MM-dd HH:mm"))
}.getOrDefault("")

internal fun inboxTitle(state: ConnectionState): String = when (state) {
    ConnectionState.Online -> "收件箱"
    ConnectionState.Connecting -> "连接中…"
    ConnectionState.Updating -> "更新中…"
    ConnectionState.WaitingForNetwork -> "等待网络"
    ConnectionState.NeedsLocalNetworkPermission -> "需要本地网络权限"
    ConnectionState.CertificateMismatch -> "hub 的证书变了"
    ConnectionState.Revoked -> "配对已失效"
}

@Composable
private fun InboxLocalStatus(label: String) {
    val pending = PendingStatus.entries.firstOrNull { label.startsWith(it.label) }
    if (pending != null) {
        PendingBadge(pending, reason = if (pending == PendingStatus.Refused) label.substringAfter("：", "").takeIf { it.isNotBlank() } else null)
    } else {
        Text(label, style = PersonaTheme.type.micro,
            color = if (label.startsWith("草稿")) PersonaTheme.palette.cautionText else PersonaTheme.palette.ink3)
    }
}

@Composable
internal fun ReplyStatus(state: ReplyComposerState) {
    state.delivery.pendingStatus()?.let { PendingBadge(it) }
        ?: if (state.sending) PendingBadge(PendingStatus.Sending) else Unit
    state.error?.let { JNotice(it, status = Status.NeedsYou) }
}
