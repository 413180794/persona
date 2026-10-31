package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberModalBottomSheetState
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.JarvisApp
import jarvis.android.data.Hub
import jarvis.android.data.InboxBucket
import jarvis.android.data.InboxFilter
import jarvis.android.data.InboxItem
import jarvis.android.data.bucketed
import kotlinx.coroutines.launch

/**
 * The inbox tab (iOS InboxView, docs/inbox.md): Feishu and mail as one list the hub's AI has triaged, in 需要你 /
 * 知悉即可 / 已折叠 / 已处理. A row swipes away as done; tapping it opens what it says, an AI draft to send, and
 * 交给 Jarvis. The app never opens Feishu's own pages (「不做第二个飞书」): the item's text is what it shows.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun InboxScreen(hub: Hub, bottomBar: @Composable () -> Unit, onHandToJarvis: (String) -> Unit) {
    val items by hub.inbox.collectAsState()
    var filter by remember { mutableStateOf<InboxFilter>(InboxFilter.All) }
    var query by rememberSaveable { mutableStateOf("") }
    var searching by rememberSaveable { mutableStateOf(false) }
    // 已折叠 and 已处理 start closed, the other two open (iOS).
    var open by rememberSaveable { mutableStateOf(setOf(InboxBucket.Action.name, InboxBucket.Fyi.name)) }
    var refreshing by remember { mutableStateOf(false) }
    var opened by remember { mutableStateOf<InboxItem?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { hub.loadInbox() }

    fun done(it: InboxItem, d: Boolean) {
        JarvisApp.instance.scope.launch { runCatching { hub.setInboxDone(it, d) }.onFailure { e -> notice = "没改成：${e.message}" } }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("收件箱") },
                actions = {
                    IconButton({ searching = !searching; if (!searching) query = "" }) { Icon(if (searching) Icons.Default.Close else Icons.Default.Search, if (searching) "关闭搜索" else "搜索") }
                },
            )
        },
        bottomBar = bottomBar,
    ) { pad ->
        Column(Modifier.padding(pad).fillMaxSize()) {
            if (searching) OutlinedTextField(
                query, { query = it }, placeholder = { Text("搜索标题、发件人、内容、摘要") }, singleLine = true,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
            )
            val filters = InboxFilter.available(items)
            if (filters.size > 1) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                filters.forEach { f -> FilterChip(filter == f, { filter = f }, label = { Text(f.title, maxLines = 1) }) }
            }
            notice?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(horizontal = 16.dp)) }
            PullToRefreshBox(
                isRefreshing = refreshing,
                onRefresh = { scope.launch { refreshing = true; runCatching { hub.api.refreshInbox() }; hub.loadInbox(); refreshing = false } },
                modifier = Modifier.fillMaxSize(),
            ) {
                val sections = items.bucketed(filter, query)
                LazyColumn(Modifier.fillMaxSize()) {
                    if (sections.isEmpty()) item {
                        Text(
                            when {
                                query.isNotBlank() -> "没有找到「${query.trim()}」"
                                items.isEmpty() -> "收件箱是空的。hub 配好飞书（lark-connector）或在设置里加了邮箱后，需要你看的消息和邮件会出现在这里。"
                                else -> "这个筛选下没有东西"
                            },
                            Modifier.padding(24.dp), color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    sections.forEach { (bucket, list) ->
                        // While searching every section is open.
                        val isOpen = query.isNotBlank() || bucket.name in open
                        item(key = "h/" + bucket.name) {
                            Row(
                                Modifier.fillMaxWidth().clickable { open = if (bucket.name in open) open - bucket.name else open + bucket.name }
                                    .padding(start = 16.dp, end = 8.dp, top = 16.dp, bottom = 4.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Icon(if (isOpen) Icons.Default.ExpandLess else Icons.Default.ExpandMore, null, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.primary)
                                Text(" ${bucket.title} · ${list.size}", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f))
                                // Bulk actions on the quiet sections (iOS: 全部看过 / 全部清除).
                                if (bucket == InboxBucket.Fyi || bucket == InboxBucket.Noise) {
                                    TextButton({ list.forEach { done(it, true) } }) { Text(if (bucket == InboxBucket.Fyi) "全部看过" else "全部清除", maxLines = 1) }
                                }
                            }
                        }
                        if (isOpen) items(list, key = { it.id }) { item ->
                            if (bucket == InboxBucket.Done) {
                                InboxRow(item, onClick = { opened = item }, trailing = { TextButton({ done(item, false) }) { Text("移回", maxLines = 1) } })
                            } else {
                                val swipe = rememberSwipeToDismissBoxState(confirmValueChange = { v ->
                                    if (v == SwipeToDismissBoxValue.EndToStart) done(item, true)
                                    v == SwipeToDismissBoxValue.EndToStart
                                })
                                SwipeToDismissBox(
                                    swipe, enableDismissFromStartToEnd = false,
                                    backgroundContent = {
                                        Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.primaryContainer).padding(end = 24.dp), contentAlignment = Alignment.CenterEnd) {
                                            Text("已处理", color = MaterialTheme.colorScheme.onPrimaryContainer)
                                        }
                                    },
                                ) { InboxRow(item, onClick = { opened = item }) }
                            }
                        }
                    }
                }
            }
        }
    }
    opened?.let { item ->
        InboxDetail(
            hub, item, onDismiss = { opened = null },
            onDone = { done(item, true); opened = null },
            onHandToJarvis = { prompt -> done(item, true); opened = null; onHandToJarvis(prompt) },
        )
    }
}

/** Feishu blue / mail grey for the source dot; avatar colours never red or orange, which read as 需要你 (iOS). */
private val AVATAR_COLORS = listOf(Color(0xFF3B82F6), Color(0xFF10B981), Color(0xFF8B5CF6), Color(0xFF0EA5E9), Color(0xFF14B8A6), Color(0xFF6366F1))

@Composable
private fun InboxRow(item: InboxItem, onClick: () -> Unit, trailing: (@Composable () -> Unit)? = null) {
    Row(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surface).clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp)) {
        Box(Modifier.size(40.dp)) {
            val c = AVATAR_COLORS[(item.title.hashCode() and 0x7fffffff) % AVATAR_COLORS.size]
            Box(Modifier.size(40.dp).clip(CircleShape).background(c), contentAlignment = Alignment.Center) {
                Text(item.title.take(1).ifBlank { "?" }, color = Color.White, fontWeight = FontWeight.SemiBold)
            }
            Box(
                Modifier.size(16.dp).align(Alignment.BottomEnd).clip(CircleShape)
                    .background(if (item.source == "lark") Color(0xFF3370FF) else Color(0xFF9CA3AF)),
            )
        }
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
              // Title and its label take what's left; the time sits at the end.
              Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically) {
                Text(item.title, style = MaterialTheme.typography.bodyLarge, fontWeight = if (item.bucket == InboxBucket.Action) FontWeight.SemiBold else FontWeight.Normal,
                    maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                Spacer(Modifier.width(6.dp))
                val mention = item.kind == "mention"
                Text(
                    item.kindLabel, style = MaterialTheme.typography.labelSmall, maxLines = 1,
                    color = if (mention) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.background(
                        if (mention) MaterialTheme.colorScheme.errorContainer else MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(4.dp),
                    ).padding(horizontal = 4.dp, vertical = 1.dp),
                )
              }
                Spacer(Modifier.width(8.dp))
                Text(ago(item.updated), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            }
            val from = listOfNotNull(item.sender?.takeIf { it.isNotBlank() }, item.preview.takeIf { it.isNotBlank() }).joinToString("：")
            if (!item.summary.isNullOrBlank()) {
                Text("✦ " + item.summary, style = MaterialTheme.typography.bodyMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                if (from.isNotBlank()) Text(from, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            } else if (from.isNotBlank()) {
                Text(from, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            item.agent?.let { a ->
                when (a.state) {
                    "running", "queued" -> Text("AI 分身正在处理：${a.task.orEmpty()}", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.tertiary, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    "drafted" -> Text("AI 分身草拟好了回复，点开看", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.tertiary, maxLines = 1)
                }
            }
        }
        if (item.count > 1) Text("${item.count}", style = MaterialTheme.typography.labelSmall, modifier = Modifier.padding(start = 8.dp).background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(10.dp)).padding(horizontal = 6.dp, vertical = 2.dp))
        trailing?.invoke()
    }
    HorizontalDivider(Modifier.padding(start = 68.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
}

/**
 * One item: what it says (the hub's text, no Feishu page), the AI summary, an AI-drafted reply to edit and send
 * (POST /reply sends it as the user, so it's an explicit button), and 交给 Jarvis.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun InboxDetail(hub: Hub, item: InboxItem, onDismiss: () -> Unit, onDone: () -> Unit, onHandToJarvis: (String) -> Unit) {
    var full by remember { mutableStateOf<InboxItem?>(null) }
    var draft by remember { mutableStateOf(item.agent?.draft.orEmpty()) }
    var instruction by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var confirmSend by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(item.id) { runCatching { full = hub.api.inboxItem(item.id) } }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.navigationBarsPadding().padding(horizontal = 16.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(item.title, style = MaterialTheme.typography.titleLarge)
            Text(listOf(item.sourceLabel, item.kindLabel, ago(item.updated)).filter { it.isNotBlank() }.joinToString(" · "), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            item.summary?.takeIf { it.isNotBlank() }?.let {
                Text("✦ $it", style = MaterialTheme.typography.bodyLarge, modifier = Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.secondaryContainer, RoundedCornerShape(10.dp)).padding(12.dp))
            }
            val text = full?.context?.takeIf { it.isNotBlank() } ?: item.preview
            Text(text, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.heightIn(max = 280.dp).verticalScroll(rememberScrollState()))
            if (full == null) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)

            HorizontalDivider()
            Text("回复", style = MaterialTheme.typography.titleSmall)
            item.agent?.note?.takeIf { it.isNotBlank() && item.agent.draft == draft }?.let {
                Text("AI 分身的依据：$it", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            OutlinedTextField(draft, { draft = it }, placeholder = { Text("自己写，或者让 AI 草拟") }, modifier = Modifier.fillMaxWidth().heightIn(min = 96.dp), enabled = !busy)
            Row(verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(instruction, { instruction = it }, placeholder = { Text(if (draft.isBlank()) "要怎么回（可以不填）" else "让 AI 改：…") }, singleLine = true, modifier = Modifier.weight(1f), enabled = !busy)
                TextButton({
                    busy = true; error = null
                    scope.launch {
                        runCatching { hub.api.inboxDraft(item.id, listOfNotNull(instruction.takeIf { it.isNotBlank() }, draft.takeIf { it.isNotBlank() }?.let { "在这版上改：$it" }).joinToString("\n")) }
                            .onSuccess { draft = it; instruction = "" }.onFailure { error = "没草拟出来：${it.message}" }
                        busy = false
                    }
                }, enabled = !busy) { Text(if (draft.isBlank()) "AI 草拟" else "AI 改", maxLines = 1) }
            }
            error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(bottom = 16.dp)) {
                OutlinedButton({ onHandToJarvis(handoffPrompt(full ?: item)) }, enabled = !busy) { Text("交给 Jarvis", maxLines = 1) }
                if (!item.isDone) OutlinedButton(onDone, enabled = !busy) { Text("已处理", maxLines = 1) }
                Spacer(Modifier.weight(1f))
                Button({ confirmSend = true }, enabled = !busy && draft.isNotBlank()) {
                    if (busy) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp) else Text("发送", maxLines = 1)
                }
            }
        }
    }
    if (confirmSend) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { confirmSend = false },
            title = { Text("以你的身份发送？") },
            text = { Text((if (item.source == "lark") "发到飞书「${item.title}」" else "回复邮件「${item.title}」") + "：\n\n" + draft.trim()) },
            confirmButton = {
                TextButton({
                    confirmSend = false; busy = true; error = null
                    scope.launch {
                        runCatching { hub.api.inboxReply(item.id, draft.trim()) }.onSuccess { hub.loadInbox(); onDismiss() }.onFailure { error = "没发出去：${it.message}"; busy = false }
                    }
                }) { Text("发送", maxLines = 1) }
            },
            dismissButton = { TextButton({ confirmSend = false }) { Text("取消", maxLines = 1) } },
        )
    }
}

/** 交给 Jarvis: the item as context the assistant can act on (docs/protocol.md「消息里的标签」), plus what to do. */
fun handoffPrompt(item: InboxItem): String {
    val title = (if (item.source == "lark") "飞书 · " else "邮件 · ") + item.title
    val ids = listOfNotNull(
        item.lark?.feedId?.takeIf { it.isNotBlank() }?.let { "feed_id=$it" },
        item.mail?.let { m -> "account=${m.accountId} mailbox=${m.mailbox} uid=${m.uids.lastOrNull() ?: ""}" },
    ).joinToString(" ")
    val body = (item.context?.takeIf { it.isNotBlank() } ?: item.preview).replace("</jarvis-context>", "")
    return "<jarvis-context kind=\"context\" title=\"${title.replace("\"", "'")}\">${if (ids.isNotEmpty()) "($ids)\n" else ""}$body</jarvis-context>\n总结一下，告诉我要做什么，给出建议的回复。"
}
