package jarvis.android.ui

import android.graphics.BitmapFactory
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.interaction.DragInteraction
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Cancel
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.FormatQuote
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material.icons.rounded.KeyboardArrowDown
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.AgentState
import jarvis.android.data.Hub
import jarvis.android.data.HubException
import jarvis.android.data.SessionKey
import jarvis.android.chat.ConnectionWatch
import jarvis.android.chat.Outbox
import jarvis.android.chat.PendingMessage
import jarvis.android.ui.chat.ChatEnv
import jarvis.android.ui.web.LinkText
import jarvis.android.ui.web.Links
import jarvis.android.ui.web.LocalLinkSource
import jarvis.android.ui.web.WebViewPool
import jarvis.android.ui.chat.PendingBubble
import jarvis.android.ui.chat.ImageRow
import jarvis.android.ui.chat.LocalChatEnv
import jarvis.android.ui.chat.MessageMenuHost
import jarvis.android.ui.chat.MessageMenuState
import jarvis.android.ui.chat.NoticeRow
import jarvis.android.ui.chat.RecapCard
import jarvis.android.ui.chat.SelectTextDialog
import jarvis.android.ui.chat.SentPhotos
import jarvis.android.ui.chat.ThinkingRow
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.chat.ToolGroup
import jarvis.android.ui.chat.UserBubble
import jarvis.android.ui.chat.WorkBridge
import jarvis.android.ui.chat.WorkEndRow
import jarvis.android.ui.chat.WorkLineRow
import jarvis.android.ui.chat.WorkStepRow
import jarvis.android.ui.chat.WorkingIndicator
import jarvis.android.ui.chat.AssistantText
import jarvis.android.ui.chat.composeMessage
import jarvis.android.ui.chat.quoted
import jarvis.android.ui.files.FilesScreen
import jarvis.android.ui.files.FilesTarget
import jarvis.android.ui.files.gitStatus
import jarvis.android.ui.media.ImageViewerArgs
import jarvis.android.ui.media.ImageViewerScreen
import jarvis.android.ui.media.chatImages
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.jsonPrimitive
import java.util.UUID

/** One line of the (reversed) chat list, newest first. */
private sealed interface ChatLine {
    val key: String

    data class Pending(val p: PendingMessage) : ChatLine { override val key get() = "p:" + p.clientMsgId }
    data class Live(val text: String) : ChatLine { override val key get() = "live" }
    data class Working(val label: String) : ChatLine { override val key get() = "working" }
    data class Queued(val text: String) : ChatLine { override val key get() = "queued" }
    data class Line(val row: FoldedRow) : ChatLine { override val key get() = row.key }
    data object Older : ChatLine { override val key get() = "older" }
}

/**
 * One agent's conversation: live items, its pending cards, and the input box. Each turn's work folds behind one line
 * under the user's message (iOS `chatLines`, docs/ios.md「展示逻辑」); a long press on a message opens its menu;
 * files, Git and pictures open over the page.
 */
@OptIn(ExperimentalMaterial3Api::class, androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
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
    /** 「交给 Jarvis」 on a message: the prompt for a new assistant conversation (null: not offered). */
    onHandToJarvis: ((String) -> Unit)? = null,
    /** Put the cursor in the composer once (the 「问 Jarvis」 shortcut), then [onFocusTaken]. */
    focusComposer: Boolean = false, onFocusTaken: () -> Unit = {},
) {
    val store = remember(key) { hub.chat(key) }
    DisposableEffect(key) {
        ConnectionWatch.chatOpened(key) // its backlog ends the title's 更新中…
        onDispose { ConnectionWatch.chatClosed(key); hub.release(key) }
    }
    // Messages sent from this phone that the transcript doesn't show yet (iOS Outbox): drawn at once, sent in the
    // background; written offline they wait (on disk) and go out in order once connected.
    val outbox by remember(key) { Outbox.of(key) }.collectAsState(emptyList())
    val state by store.state.collectAsState()
    val byMachine by hub.sessions.collectAsState()
    val session = byMachine[key.machineId]?.firstOrNull { it.sessionId == key.sessionId }
    val machines by hub.machines.collectAsState()
    val machine = machines.firstOrNull { it.id == key.machineId }
    val pending by hub.interactions.collectAsState()
    val cards = pending.filter { it.machineId == key.machineId && it.sessionId == key.sessionId }
    val working = session?.agentState == AgentState.Working
    val scope = rememberCoroutineScope()

    // Looking at it counts as having seen the result (the hub then stops treating it as done-unseen). Only while the
    // app is in front: a chat left open behind the home screen must not swallow the "done" notification.
    val lifecycle by androidx.lifecycle.compose.LocalLifecycleOwner.current.lifecycle.currentStateFlow.collectAsState()
    val visible = lifecycle.isAtLeast(androidx.lifecycle.Lifecycle.State.RESUMED)
    LaunchedEffect(key, session?.agentState, visible) {
        if (visible && session?.agentState == AgentState.Unread) runCatching { hub.api.seen(key) }
    }

    // What the rows reach for: files and pictures open over this page.
    var filesTarget by remember(key) { mutableStateOf<FilesTarget?>(null) }
    var viewer by remember(key) { mutableStateOf<ImageViewerArgs?>(null) }
    val env = remember(key) { ChatEnv(hub, key) }
    env.cwd = session?.cwd?.ifEmpty { null }
    env.canOpenFiles = machine != null && !session?.cwd.isNullOrEmpty()
    env.onOpenFiles = { filesTarget = it }
    env.onOpenImage = { viewer = it }
    val menu = remember(key) { MessageMenuState() }
    env.menu = menu

    // Rows are rebuilt off the main thread on each new item list; only the first frame builds them here.
    var rows by remember(key) { mutableStateOf(buildRows(state.items)) }
    val items = state.items
    LaunchedEffect(items) {
        rows = withContext(Dispatchers.Default) { buildRows(items) }
        Outbox.arrived(key, items) // a pending bubble gives way to the transcript's copy
    }
    // Work lines the user opened (by the id of their first step).
    var expandedWork by remember(key) { mutableStateOf(setOf<String>()) }
    val toggleWork: (String) -> Unit = { id -> expandedWork = if (id in expandedWork) expandedWork - id else expandedWork + id }
    val folded = remember(rows, working, expandedWork) { foldWork(rows, working, expandedWork) }
    // The viewer pages through every image; user tags warm up off the main thread (iOS `RowMemo.prewarm`).
    LaunchedEffect(items) {
        withContext(Dispatchers.Default) {
            env.gallery = chatImages(key, items)
            for (it in items.asReversed().take(400)) if (it.kind == "text" && it.role == "user") userText(it)
        }
    }

    // The reply being written (the assistant streams it; herdr agents don't), unless the transcript already holds it;
    // otherwise a working line when no running work line says so already.
    val lastText = ((rows.lastOrNull() as? ChatRow.Assistant)?.item?.cutText)
    val live = state.live.takeIf { assistant && it.isNotBlank() && !(lastText != null && lastText.startsWith(it)) }
    val runningWork = folded.any { it is FoldedRow.Work && it.running }
    val lines = remember(folded, outbox, live, working, runningWork, session?.queued, state.loadingOlder) {
        buildList {
            when {
                live != null -> add(ChatLine.Live(live))
                working && !runningWork -> add(ChatLine.Working(if (assistant) "Jarvis 正在思考…" else "${session?.agentLabel ?: "Claude"} 正在工作…"))
            }
            for (i in outbox.indices.reversed()) add(ChatLine.Pending(outbox[i]))
            session?.queued?.takeIf { it.isNotEmpty() }?.let { add(ChatLine.Queued("排队中：" + it.joinToString(" / "))) }
            for (i in folded.indices.reversed()) add(ChatLine.Line(folded[i]))
            if (state.loadingOlder) add(ChatLine.Older)
        }
    }

    val list = rememberLazyListState()
    // The list is reversed (newest at index 0): new content shows without scrolling, older pages load at the end.
    LaunchedEffect(list) {
        snapshotFlow { list.layoutInfo.let { (it.visibleItemsInfo.lastOrNull()?.index ?: 0) to it.totalItemsCount } }
            .distinctUntilChanged()
            .collect { (last, total) -> if (total > 0 && last >= total - 3) store.loadOlder() }
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
    // A drag closes the message menu (as on iOS: scrolling dismisses it).
    LaunchedEffect(list) { list.interactionSource.interactions.collect { if (it is DragInteraction.Start) menu.close() } }
    LaunchedEffect(lines.firstOrNull()?.key, state.live.length / 80) {
        if (follow && lines.isNotEmpty()) list.scrollToItem(0)
    }
    val farFromEnd by remember { derivedStateOf { list.firstVisibleItemIndex > 2 } }

    // Links in a fresh answer start loading now, so tapping one shows a finished page (docs/android.md「链接」).
    val appContext = androidx.compose.ui.platform.LocalContext.current.applicationContext
    LaunchedEffect(state.items.lastOrNull()?.id) {
        val urls = LinkText.toPreload(state.items, java.time.Instant.now())
        if (urls.isNotEmpty()) WebViewPool.preload(appContext, urls)
    }

    var draft by rememberSaveable(key.path) { mutableStateOf("") }
    // Refusals already explained (the dialog shows once per message; the bubble keeps its red mark).
    var refusalsShown by remember(key) { mutableStateOf(setOf<String>()) }
    var notice by remember { mutableStateOf<String?>(null) }
    var terminal by remember { mutableStateOf(false) }
    var moreMenu by remember { mutableStateOf(false) }
    val focus = remember { FocusRequester() }
    FocusComposer(focusComposer, focus, onFocusTaken)
    // Excerpts picked 「作为上下文」 (a message's menu or a page in the link sheet): chips above the input box, sent as
    // <jarvis-context> tags with the next message. Kept in [Links.contexts] so the link sheet can add to them.
    val contexts = Links.contexts[key.path].orEmpty()

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
            if (paths.isNotEmpty()) draft = jarvis.android.data.withPaths(draft, paths)
        }
    }

    // The message leaves the input box at once: the outbox draws it, sends it (or keeps it until connected) with one
    // client_msg_id, and puts a refusal or failure on the bubble (重发 / 删除 there).
    fun send() {
        val sentContexts = contexts
        val text = composeMessage(draft, images.isNotEmpty(), sentContexts) ?: return
        Outbox.send(hub, key, text, images)
        draft = ""; images = emptyList(); notice = null
        sentContexts.forEach { Links.removeContext(key, it) } // page picks live in Links.contexts
        follow = true; scope.launch { list.scrollToItem(0) } // what you sent and its answer show, wherever you had scrolled
    }

    // The message menu's actions (iOS `MessageMenu`): 引用 / 编辑 fill the input box, 交给 Jarvis starts an assistant
    // conversation about it (the inbox's path), 作为上下文 rides along with this chat's next message.
    val title = if (assistant) session?.title?.takeIf { it.isNotBlank() && it != "Claude Code" } ?: "Jarvis"
    else session?.displayTitle ?: key.sessionId.take(8)
    menu.quote = { text -> draft = quoted(text, draft); runCatching { focus.requestFocus() } }
    menu.edit = { text -> draft = text; runCatching { focus.requestFocus() } }
    menu.askJarvis = onHandToJarvis?.let { hand ->
        { text: String -> hand(ContextRef("context", "$title · 消息", "", text).xml + "\n\n看看这条消息，告诉我该怎么处理。") }
    }
    menu.asContext = { text ->
        Links.addContext(key, ContextRef("context", "$title · 消息", "", text))
        Toasts.show("已作为上下文，随下一条消息发送", ToastStyle.SUCCESS)
    }

    // Files and Git: whether the session's folder is a repository (and how many changes, for the menu).
    var gitCount by remember(key) { mutableStateOf<Int?>(null) }
    LaunchedEffect(env.cwd, machine != null) {
        val cwd = env.cwd ?: return@LaunchedEffect
        if (machine == null || assistant) return@LaunchedEffect
        gitCount = runCatching { hub.api.gitStatus(key.machineId, cwd) }.getOrNull()?.takeIf { it.isRepo }?.files?.size
    }
    val openTerminal: (() -> Unit)? = if (session?.isLive == true && !assistant) ({ terminal = true }) else null

    if (terminal) {
        jarvis.android.terminal.TerminalScreen(hub, key, session?.displayTitle ?: key.sessionId.take(8), onClose = { terminal = false })
        return
    }

    filesTarget?.let { t ->
        // Over the chat, as a pushed page would be; the image viewer (below) can still open on top of it.
        FilesScreen(hub, t, onBack = { filesTarget = null }, onOpenImage = { viewer = it })
        viewer?.let { ImageViewerOverlay(hub, it, onClose = { viewer = null }, onEdited = null) }
        return
    }

    CompositionLocalProvider(LocalChatEnv provides env, LocalLinkSource provides key) {
        Scaffold(
            topBar = {
                TopAppBar(
                    navigationIcon = navigation ?: { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
                    title = {
                        Column {
                            Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            val where = if (assistant) listOfNotNull(
                                "Jarvis", session?.agentState?.takeIf { it == AgentState.Working }?.let { "正在回复" },
                            ).joinToString(" · ") else listOfNotNull(
                                machine?.displayName,
                                session?.spaceName?.takeIf { it.isNotBlank() },
                                session?.agentState?.label(),
                            ).joinToString(" · ")
                            // While the hub is out of reach the subtitle says so, with a small spinner (docs/ios.md「连接状态」).
                            val link by ConnectionWatch.label.collectAsState()
                            val l = link
                            if (l != null) Row(verticalAlignment = Alignment.CenterVertically) {
                                if (l != "需要重新配对") CircularProgressIndicator(Modifier.size(11.dp), strokeWidth = 1.5.dp)
                                Spacer(Modifier.size(6.dp))
                                Text(l, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                            } else {
                                Text(where, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                            }
                        }
                    },
                    actions = {
                        if (openTerminal != null) IconButton(onClick = openTerminal) { Icon(Icons.Default.Terminal, "终端") }
                        if (working) {
                            IconButton(onClick = { scope.launch { runCatching { hub.api.interrupt(key) }.onFailure { notice = it.message } } }) {
                                Icon(Icons.Default.Stop, "中断")
                            }
                        }
                        actions()
                        val cwd = env.cwd
                        if (!assistant && cwd != null && machine != null) Box {
                            IconButton(onClick = { moreMenu = true }) { Icon(Icons.Default.MoreVert, "更多") }
                            DropdownMenu(moreMenu, onDismissRequest = { moreMenu = false }) {
                                DropdownMenuItem({ Text("文件", maxLines = 1) }, {
                                    moreMenu = false; filesTarget = FilesTarget(key.machineId, cwd, cwd, FilesTarget.Mode.DIR)
                                })
                                gitCount?.let { n ->
                                    DropdownMenuItem({ Text(if (n > 0) "Git（$n）" else "Git", maxLines = 1) }, {
                                        moreMenu = false; filesTarget = FilesTarget(key.machineId, cwd, cwd, FilesTarget.Mode.GIT)
                                    })
                                }
                                DropdownMenuItem({ Text("拷贝目录", maxLines = 1) }, {
                                    moreMenu = false
                                    jarvis.android.ui.media.MediaShare.copyText(context, cwd, "目录")
                                    Toasts.show("已拷贝", ToastStyle.SUCCESS)
                                })
                            }
                        }
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
                            verticalArrangement = Arrangement.spacedBy(WorkBridge * 2, Alignment.Bottom),
                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp),
                        ) {
                            items(lines, key = { it.key }, contentType = { contentType(it) }) { line ->
                                when (line) {
                                    is ChatLine.Pending -> PendingBubble(line.p, key)
                                    is ChatLine.Live -> Markdown(line.text, Modifier.fillMaxWidth().padding(vertical = 2.dp))
                                    is ChatLine.Working -> WorkingIndicator(line.label)
                                    is ChatLine.Queued -> Text(line.text, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                    is ChatLine.Line -> FoldedRowView(line.row, key, working, toggleWork, openTerminal)
                                    ChatLine.Older -> Box(Modifier.fillMaxWidth().padding(8.dp), contentAlignment = Alignment.Center) {
                                        CircularProgressIndicator(Modifier.size(20.dp))
                                    }
                                }
                            }
                        }
                    }
                    if (farFromEnd) {
                        Box(
                            Modifier.align(Alignment.BottomCenter).padding(12.dp).size(40.dp).clip(CircleShape)
                                .background(MaterialTheme.colorScheme.surfaceContainerHighest)
                                .clickable { follow = true; scope.launch { list.scrollToItem(0) } },
                            contentAlignment = Alignment.Center,
                        ) { Icon(Icons.Rounded.KeyboardArrowDown, contentDescription = "回到最新", tint = MaterialTheme.colorScheme.onSurface) }
                    }
                }
                // What the agent waits on, pinned above the input box (iOS InteractionPanel): each folds to one line.
                if (cards.isNotEmpty()) {
                    // While typing, the keyboard takes half the screen: the cards get less, so the conversation stays in view.
                    val share = if (androidx.compose.foundation.layout.WindowInsets.isImeVisible) 0.25f else 0.55f
                    val maxH = (androidx.compose.ui.platform.LocalConfiguration.current.screenHeightDp * share).dp
                    Column(
                        Modifier.fillMaxWidth().heightIn(max = maxH).verticalScroll(rememberScrollState()).padding(horizontal = 12.dp, vertical = 4.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) { cards.forEach { ix -> androidx.compose.runtime.key(ix.id) { InteractionCard(hub, ix) } } }
                }
                notice?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(horizontal = 16.dp)) }
                session?.suggestion?.takeIf { it.isNotBlank() && draft.isEmpty() }?.let { sug ->
                    AssistChip(onClick = { draft = sug }, label = { Text(sug, maxLines = 1, overflow = TextOverflow.Ellipsis) }, modifier = Modifier.padding(horizontal = 12.dp))
                }
                if (contexts.isNotEmpty()) Row(
                    Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp, vertical = 2.dp),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    contexts.forEach { ref -> ContextChip(ref) { Links.removeContext(key, ref) } }
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
                        IconButton({ attachMenu = true }, enabled = uploading == null) { Icon(Icons.Default.Add, "添加照片或文件") }
                        DropdownMenu(attachMenu, onDismissRequest = { attachMenu = false }) {
                            DropdownMenuItem({ Text("照片") }, {
                                attachMenu = false
                                pickPhotos.launch(androidx.activity.result.PickVisualMediaRequest(androidx.activity.result.contract.ActivityResultContracts.PickVisualMedia.ImageOnly))
                            }, enabled = images.size < jarvis.android.data.MAX_IMAGES)
                            DropdownMenuItem({ Text("文件（传到电脑上）") }, { attachMenu = false; pickFiles.launch(arrayOf("*/*")) })
                        }
                    }
                    OutlinedTextField(
                        draft, { draft = it },
                        placeholder = {
                            Text(
                                when {
                                    assistant -> "问 Jarvis…"
                                    session?.isLive == false -> "发消息会在电脑上恢复这个会话"
                                    else -> "发给 ${session?.agentLabel ?: "Claude"}"
                                },
                            )
                        },
                        modifier = Modifier.weight(1f).heightIn(max = 180.dp).focusRequester(focus), shape = RoundedCornerShape(20.dp),
                    )
                    IconButton(
                        onClick = { send() },
                        enabled = uploading == null && (draft.isNotBlank() || images.isNotEmpty() || contexts.isNotEmpty()),
                    ) { Icon(Icons.AutoMirrored.Filled.Send, "发送") }
                }
            }
        }
        MessageMenuHost(menu)
        menu.selecting?.let { text -> SelectTextDialog(text) { menu.selecting = null } }
    }

    // A send the computer refused: say so once (清空并发送 when the node can clear a one-line draft).
    outbox.firstOrNull { it.phase == PendingMessage.Phase.REFUSED && it.clientMsgId !in refusalsShown }?.let { r ->
        val clearable = r.canClearDraft
        val dialog = r.fields["code"] == "dialog_open"
        val done = { refusalsShown = refusalsShown + r.clientMsgId }
        AlertDialog(
            onDismissRequest = done,
            title = { Text(if (dialog) "电脑上有对话框" else "电脑的输入框里有没发出去的字") },
            text = {
                Text(
                    if (dialog) "先处理掉电脑上的对话框（或在终端里按键），再点消息旁的感叹号重发。什么都没有打进去。"
                    else "输入框里是：${r.fields["draft"] ?: "（读不到）"}\n什么都没有打进去。" + if (!clearable) "\n多行草稿只能在电脑上处理。" else "",
                )
            },
            confirmButton = {
                if (clearable) TextButton(onClick = { done(); Outbox.retry(hub, key, r.clientMsgId, clearDraft = true) }) { Text("清空并发送", maxLines = 1) }
                else if (openTerminal != null) TextButton(onClick = { done(); terminal = true }) { Text("打开终端", maxLines = 1) }
            },
            dismissButton = { TextButton(onClick = done) { Text("好", maxLines = 1) } },
        )
    }


    viewer?.let { args ->
        ImageViewerOverlay(hub, args, onClose = { viewer = null }, onEdited = { payload ->
            if (images.size >= jarvis.android.data.MAX_IMAGES) notice = "一次最多 ${jarvis.android.data.MAX_IMAGES} 张"
            else images = images + payload
        })
    }
}

/**
 * The viewer over the chat page, edge to edge (drawn on top of it, not in a Dialog: a dialog window stops at the
 * system bars). [onEdited] null: no 编辑.
 */
@Composable
private fun ImageViewerOverlay(hub: Hub, args: ImageViewerArgs, onClose: () -> Unit, onEdited: ((jarvis.android.data.ImagePayload) -> Unit)?) {
    // Light status / navigation bar icons on the black page (light mode would draw them dark), restored on close.
    val view = androidx.compose.ui.platform.LocalView.current
    DisposableEffect(view) {
        val window = (view.context as? android.app.Activity)?.window
        val bars = window?.let { androidx.core.view.WindowCompat.getInsetsController(it, view) }
        val status = bars?.isAppearanceLightStatusBars
        val nav = bars?.isAppearanceLightNavigationBars
        bars?.isAppearanceLightStatusBars = false
        bars?.isAppearanceLightNavigationBars = false
        onDispose {
            status?.let { bars.isAppearanceLightStatusBars = it }
            nav?.let { bars.isAppearanceLightNavigationBars = it }
        }
    }
    ImageViewerScreen(hub, args.copy(canEdit = args.canEdit && onEdited != null), onBack = onClose, onEdited = { onEdited?.invoke(it) })
}

/** An excerpt waiting for the next message: its title, a ✕ to drop it. */
@Composable
private fun ContextChip(ref: ContextRef, onRemove: () -> Unit) {
    Row(
        Modifier.clip(CircleShape).background(MaterialTheme.colorScheme.primary.copy(alpha = 0.1f)).padding(start = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Default.FormatQuote, null, Modifier.size(14.dp), tint = MaterialTheme.colorScheme.primary)
        Spacer(Modifier.size(4.dp))
        Text(ref.title, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, maxLines = 1,
            overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = 220.dp))
        IconButton(onClick = onRemove, modifier = Modifier.size(32.dp)) {
            Icon(Icons.Default.Close, "不带这段上下文", Modifier.size(14.dp), tint = MaterialTheme.colorScheme.primary)
        }
    }
}

private fun contentType(l: ChatLine): Any = when (l) {
    is ChatLine.Line -> when (val r = l.row) {
        is FoldedRow.Plain -> r.row::class
        is FoldedRow.Step -> "step:" + r.item::class.simpleName
        else -> r::class
    }
    else -> l::class
}

@Composable
private fun FoldedRowView(f: FoldedRow, key: SessionKey, working: Boolean, toggleWork: (String) -> Unit, onTerminal: (() -> Unit)?) {
    when (f) {
        is FoldedRow.Plain -> when (val r = f.row) {
            is ChatRow.User -> UserBubble(r.item)
            is ChatRow.Assistant -> AssistantText(r.item)
            is ChatRow.Thinking -> ThinkingRow(r.item)
            is ChatRow.Tools -> ToolGroup(r.steps, working)
            is ChatRow.Notice -> NoticeRow(r.item)
            is ChatRow.Recap -> RecapCard(r.item)
            is ChatRow.Image -> if (r.item.role == "user") SentPhotos(listOf(r.item), key) else ImageRow(r.item, key)
        }
        is FoldedRow.Work -> WorkLineRow(f, onToggle = { toggleWork(f.id) }, onTerminal = if (f.running) onTerminal else null)
        is FoldedRow.Step -> WorkStepRow(f, working)
        is FoldedRow.StepsEnd -> WorkEndRow(f.count, onCollapse = { toggleWork(f.workId) })
    }
}
