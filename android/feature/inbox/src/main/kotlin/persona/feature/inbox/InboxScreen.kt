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
    val items = orderHubInbox(snapshot.records.map { record ->
        pending[record.item.id]?.let { done ->
            record.item.copy(doneAt = if (done) java.time.Instant.now().toString() else null)
        } ?: record.item
    })
    fun list(settings: InboxListSettings) = buildInboxList(
        items = items, interactions = snapshot.interactions, sessions = snapshot.sessions, mutes = snapshot.mutes, settings = settings,
    )
    val result = list(InboxListSettings(source, kind, query, expanded.toSet() + section))
    fun working(row: InboxRow) = row.type == InboxRowType.Item &&
        records[row.id]?.agent?.state in setOf("running", "queued")
    // The summary speaks for the whole inbox, so it reads the unfiltered list.
    val overall = if (query.isEmpty() && source.isEmpty() && kind.isEmpty()) result else list(InboxListSettings())
    val workingRows = result.groups.filter { it.id != "done" }.flatMap { it.rows }.filter(::working)
    val triageGroups = result.groups.mapNotNull { group ->
        if (group.id == "done") group else {
            val rows = group.rows.filterNot(::working)
            if (rows.isEmpty()) null else group.copy(rows = rows, visibleRows = group.visibleRows.filterNot(::working))
        }
    }
    val displayGroups = buildList {
        triageGroups.firstOrNull { it.id == "action" }?.let { add(it) }
        if (workingRows.isNotEmpty()) add(InboxGroup("working", "Persona 在办", workingRows, workingRows, false))
        addAll(triageGroups.filter { it.id != "action" })
    }
    val groups = displayGroups.filter { section.isEmpty() || it.id == section }
    val needsCount = displayGroups.firstOrNull { it.id == "action" }?.count ?: 0
    val doneCount = result.groups.firstOrNull { it.id == "done" }?.count ?: 0
    val allCount = result.groups.sumOf { it.count }
    val sources = listOf("" to "所有来源") + snapshot.records.map { record ->
        record.item.account to record.sourceLabel.ifBlank { if (record.feedId != null) "飞书" else record.item.account.ifBlank { "邮件" } }
    }.filter { it.first.isNotEmpty() }.distinctBy { it.first }
    var showFilters by remember { mutableStateOf(false) }
    var filterPage by remember { mutableStateOf<String?>(null) }
    var handingOff by remember { mutableStateOf<String?>(null) }

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
    // 全部标为已处理 / 全部看过 / 全部清除, and their 撤销: one request for them all (the hub skips the ones that took new
    // messages since this list), shown at once, then a notice with 撤销 when something was filed.
    fun markAll(targets: List<persona.core.contracts.api.InboxDoneTarget>, done: Boolean, undoable: Boolean = true) {
        val asked = targets.filter { it.id !in pending }
        if (asked.isEmpty()) return
        asked.forEach { pending[it.id] = done }
        scope.launch {
            try {
                val result = repository.setDone(asked, done)
                error = null
                val notice = if (undoable) bulkNotice(result) else null
                if (notice != null) {
                    val back = result.changed.map { persona.core.contracts.api.InboxDoneTarget(it) }
                    val undo = if (back.isEmpty()) null else FeedbackAction("撤销") { markAll(back, false, undoable = false) }
                    if (undo != null) feedback?.notice(notice, undo) else feedback?.notice(notice)
                }
            }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) { if (feedback?.failure(e) != true) error = "操作失败，已恢复原来的状态" }
            finally { asked.forEach { pending.remove(it.id) } }
        }
    }
    LaunchedEffect(repository) { refresh(collectNow = false) }
    val bulk = bulkTargets(groups, records)
    val scopeText = bulkScope(section, sources.firstOrNull { it.first == source && source.isNotEmpty() }?.second,
        if (kind == "agent") "Agent" else kindLabel(kind), query)
    var showMore by remember { mutableStateOf(false) }
    if (showMore) {
        JActionSheet(title = "收件箱", onDismiss = { showMore = false },
            actions = listOf(JAction(bulkLabel(bulk.size, scopeText), PhIcons.Checks, enabled = bulk.isNotEmpty()) { markAll(bulk, true) }),
            note = (if (scopeText == null) "整个收件箱里还没处理的。" else "只标当前筛选：$scopeText。") + "只记在 Persona，不改飞书和邮件里的已读。")
    }
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
    // One remembered list per filter. The query and the folds stay out of the key: a new key recreates the page, so
    // typing would drop the search field's focus (and the keystroke) and folding a section would jump to the top.
    listStates.SaveableStateProvider(listOf(source, kind, section).toString()) {
        val scroll = rememberLazyListState()
        JListPage(
            title = "收件箱",
            summary = inboxSummary(overall, ::working),
            subtitle = inboxTitle(connection).takeIf { connection != ConnectionState.Online },
            titleBusy = connection in setOf(ConnectionState.Connecting, ConnectionState.Updating),
            onTitleClick = if (connection == ConnectionState.Online) null else ({ navigator.navigate(ConnectionSettingsRoute) }),
            state = scroll, refreshing = refreshing, onRefresh = { refresh() },
            actions = {
                JIconButton(PhIcons.FunnelSimple, "筛选", { showFilters = true }, badge = source.isNotEmpty() || kind.isNotEmpty())
                JIconButton(PhIcons.DotsThree, "更多", { showMore = true })
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
                when {
                    !filtered && failedEmpty -> JEmptyState("收件箱没能加载", "检查连接后再试一次。", action = {
                        JButton("重试", { refresh() }, enabled = !refreshing, kind = JButtonKind.Secondary)
                    })
                    query.isNotBlank() -> JEmptyState("没有找到", "没有和「${query.trim()}」匹配的消息或邮件。", icon = PhIcons.MagnifyingGlass)
                    snapshot.records.isEmpty() && snapshot.interactions.isEmpty() -> JEmptyState("收件箱是空的",
                        "hub 配好飞书（lark-connector）或在设置里加了邮箱后，需要你看的消息和邮件会出现在这里。", icon = PhIcons.Tray)
                    else -> JEmptyState("都看完了", "这个筛选下没有东西。", icon = PhIcons.CheckCircle)
                }
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
                                markAll(bulkTargets(listOf(group), records), true)
                            })
                        }) else null,
                    )
                }
                itemsIndexed(group.visibleRows, key = { _, row -> "${row.type}:${row.id}" }, contentType = { _, row -> row.type }) { index, row ->
                    val position = GroupPosition.of(index, group.visibleRows.size)
                    if (row.type == InboxRowType.Interaction) {
                        interactions[row.id]?.let { interaction ->
                            MessageItem(sender = "Persona", title = interaction.sessionTitle.ifBlank { interaction.title },
                                preview = interaction.summary ?: interaction.title, position = position, status = Status.NeedsYou,
                                onClick = { navigator.navigate(ChatRoute(interaction.machineId, interaction.sessionId)) })
                        }
                    } else records[row.id]?.let { record ->
                        val done = pending[record.item.id] ?: record.item.isDone
                        InboxItemRow(record, repository, navigator, composers, position, done, record.item.id in pending,
                            onDone = { markDone(record.item.id, !done) }, onHandOff = { handingOff = record.item.id })
                    }
                }
            }
        }
    }
    handingOff?.let { id -> records[id] }?.let { record ->
        InboxHandOffSheet(repository, record, navigator, onDismiss = { handingOff = null })
    }
}

@Composable
private fun InboxItemRow(record: InboxRecord, repository: InboxRepository, navigator: Navigator,
    composers: ReplyComposers, position: GroupPosition, done: Boolean, busy: Boolean, onDone: () -> Unit,
    onHandOff: () -> Unit,
) {
    val item = record.item
    var menu by remember { mutableStateOf(false) }
    if (menu) JActionSheet(title = item.title, onDismiss = { menu = false }, actions = listOf(
        JAction(if (done) "移回收件箱" else "已处理", PhIcons.Check, enabled = !busy, onClick = onDone),
        JAction("交给 AI", PhIcons.Robot, onClick = onHandOff),
    ))
    JSwipeActions(
        start = if (busy) null else SwipeAction(if (done) "移回收件箱" else "已处理", PhIcons.Check, Status.Unread, onDone),
        end = SwipeAction("交给 AI", PhIcons.Robot, Status.Working, onHandOff),
    ) {
        MessageItem(sender = item.sender.orEmpty().ifBlank { item.title }, title = item.title,
            selected = isInboxItemSelected(LocalDetailKey.current, item.id, record.feedId),
            position = position, avatar = rememberLarkAvatar(record.avatar),
            source = if (record.feedId != null) MessageSource.Lark else MessageSource.Mail,
            sourceLabel = if (record.feedId != null) "飞书" else "邮件",
            group = item.title.takeIf { record.feedId != null && item.kind in setOf("group", "mention", "bot") },
            time = item.updatedAt?.let(::displayTime), preview = item.summary?.takeIf { it.isNotBlank() } ?: item.preview,
            status = when { done -> null; record.agent?.state in setOf("running", "queued") -> Status.Working; item.triage == InboxTier.Action -> Status.NeedsYou; else -> null },
            tags = listOfNotNull(kindLabel(record).takeIf { it.isNotBlank() }, record.sourceLabel.takeIf { it.isNotBlank() && it !in setOf("飞书", "邮件") },
                "${record.count} 条消息".takeIf { record.count > 0 }),
            onClick = {
                record.feedId?.let { navigator.navigate(LarkRoute(it, item.id, record.startPosition)) }
                    ?: navigator.navigate(MailRoute(item.id))
            }, onLongClick = { menu = true }, extra = {
                record.localDelivery?.pendingStatus()?.let { PendingBadge(it) }
                    ?: record.localStatus?.let { InboxLocalStatus(it) }
                if (!done) AgentDraftCard(record, composers, navigator, repository = repository)
            })
    }
}

@Composable
internal fun StatusText(text: String, isError: Boolean = false) {
    JNotice(text, Modifier.padding(vertical = Tokens.Space.s), status = if (isError) Status.NeedsYou else Status.Unknown)
}

/**
 * The page's one line, for the whole inbox: give it the unfiltered list, since a search or a filter that shows nothing
 * is not 「都处理完了」. Rows an agent is working on are Persona's, not the user's to answer.
 */
internal fun inboxSummary(inbox: InboxListResult, working: (InboxRow) -> Boolean): String {
    val open = inbox.groups.filter { it.id != "done" }
    val busy = open.sumOf { group -> group.rows.count(working) }
    val needs = open.firstOrNull { it.id == "action" }?.rows?.count { !working(it) } ?: 0
    return if (needs == 0) "都处理完了" else "$needs 条要回，Persona 在办 $busy 件"
}

private fun kindLabel(kind: String) = when (kind) {
    "mention" -> "@我"; "dm" -> "私聊"; "group" -> "群聊"; "bot" -> "机器人"; "mail" -> "邮件"; else -> ""
}

/** The row's kind: an @ of everyone is 「@所有人」, a mail the user was only copied on is 「抄送」 (the old app's labels). */
internal fun kindLabel(record: InboxRecord): String = when {
    record.item.kind == "mention" && record.atAll -> "@所有人"
    record.item.kind == "mail" && record.mail?.direct == false -> "抄送"
    else -> kindLabel(record.item.kind)
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
