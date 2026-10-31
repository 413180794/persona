package jarvis.android.ui

import android.graphics.BitmapFactory
import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.Image
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
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Stop
import androidx.compose.ui.draw.clip
import androidx.compose.material.icons.filled.Cancel
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.AgentState
import jarvis.android.data.Hub
import jarvis.android.data.HubException
import jarvis.android.data.Item
import jarvis.android.data.SessionKey
import kotlinx.coroutines.launch
import kotlinx.serialization.json.jsonPrimitive
import java.util.UUID

/** One agent's conversation: live items, its pending cards, and the input box. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatScreen(
    hub: Hub, key: SessionKey, onBack: () -> Unit,
    /** The Jarvis tab's conversation: headless (no terminal), titled Jarvis, its reply streamed as it is written. */
    assistant: Boolean = false,
    /** Replaces the back arrow (the Jarvis tab opens its history instead). */
    navigation: (@Composable () -> Unit)? = null,
    actions: @Composable () -> Unit = {},
    /** Something (a tab bar) sits under the screen and takes the system navigation bar's inset itself. */
    embedded: Boolean = false,
) {
    val store = remember(key) { hub.chat(key) }
    DisposableEffect(key) { onDispose { hub.release(key) } }
    val state by store.state.collectAsState()
    val byMachine by hub.sessions.collectAsState()
    val session = byMachine[key.machineId]?.firstOrNull { it.sessionId == key.sessionId }
    val machines by hub.machines.collectAsState()
    val pending by hub.interactions.collectAsState()
    val cards = pending.filter { it.machineId == key.machineId && it.sessionId == key.sessionId }
    val scope = rememberCoroutineScope()

    // Looking at it counts as having seen the result (the hub then stops treating it as done-unseen). Only while the
    // app is in front: a chat left open behind the home screen must not swallow the "done" notification.
    val lifecycle by androidx.lifecycle.compose.LocalLifecycleOwner.current.lifecycle.currentStateFlow.collectAsState()
    val visible = lifecycle.isAtLeast(androidx.lifecycle.Lifecycle.State.RESUMED)
    LaunchedEffect(key, session?.agentState, visible) {
        if (visible && session?.agentState == AgentState.Unread) runCatching { hub.api.seen(key) }
    }

    val rows = remember(state.items) { buildRows(state.items).asReversed() }
    val list = rememberLazyListState()
    // The list is reversed (newest at index 0): new content shows without scrolling, older pages load at the end.
    LaunchedEffect(list) {
        snapshotFlow { list.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }.collect { last ->
            if (last >= rows.size - 3) store.loadOlder()
        }
    }
    // Follow the newest end while the user is there. A reversed list keeps its first visible item in place when
    // rows are added before it, so a long reply that grew while you read it would leave the next one off screen.
    // "There" = the newest end is on screen (index 0 visible), or the user just sent something.
    var follow by remember { mutableStateOf(true) }
    LaunchedEffect(list) {
        snapshotFlow { list.firstVisibleItemIndex == 0 }.collect { atNewest -> if (atNewest) follow = true }
    }
    LaunchedEffect(list) {
        snapshotFlow { list.isScrollInProgress }.collect { scrolling ->
            if (scrolling && list.firstVisibleItemIndex > 0) follow = false // the user scrolled back to read
        }
    }
    LaunchedEffect(rows.firstOrNull()?.id, state.live.length / 80, cards.size) {
        if (follow && rows.isNotEmpty()) list.scrollToItem(0)
    }

    var draft by rememberSaveable(key.path) { mutableStateOf("") }
    var sending by remember { mutableStateOf(false) }
    var refusal by remember { mutableStateOf<HubException?>(null) }
    var pendingMsgId by remember { mutableStateOf<String?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    var terminal by remember { mutableStateOf(false) }

    // Attachments (docs/protocol.md「发送文件」): photos go with the message; files are uploaded to the session's
    // computer first and their paths dropped into the text, which the user then sends (or not).
    val context = androidx.compose.ui.platform.LocalContext.current
    var images by remember(key.path) { mutableStateOf(listOf<jarvis.android.data.ImagePayload>()) }
    var uploading by remember { mutableStateOf<String?>(null) }
    var attachMenu by remember { mutableStateOf(false) }
    val pickPhotos = androidx.activity.compose.rememberLauncherForActivityResult(
        androidx.activity.result.contract.ActivityResultContracts.PickMultipleVisualMedia(jarvis.android.data.MAX_IMAGES),
    ) { uris ->
        if (uris.isEmpty()) return@rememberLauncherForActivityResult
        scope.launch {
            val room = jarvis.android.data.MAX_IMAGES - images.size
            if (uris.size > room) notice = "一次最多 ${jarvis.android.data.MAX_IMAGES} 张"
            val picked = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.Default) {
                uris.take(room.coerceAtLeast(0)).map { jarvis.android.data.imagePayload(context, it) }
            }
            if (picked.any { it == null }) notice = "有 ${picked.count { it == null }} 张照片读不出来"
            images = images + picked.filterNotNull()
        }
    }
    val pickFiles = androidx.activity.compose.rememberLauncherForActivityResult(
        androidx.activity.result.contract.ActivityResultContracts.OpenMultipleDocuments(),
    ) { uris ->
        if (uris.isEmpty()) return@rememberLauncherForActivityResult
        scope.launch {
            val paths = mutableListOf<String>()
            for (u in uris) {
                val info = jarvis.android.data.fileInfo(context, u)
                if (info == null) { notice = "读不到这个文件的大小，传不了"; continue }
                val (name, size) = info
                if (size > jarvis.android.data.MAX_UPLOAD_BYTES) { notice = "「$name」超过 200 MB，传不了"; continue }
                uploading = name
                runCatching { hub.api.upload(key.machineId, name, size) { context.contentResolver.openInputStream(u)!! } }
                    .onSuccess { paths += it.path }
                    .onFailure { e -> notice = "「$name」没传上去：${e.message}" }
            }
            uploading = null
            if (paths.isNotEmpty()) { draft = jarvis.android.data.withPaths(draft, paths); pendingMsgId = null }
        }
    }

    fun send(clearDraft: String? = null) {
        val text = draft.trim().ifEmpty { if (images.isNotEmpty()) "请看图片" else "" }
        if (text.isEmpty()) return
        val id = pendingMsgId ?: UUID.randomUUID().toString().also { pendingMsgId = it }
        sending = true; notice = null
        scope.launch {
            try {
                hub.api.send(key, text, id, clearDraft, images)
                draft = ""; pendingMsgId = null; images = emptyList()
                follow = true; list.scrollToItem(0) // what you sent and its answer show, wherever you had scrolled
            } catch (e: HubException) {
                when (e.code) {
                    "dialog_open", "input_draft" -> { refusal = e; if (e.fields?.get("delivered")?.jsonPrimitive?.content == "none") pendingMsgId = null }
                    else -> notice = e.message
                }
            } catch (e: Exception) {
                notice = "没有送达：${e.message}" // keep the id: a retry is typed at most once
            }
            sending = false
        }
    }

    if (terminal) {
        jarvis.android.terminal.TerminalScreen(hub, key, session?.displayTitle ?: key.sessionId.take(8), onClose = { terminal = false })
        return
    }

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = navigation ?: { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
                title = {
                    Column {
                        Text(
                            if (assistant) session?.title?.takeIf { it.isNotBlank() && it != "Claude Code" } ?: "Jarvis"
                            else session?.displayTitle ?: key.sessionId.take(8),
                            maxLines = 1, overflow = TextOverflow.Ellipsis,
                        )
                        val where = if (assistant) listOfNotNull(
                            "Jarvis", session?.agentState?.takeIf { it == AgentState.Working }?.let { "正在回复" },
                        ).joinToString(" · ") else listOfNotNull(
                            machines.firstOrNull { it.id == key.machineId }?.displayName,
                            session?.spaceName?.takeIf { it.isNotBlank() },
                            session?.agentState?.label(),
                        ).joinToString(" · ")
                        Text(where, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                    }
                },
                actions = {
                    if (session?.isLive == true && !assistant) IconButton(onClick = { terminal = true }) { Icon(Icons.Default.Terminal, "终端") }
                    if (session?.agentState == AgentState.Working) {
                        IconButton(onClick = { scope.launch { runCatching { hub.api.interrupt(key) }.onFailure { notice = it.message } } }) {
                            Icon(Icons.Default.Stop, "中断")
                        }
                    }
                    actions()
                },
            )
        },
    ) { pad ->
        Column(Modifier.padding(pad).fillMaxSize().imePadding()) {
            Box(Modifier.weight(1f)) {
                if (state.loading && rows.isEmpty()) {
                    CircularProgressIndicator(Modifier.align(Alignment.Center))
                } else {
                    LazyColumn(
                        state = list, reverseLayout = true, modifier = Modifier.fillMaxSize(),
                        verticalArrangement = Arrangement.spacedBy(6.dp),
                        contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 12.dp, vertical = 8.dp),
                    ) {
                        items(cards, key = { "ix/" + it.id }) { ix -> InteractionCard(hub, ix) }
                        // The reply being written (the assistant streams it; herdr agents don't), unless the transcript
                        // already holds it; otherwise a working line.
                        val lastText = (rows.firstOrNull() as? Row.Assistant)?.text
                        if (assistant && state.live.isNotBlank() && !(lastText != null && (lastText.startsWith(state.live) || lastText == state.live))) {
                            item(key = "live") { Markdown(state.live, Modifier.fillMaxWidth().padding(vertical = 2.dp)) }
                        } else if (session?.agentState == AgentState.Working) item(key = "working") {
                            Text(
                                if (assistant) "Jarvis 正在思考…" else "${session.agentLabel} 正在工作…",
                                style = MaterialTheme.typography.labelMedium, color = AgentState.Working.color(),
                            )
                        }
                        session?.queued?.takeIf { it.isNotEmpty() }?.let { q ->
                            item(key = "queued") {
                                Text("排队中：" + q.joinToString(" / "), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                        items(rows, key = { it.id }) { r -> RowView(hub, key, r) }
                        if (state.loadingOlder) item(key = "older") {
                            Box(Modifier.fillMaxWidth().padding(8.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(20.dp)) }
                        }
                    }
                }
            }
            // Live updates come over the WebSocket: say when it's down, or a reply that never shows looks like silence.
            val conn by hub.conn.collectAsState()
            if (conn != jarvis.android.data.ConnState.Online) Text(
                if (conn == jarvis.android.data.ConnState.Connecting) "正在连接 hub…，新消息稍后显示" else "和 hub 的连接断了，正在重连；发出去的消息不受影响",
                color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 2.dp),
            )
            notice?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(horizontal = 16.dp)) }
            session?.suggestion?.takeIf { it.isNotBlank() && draft.isEmpty() }?.let { sug ->
                AssistChip(onClick = { draft = sug }, label = { Text(sug, maxLines = 1, overflow = TextOverflow.Ellipsis) }, modifier = Modifier.padding(horizontal = 12.dp))
            }
            if (images.isNotEmpty()) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                images.forEachIndexed { i, img ->
                    Box {
                        val bmp = remember(img.data) {
                            runCatching { android.util.Base64.decode(img.data, android.util.Base64.DEFAULT).let { BitmapFactory.decodeByteArray(it, 0, it.size) } }.getOrNull()
                        }
                        bmp?.let { Image(it.asImageBitmap(), "第 ${i + 1} 张", Modifier.size(64.dp).clip(RoundedCornerShape(8.dp)), contentScale = androidx.compose.ui.layout.ContentScale.Crop) }
                        IconButton({ images = images.filterIndexed { j, _ -> j != i } }, Modifier.size(24.dp).align(Alignment.TopEnd)) {
                            Icon(Icons.Default.Cancel, "移除这张", tint = androidx.compose.ui.graphics.Color.White)
                        }
                    }
                }
            }
            uploading?.let { name ->
                Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                    CircularProgressIndicator(Modifier.size(14.dp), strokeWidth = 2.dp)
                    Text("  正在传「$name」到电脑上…", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            Row(Modifier.fillMaxWidth().then(if (embedded) Modifier else Modifier.navigationBarsPadding()).padding(8.dp), verticalAlignment = Alignment.Bottom) {
                Box {
                    IconButton({ attachMenu = true }, enabled = !sending && uploading == null) { Icon(Icons.Default.Add, "添加照片或文件") }
                    androidx.compose.material3.DropdownMenu(attachMenu, onDismissRequest = { attachMenu = false }) {
                        androidx.compose.material3.DropdownMenuItem({ Text("照片") }, {
                            attachMenu = false
                            pickPhotos.launch(androidx.activity.result.PickVisualMediaRequest(androidx.activity.result.contract.ActivityResultContracts.PickVisualMedia.ImageOnly))
                        }, enabled = images.size < jarvis.android.data.MAX_IMAGES)
                        androidx.compose.material3.DropdownMenuItem({ Text("文件（传到电脑上）") }, { attachMenu = false; pickFiles.launch(arrayOf("*/*")) })
                    }
                }
                OutlinedTextField(
                    draft, { draft = it; pendingMsgId = null },
                    placeholder = {
                        Text(
                            when {
                                assistant -> "问 Jarvis…"
                                session?.isLive == false -> "发消息会在电脑上恢复这个会话"
                                else -> "发给 ${session?.agentLabel ?: "Claude"}"
                            },
                        )
                    },
                    modifier = Modifier.weight(1f).heightIn(max = 180.dp), shape = RoundedCornerShape(20.dp),
                )
                IconButton(onClick = { send() }, enabled = !sending && uploading == null && (draft.isNotBlank() || images.isNotEmpty())) {
                    if (sending) CircularProgressIndicator(Modifier.size(20.dp)) else Icon(Icons.AutoMirrored.Filled.Send, "发送")
                }
            }
        }
    }

    refusal?.let { e ->
        val clearable = e.fields?.get("clearable")?.jsonPrimitive?.content
        val draftOnPc = e.fields?.get("draft")?.jsonPrimitive?.content
        AlertDialog(
            onDismissRequest = { refusal = null },
            title = { Text(if (e.code == "dialog_open") "电脑上有对话框" else "电脑的输入框里有没发出去的字") },
            text = {
                Text(
                    if (e.code == "dialog_open") "先处理掉电脑上的对话框（或在终端里按键），再发送。什么都没有打进去。"
                    else "输入框里是：${draftOnPc ?: "（读不到）"}\n什么都没有打进去。" + if (clearable == null) "\n多行草稿只能在电脑上处理。" else "",
                )
            },
            confirmButton = {
                if (clearable != null) TextButton(onClick = { refusal = null; send(clearDraft = clearable) }) { Text("清空并发送", maxLines = 1) }
                else TextButton(onClick = { refusal = null; terminal = true }) { Text("打开终端", maxLines = 1) }
            },
            dismissButton = { TextButton(onClick = { refusal = null }) { Text("取消", maxLines = 1) } },
        )
    }

}

@Composable
private fun RowView(hub: Hub, key: SessionKey, r: Row) {
    when (r) {
        is Row.User -> Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            Surface(
                color = MaterialTheme.colorScheme.primaryContainer, shape = RoundedCornerShape(16.dp),
                modifier = Modifier.widthIn(max = 320.dp),
            ) {
                Column(Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) {
                    r.from?.let { Text("来自 $it", style = MaterialTheme.typography.labelSmall) }
                    SelectionContainer { Text(r.text, style = MaterialTheme.typography.bodyLarge) }
                    if (r.queued) Text("工作中输入", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        is Row.Assistant -> Markdown(r.text, Modifier.fillMaxWidth().padding(vertical = 2.dp))
        is Row.Thinking -> Text(
            r.text.lineSequence().firstOrNull { it.isNotBlank() }?.let { "思考：$it" } ?: "思考",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
        )
        is Row.Tool -> ToolRow(hub, key, r)
        is Row.Image -> LazyImage(hub, key, r.item)
        is Row.Notice -> Text(r.text, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun ToolRow(hub: Hub, key: SessionKey, r: Row.Tool) {
    var open by remember { mutableStateOf(false) }
    var full by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val err = r.result?.isError == true
    Column(
        Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.45f), RoundedCornerShape(10.dp))
            .clickable { open = !open }.padding(horizontal = 10.dp, vertical = 7.dp).animateContentSize(),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(toolVerb(r.use.toolName), style = MaterialTheme.typography.labelLarge, color = if (err) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary)
            Spacer(Modifier.size(8.dp))
            Text(toolSubject(r.use), style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (r.result == null) CircularProgressIndicator(Modifier.size(12.dp), strokeWidth = 1.5.dp)
        }
        if (open) {
            val detail = toolDetail(r.use)
            if (detail.isNotBlank()) Mono(detail)
            r.result?.let { res ->
                Mono(full ?: res.text.orEmpty())
                if (res.fullSize != null && full == null) {
                    TextButton(onClick = { scope.launch { full = runCatching { hub.api.fullItem(key, res.id).text }.getOrNull() } }) { Text("看全文", maxLines = 1) }
                }
            }
        }
    }
}

@Composable
private fun Mono(text: String) {
    SelectionContainer {
        Text(
            text.take(20_000), fontFamily = FontFamily.Monospace, fontSize = 11.sp, lineHeight = 14.sp, softWrap = false,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 6.dp).heightIn(max = 400.dp).horizontalScroll(rememberScrollState()),
        )
    }
}

@Composable
private fun LazyImage(hub: Hub, key: SessionKey, item: Item) {
    val bmp by produceState<android.graphics.Bitmap?>(null, item.id) {
        val inline = item.image?.data?.takeIf { it.isNotEmpty() }
        val bytes = inline?.let { android.util.Base64.decode(it, android.util.Base64.DEFAULT) }
            ?: runCatching { hub.api.image(key, item.id) }.getOrNull()
        value = bytes?.let { BitmapFactory.decodeByteArray(it, 0, it.size) }
    }
    bmp?.let { Image(it.asImageBitmap(), null, Modifier.widthIn(max = 280.dp).heightIn(max = 360.dp)) }
        ?: Box(Modifier.size(120.dp).background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(8.dp)))
}
