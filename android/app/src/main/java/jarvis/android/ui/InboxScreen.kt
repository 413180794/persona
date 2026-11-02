package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.runtime.MutableState
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.JarvisApp
import jarvis.android.data.Hub
import jarvis.android.data.InboxBucket
import jarvis.android.data.InboxFilter
import jarvis.android.data.InboxItem
import jarvis.android.data.bucketed
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.Chip
import jarvis.android.ui.design.ChipLook
import jarvis.android.ui.design.ChoiceChip
import jarvis.android.ui.design.CountBadge
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.Monogram
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SearchField
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.design.surface
import kotlinx.coroutines.launch

/**
 * The inbox (iOS InboxView, docs/inbox.md), pushed from the shell's drawer: Feishu and mail as one list the hub's AI
 * has triaged, in 需要你 / 知悉即可 / 已折叠 / 已处理. The search field stays on top, then the source / kind filters.
 * A row swipes away as done; an AI-drafted reply shows right in its row with 修改 / 发送; tapping a row opens what
 * it says, an AI draft to send, and 交给 AI. The app never opens Feishu's own pages (「不做第二个飞书」).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun InboxScreen(
    hub: Hub, onBack: (() -> Unit)?, onHandOff: (HandOff) -> Unit,
    /** The item whose detail is open, by id: the shell keeps it, so folding / unfolding keeps it open. */
    openId: MutableState<String?> = remember { mutableStateOf(null) },
    /** A wide window: the detail is the right pane ([InboxDetailPane]), not a sheet over the list. */
    detailInPane: Boolean = false,
) {
    val items by hub.inbox.collectAsState()
    var filter by remember { mutableStateOf<InboxFilter>(InboxFilter.All) }
    var query by rememberSaveable { mutableStateOf("") }
    // 已折叠 and 已处理 start closed, the other two open (iOS).
    var open by rememberSaveable { mutableStateOf(setOf(InboxBucket.Action.name, InboxBucket.Fyi.name)) }
    var refreshing by remember { mutableStateOf(false) }
    // The open item as last seen in the list (it can leave the list while its detail is up).
    var openedItem by remember { mutableStateOf<InboxItem?>(null) }
    val openedId = openId.value
    if (openedId == null) openedItem = null else items.firstOrNull { it.id == openedId }?.let { openedItem = it }
    val opened = openedItem?.takeIf { it.id == openedId }
    var sending by remember { mutableStateOf<InboxItem?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { hub.loadInbox() }

    fun done(it: InboxItem, d: Boolean) {
        JarvisApp.instance.scope.launch { runCatching { hub.setInboxDone(it, d) }.onFailure { e -> Toasts.show("没改成：${e.message}", ToastStyle.ERROR) } }
    }

    Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
        PageHeader("收件箱", onBack)
        Column(Modifier.fillMaxWidth().padding(bottom = JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
            SearchField(query, { query = it }, "搜索标题、发件人、内容", Modifier.padding(horizontal = JSpace.page))
            val filters = InboxFilter.available(items)
            if (filters.size > 1) Row(
                Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = JSpace.page),
                horizontalArrangement = Arrangement.spacedBy(JSpace.s),
            ) {
                filters.forEach { f ->
                    ChoiceChip(f.title, filter == f, { filter = f }, lead = f.icon()?.let { ic ->
                        { Icon(ic, null, Modifier.size(JSize.markM), tint = if (filter == f) JTheme.colors.accent else JTheme.colors.secondaryText) }
                    })
                }
            }
        }
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; runCatching { hub.api.refreshInbox() }; hub.loadInbox(); refreshing = false } },
            modifier = Modifier.weight(1f).fillMaxWidth(),
        ) {
            val sections = items.bucketed(filter, query)
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = JSpace.xxl + WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding())) {
                if (sections.isEmpty()) item(key = "empty") {
                    when {
                        query.isNotBlank() -> EmptyState(JIcons.search, "没有找到", "没有和「${query.trim()}」匹配的消息或邮件。")
                        items.isEmpty() -> EmptyState(JIcons.inbox, "收件箱是空的", "hub 配好飞书（lark-connector）或在设置里加了邮箱后，需要你看的消息和邮件会出现在这里。")
                        else -> EmptyState(JIcons.done, "都看完了", "这个筛选下没有东西。")
                    }
                }
                sections.forEach { (bucket, list) ->
                    // While searching every section is open.
                    val isOpen = query.isNotBlank() || bucket.name in open
                    item(key = "h/" + bucket.name) {
                        SectionHeader(
                            bucket.title, count = list.size, folded = !isOpen,
                            countColor = if (bucket == InboxBucket.Action) JTheme.needsYou.text else null,
                            onClick = { open = if (bucket.name in open) open - bucket.name else open + bucket.name },
                        ) {
                            // Bulk actions on the quiet sections (iOS: 全部看过 / 全部清除).
                            if (bucket == InboxBucket.Fyi || bucket == InboxBucket.Noise) {
                                Text(
                                    if (bucket == InboxBucket.Fyi) "全部看过" else "全部清除", style = JTheme.type.secondaryMedium, color = JTheme.colors.accent, maxLines = 1,
                                    modifier = Modifier.clip(CircleShape).clickable { list.forEach { done(it, true) } }.padding(horizontal = JSpace.s, vertical = JSpace.xs),
                                )
                            }
                        }
                    }
                    if (isOpen) itemsIndexed(list, key = { _, it -> it.id }) { i, item ->
                        val first = i == 0
                        val last = i == list.lastIndex
                        if (bucket == InboxBucket.Done) {
                            CardSlice(first, last, dividerStart = InboxTextColumn) {
                                InboxRow(item, onClick = { openId.value = item.id }, selected = detailInPane && item.id == openedId, trailing = {
                                    Text("移回", style = JTheme.type.secondaryMedium, color = JTheme.colors.accent, maxLines = 1,
                                        modifier = Modifier.clip(CircleShape).clickable { done(item, false) }.padding(horizontal = JSpace.s, vertical = JSpace.xs))
                                })
                            }
                        } else {
                            val swipe = rememberSwipeToDismissBoxState(confirmValueChange = { v ->
                                if (v == SwipeToDismissBoxValue.EndToStart) done(item, true)
                                v == SwipeToDismissBoxValue.EndToStart
                            })
                            SwipeToDismissBox(
                                swipe, enableDismissFromStartToEnd = false,
                                backgroundContent = {
                                    Row(
                                        Modifier.fillMaxSize().padding(horizontal = JSpace.page).background(JTheme.colors.positive, RoundedCornerShape(JTheme.style.corner)).padding(end = JSpace.xl),
                                        horizontalArrangement = Arrangement.spacedBy(JSpace.sm, Alignment.End), verticalAlignment = Alignment.CenterVertically,
                                    ) {
                                        Icon(JIcons.done, null, Modifier.size(JSize.iconXS), tint = Color.White)
                                        Text("已处理", style = JTheme.type.secondaryStrong, color = Color.White, maxLines = 1)
                                    }
                                },
                            ) {
                                CardSlice(first, last, dividerStart = InboxTextColumn) {
                                    InboxRow(item, onClick = { openId.value = item.id }, onEditDraft = { openId.value = item.id }, onSendDraft = { sending = item },
                                        selected = detailInPane && item.id == openedId)
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    // A link tapped in the detail opens the link sheet, which can't float over this modal sheet: the detail steps
    // aside while the page is open and comes back, with what was typed, when it closes.
    val details = androidx.compose.runtime.saveable.rememberSaveableStateHolder()
    if (!detailInPane) opened?.takeIf { jarvis.android.ui.web.Links.current.value == null }?.let { item ->
        details.SaveableStateProvider(item.id) {
            InboxDetail(
                hub, item, onDismiss = { openId.value = null },
                onDone = { done(item, true); openId.value = null },
                onHandOff = { h -> if (h.markDone) done(item, true); openId.value = null; onHandOff(h) },
            )
        }
    }
    sending?.let { item ->
        ConfirmSend(item, item.agent?.draft.orEmpty(), onDismiss = { sending = null }) {
            sending = null
            JarvisApp.instance.scope.launch {
                runCatching { hub.api.inboxReply(item.id, item.agent?.draft.orEmpty().trim()) }
                    .onSuccess { hub.loadInbox(); Toasts.show("已发送", ToastStyle.SUCCESS) }
                    .onFailure { Toasts.show("没发出去：${it.message}", ToastStyle.ERROR) }
            }
        }
    }
}

/**
 * A wide window: the open item's detail as the right pane (the same content as the sheet), a readable column under
 * the shell's bar. Links open in the link sheet over it, so it needs no stepping aside.
 */
@Composable
fun InboxDetailPane(hub: Hub, id: String?, onClose: () -> Unit, onHandOff: (HandOff) -> Unit) {
    val items by hub.inbox.collectAsState()
    var last by remember(id) { mutableStateOf<InboxItem?>(null) }
    items.firstOrNull { it.id == id }?.let { last = it }
    val item = last
    Box(Modifier.fillMaxSize().background(JTheme.colors.page).statusBarsPadding().padding(top = JSize.shellBar), contentAlignment = Alignment.TopCenter) {
        if (item == null) {
            EmptyState(JIcons.inbox, "选一件事", "左边点一件事，详情和回复在这里。", Modifier.align(Alignment.Center))
            return@Box
        }
        fun done() { JarvisApp.instance.scope.launch { runCatching { hub.setInboxDone(item, true) } } }
        androidx.compose.runtime.key(item.id) {
            InboxDetailBody(
                hub, item, Modifier.widthIn(max = JSize.readable).navigationBarsPadding().padding(top = JSpace.s),
                onDismiss = onClose, onDone = { done(); onClose() },
                onHandOff = { h -> if (h.markDone) done(); onClose(); onHandOff(h) },
            )
        }
    }
}

/** Where a row's words start (after the avatar): its separator starts there too. */
private val InboxTextColumn = JSpace.l + JSize.avatar + JSpace.m

private fun InboxFilter.icon(): ImageVector? = when (this) {
    InboxFilter.All -> null
    InboxFilter.Feishu -> JIcons.feishuChats
    is InboxFilter.Account, InboxFilter.Mail -> JIcons.mail
    InboxFilter.Mention -> JIcons.mention
    InboxFilter.Dm -> JIcons.person
    InboxFilter.Group -> JIcons.groupChat
}

/** Senders' colors: never red or orange, which read as 需要你 (iOS). */
private val AVATAR_COLORS = listOf(Color(0xFF3B82F6), Color(0xFF10B981), Color(0xFF8B5CF6), Color(0xFF0EA5E9), Color(0xFF14B8A6), Color(0xFF6366F1))

/** Feishu's own blue on the source badge (its brand), mail's quiet gray. */
private val FEISHU_BLUE = Color(0xFF3370FF)

/**
 * One item (iOS `InboxRow`): the sender's initial with the source on its corner; the title (heavier when it is 需要你),
 * its kind (私聊 / @我…) and time; ✦ the AI's gist in its color; who said what; the AI 分身's work — busy, or its
 * drafted reply right here with 修改 / 发送.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun InboxRow(
    item: InboxItem, onClick: () -> Unit, trailing: (@Composable () -> Unit)? = null,
    onEditDraft: () -> Unit = {}, onSendDraft: () -> Unit = {},
    /** Its detail is open in the right pane. */
    selected: Boolean = false,
) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().then(if (selected) Modifier.background(c.fillChip) else Modifier).clickable(onClick = onClick).padding(start = JSpace.l, end = JSpace.m, top = JSpace.m, bottom = JSpace.m),
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Box(Modifier.size(JSize.avatar)) {
            Monogram(item.title, AVATAR_COLORS[(item.title.hashCode() and 0x7fffffff) % AVATAR_COLORS.size])
            Box(
                Modifier.align(Alignment.BottomEnd).offset(x = JSpace.xxs, y = JSpace.xxs).size(JSize.markL).clip(CircleShape).background(c.card).padding(JSize.lineMedium)
                    .clip(CircleShape).background(if (item.source == "lark") FEISHU_BLUE else c.countBadge),
                contentAlignment = Alignment.Center,
            ) { Icon(if (item.source == "lark") JIcons.feishuChats else JIcons.mail, null, Modifier.size(JSize.dotXL), tint = Color.White) }
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    Text(item.title, style = if (item.bucket == InboxBucket.Action) JTheme.type.heading else JTheme.type.body, color = c.label,
                        maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                    val mention = item.kind == "mention"
                    if (item.kindLabel.isNotBlank()) Chip(item.kindLabel, look = if (mention) ChipLook.Tint(JTheme.needsYou.tint, JTheme.needsYou.text) else ChipLook.Neutral)
                }
                Text(listTime(item.updated), style = JTheme.type.caption, color = c.tertiaryText, maxLines = 1)
            }
            val from = listOfNotNull(item.sender?.takeIf { it.isNotBlank() }, item.preview.takeIf { it.isNotBlank() }).joinToString("：")
            Row(verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
                    if (!item.summary.isNullOrBlank()) {
                        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                            Icon(JIcons.ai, null, Modifier.padding(top = JSpace.xxs).size(JSize.markS), tint = c.ai)
                            Text(item.summary, style = JTheme.type.secondary, color = c.label, maxLines = 2, overflow = TextOverflow.Ellipsis)
                        }
                        if (from.isNotBlank()) Text(from, style = JTheme.type.caption, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    } else if (from.isNotBlank()) {
                        Text(from, style = JTheme.type.secondary, color = c.secondaryText, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    }
                }
                if (item.count > 1) CountBadge(item.count, color = if (item.kind == "mention") c.accent else c.countBadge)
            }
            item.agent?.let { a ->
                when (a.state) {
                    "running", "queued" -> Row(Modifier.padding(top = JSpace.xxs), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        CircularProgressIndicator(Modifier.size(JSize.markXS), strokeWidth = JSize.lineMedium, color = c.ai)
                        Text("AI 分身正在处理：${a.task.orEmpty()}", style = JTheme.type.caption, color = c.ai, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    "drafted" -> a.draft?.takeIf { it.isNotBlank() }?.let { draft -> DraftCard(draft, onEditDraft, onSendDraft) }
                }
            }
        }
        trailing?.invoke()
    }
}

/** The AI 分身's drafted reply inside its row (iOS): ✦ 草拟的回复, the words, 修改 and 发送. */
@Composable
private fun DraftCard(draft: String, onEdit: () -> Unit, onSend: () -> Unit) {
    val c = JTheme.colors
    Column(
        Modifier.fillMaxWidth().padding(top = JSpace.s).clip(RoundedCornerShape(JRadius.card)).background(c.ai.copy(alpha = if (c.dark) 0.16f else 0.07f))
            .border(JSize.hairline, c.ai.copy(alpha = 0.25f), RoundedCornerShape(JRadius.card)).padding(JSpace.m),
        verticalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            Icon(JIcons.ai, null, Modifier.size(JSize.markS), tint = c.ai)
            Text("AI 分身草拟的回复", style = JTheme.type.captionStrong, color = c.ai, maxLines = 1)
        }
        Text(draft, style = JTheme.type.secondary, color = c.label, maxLines = 4, overflow = TextOverflow.Ellipsis)
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(JSpace.s, Alignment.End)) {
            PillButton("修改", onEdit, kind = ButtonKind.Tinted)
            PillButton("发送", onSend, kind = ButtonKind.Prominent, icon = JIcons.sendMail)
        }
    }
}

/** Sending as the user always asks first: where it goes and what it says. */
@Composable
private fun ConfirmSend(item: InboxItem, text: String, onDismiss: () -> Unit, onSend: () -> Unit) {
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("以你的身份发送？") },
        text = { Text((if (item.source == "lark") "发到飞书「${item.title}」" else "回复邮件「${item.title}」") + "：\n\n" + text.trim()) },
        confirmButton = { TextButton(onSend) { Text("发送", maxLines = 1) } },
        dismissButton = { TextButton(onDismiss) { Text("取消", maxLines = 1) } },
    )
}

/**
 * One item: what it says (the hub's text, no Feishu page), the AI summary, an AI-drafted reply to edit and send
 * (POST /reply sends it as the user, so it's an explicit button), and 交给 AI.
 */
@Composable
private fun InboxDetail(hub: Hub, item: InboxItem, onDismiss: () -> Unit, onDone: () -> Unit, onHandOff: (HandOff) -> Unit) {
    JSheet(onDismiss) { InboxDetailBody(hub, item, Modifier, onDismiss, onDone, onHandOff) }
}

/** The detail's content: in the sheet (a phone's width) or as the right pane (a wide window). */
@Composable
private fun InboxDetailBody(hub: Hub, item: InboxItem, modifier: Modifier, onDismiss: () -> Unit, onDone: () -> Unit, onHandOff: (HandOff) -> Unit) {
    var full by remember { mutableStateOf<InboxItem?>(null) }
    var draft by rememberSaveable { mutableStateOf(item.agent?.draft.orEmpty()) }
    var instruction by rememberSaveable { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var confirmSend by remember { mutableStateOf(false) }
    var handingOff by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val c = JTheme.colors
    LaunchedEffect(item.id) { runCatching { full = hub.api.inboxItem(item.id) } }

    run {
        Column(modifier.padding(horizontal = JSpace.page).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
            Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                Text(item.title, style = JTheme.type.title, color = c.label)
                Text(listOf(item.sourceLabel, item.kindLabel, listTime(item.updated)).filter { it.isNotBlank() }.joinToString(" · "), style = JTheme.type.caption, color = c.secondaryText)
            }
            item.summary?.takeIf { it.isNotBlank() }?.let {
                Row(
                    Modifier.fillMaxWidth().background(c.ai.copy(alpha = if (c.dark) 0.16f else 0.07f), RoundedCornerShape(JRadius.card)).padding(JSpace.m),
                    horizontalArrangement = Arrangement.spacedBy(JSpace.s),
                ) {
                    Icon(JIcons.ai, null, Modifier.padding(top = JSpace.xxs).size(JSize.markM), tint = c.ai)
                    Text(linkified(it) { url -> jarvis.android.ui.web.Links.open(url, null) }, style = JTheme.type.body, color = c.label)
                }
            }
            val text = full?.context?.takeIf { it.isNotBlank() } ?: item.preview
            Box(Modifier.fillMaxWidth().surface(RoundedCornerShape(JRadius.card)).heightIn(max = JSize.box280 + JSize.box120).verticalScroll(rememberScrollState()).padding(JSpace.m)) {
                Text(linkified(text) { url -> jarvis.android.ui.web.Links.open(url, null) }, style = JTheme.type.secondary, color = c.label)
            }
            if (full == null) CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule, color = c.secondaryText)

            SectionHeader("回复", modifier = Modifier.padding(start = 0.dp))
            item.agent?.note?.takeIf { it.isNotBlank() && item.agent.draft == draft }?.let {
                Text("AI 分身的依据：$it", style = JTheme.type.caption, color = c.secondaryText)
            }
            JTextField(draft, { draft = it }, "自己写，或者让 AI 草拟", minLines = 3, enabled = !busy, fill = c.card)
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                JTextField(instruction, { instruction = it }, if (draft.isBlank()) "要怎么回（可以不填）" else "让 AI 改：…", Modifier.weight(1f), singleLine = true, enabled = !busy, fill = c.card)
                PillButton(if (draft.isBlank()) "AI 草拟" else "AI 改", {
                    busy = true; error = null
                    scope.launch {
                        runCatching { hub.api.inboxDraft(item.id, listOfNotNull(instruction.takeIf { it.isNotBlank() }, draft.takeIf { it.isNotBlank() }?.let { "在这版上改：$it" }).joinToString("\n")) }
                            .onSuccess { draft = it; instruction = "" }.onFailure { error = "没草拟出来：${it.message}" }
                        busy = false
                    }
                }, kind = ButtonKind.Tinted, icon = JIcons.ai, enabled = !busy)
            }
            error?.let { Text(it, color = c.danger, style = JTheme.type.caption) }
            Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalAlignment = Alignment.CenterVertically) {
                PillButton("交给 AI", { handingOff = true }, kind = ButtonKind.Neutral, enabled = !busy)
                if (!item.isDone) PillButton("已处理", onDone, kind = ButtonKind.Neutral, icon = JIcons.done, enabled = !busy)
                Spacer(Modifier.weight(1f))
                if (busy) CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule, color = c.secondaryText)
                else PillButton("发送", { confirmSend = true }, kind = ButtonKind.Prominent, icon = JIcons.sendMail, enabled = draft.isNotBlank())
            }
        }
    }
    if (handingOff) HandOffSheet(hub, full ?: item, onDismiss = { handingOff = false }, onSend = { handingOff = false; onHandOff(it) })
    if (confirmSend) ConfirmSend(item, draft, onDismiss = { confirmSend = false }) {
        confirmSend = false; busy = true; error = null
        scope.launch {
            runCatching { hub.api.inboxReply(item.id, draft.trim()) }.onSuccess { hub.loadInbox(); onDismiss() }.onFailure { error = "没发出去：${it.message}"; busy = false }
        }
    }
}

/** 交给 AI: the item as context (docs/protocol.md「消息里的标签」), plus what to do — iOS InboxHandOffSheet.message. */
fun handoffPrompt(item: InboxItem, ask: String = HANDOFF_PRESETS[0]): String {
    val title = (if (item.source == "lark") "飞书 · " else "邮件 · ") + item.title
    var text = item.context?.takeIf { it.isNotBlank() } ?: listOfNotNull(item.sender, item.preview).filter { it.isNotBlank() }.joinToString("：")
    item.lark?.feedId?.takeIf { it.isNotBlank() }?.let { text += "\n\n（飞书会话 feedId $it，可用飞书工具读更多上下文或回复）" }
    item.mail?.let { m -> m.uids.lastOrNull()?.let { uid -> text += "\n\n（邮件 account_id ${m.accountId}，mailbox ${m.mailbox}，uid $uid，可用 mail_read 读全文、mail_send 回复）" } }
    return ContextRef("context", title, "", text).xml + "\n\n" + ask
}

/** What to ask for, as the iOS app offers it. */
val HANDOFF_PRESETS = listOf("总结一下，告诉我要做什么，给出建议的回复", "查清楚背景再告诉我", "直接帮我处理，回复前先问我")

/** An inbox item handed to AI: the message to send, and to whom (null: Jarvis, a new conversation). */
data class HandOff(val prompt: String, val to: jarvis.android.data.SessionKey?, val markDone: Boolean)

/** The agents an item can go to: the most recently active live ones, not Jarvis itself (iOS: eight). */
fun handOffTargets(sessions: Collection<jarvis.android.data.Session>): List<jarvis.android.data.Session> =
    sessions.filter { !it.assistant && it.isLive }.sortedByDescending { it.updated ?: java.time.Instant.EPOCH }.take(8)

/**
 * 交给 AI (iOS InboxHandOffSheet): what to do with the item, and who does it — Jarvis in a new conversation, or one
 * of the agents running in herdr, as a message in its session.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun HandOffSheet(hub: Hub, item: InboxItem, onDismiss: () -> Unit, onSend: (HandOff) -> Unit) {
    var instruction by remember { mutableStateOf("") }
    var target by remember { mutableStateOf<jarvis.android.data.SessionKey?>(null) }
    var markDone by remember { mutableStateOf(true) }
    val byMachine by hub.sessions.collectAsState()
    val machines by hub.machines.collectAsState()
    val agents = remember(byMachine) { handOffTargets(byMachine.values.flatten()) }
    JSheet(onDismiss, title = "交给 AI", onClose = onDismiss) {
        Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
            SectionHeader("要它做什么")
            Column(Modifier.padding(horizontal = JSpace.page), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
                JTextField(instruction, { instruction = it }, HANDOFF_PRESETS[0], minLines = 2, fill = JTheme.colors.card)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    HANDOFF_PRESETS.forEach { p -> ChoiceChip(p, instruction == p, { instruction = p }) }
                }
            }
            SectionHeader("交给谁")
            GroupCard {
                TargetRow("Jarvis（新对话）", null, target == null) { target = null }
                agents.forEach { s ->
                    RowDivider(JSpace.l + JSize.iconS + JSpace.m)
                    TargetRow(s.displayTitle, machines.firstOrNull { it.id == s.machineId }?.displayName ?: s.machineId, target == s.key) { target = s.key }
                }
            }
            GroupCard(Modifier.padding(top = JSpace.s)) {
                Row(Modifier.fillMaxWidth().heightIn(min = JSize.box50).padding(horizontal = JSpace.l), verticalAlignment = Alignment.CenterVertically) {
                    Text("同时标为已处理", style = JTheme.type.body, color = JTheme.colors.label, modifier = Modifier.weight(1f))
                    androidx.compose.material3.Switch(markDone, { markDone = it }, colors = jarvis.android.ui.settings.jSwitchColors())
                }
            }
            PillButton(
                "发送", { onSend(HandOff(handoffPrompt(item, instruction.trim().ifEmpty { HANDOFF_PRESETS[0] }), target, markDone)) },
                Modifier.fillMaxWidth().padding(horizontal = JSpace.page, vertical = JSpace.s), kind = ButtonKind.Prominent, large = true, icon = JIcons.sendMail,
            )
        }
    }
}

@Composable
private fun TargetRow(title: String, subtitle: String?, selected: Boolean, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.box50).clickable(onClick = onClick).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Box(
            Modifier.size(JSize.iconS).clip(CircleShape).then(if (selected) Modifier.background(JTheme.colors.accent) else Modifier.border(JSize.lineMedium, JTheme.colors.tertiaryText, CircleShape)),
            contentAlignment = Alignment.Center,
        ) { if (selected) Icon(JIcons.done, null, Modifier.size(JSize.markS), tint = Color.White) }
        Column(Modifier.weight(1f)) {
            Text(title, style = JTheme.type.body, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            subtitle?.let { Text(it, style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1) }
        }
    }
}
