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
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Mic
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
import androidx.compose.material3.HorizontalDivider
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
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
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
import jarvis.android.ui.chat.ChatComposer
import jarvis.android.ui.chat.Drafts
import jarvis.android.ui.chat.ComposerActions
import jarvis.android.ui.chat.ShellComposer
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.GroupRow
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSheet
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.design.StatusDot
import jarvis.android.ui.design.surface
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.height
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
    data class Queued(val items: List<String>) : ChatLine { override val key get() = "queued" }
    data class Line(val row: FoldedRow) : ChatLine { override val key get() = row.key }
    /** An API error answer or notice (and its repeats), drawn as a card. */
    data class Error(val e: jarvis.android.ui.chat.ApiErrorLine, val latest: Boolean) : ChatLine { override val key get() = "e:" + e.row.key }
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
    /** The Jarvis assistant's conversation: headless (no terminal), its reply streamed as it is written. */
    assistant: Boolean = false,
    /** In the shell (the 助手 page): the shell draws the top bar over it; neutral bubbles and the shell's composer. */
    inShell: Boolean = false,
    /** 「交给 Jarvis」 on a message: the prompt for a new assistant conversation (null: not offered). */
    onHandToJarvis: ((String) -> Unit)? = null,
    /** Put the cursor in the composer once (the 「问 Jarvis」 shortcut), then [onFocusTaken]. */
    focusComposer: Boolean = false, onFocusTaken: () -> Unit = {},
    /** /clear made a new session in the same pane: open it (null: stay; the list shows it). */
    onCleared: ((SessionKey) -> Unit)? = null,
    /** The shell composer's voice button: the hub's voice conversation (语音对话). */
    onVoice: (() -> Unit)? = null,
    /** Every work line open from the start (screenshots, iOS `-openWork YES`). */
    openWork: Boolean = false,
    /** False in a wide window's right pane: the list is beside it (iOS: a split view's detail has no back). */
    backButton: Boolean = true,
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
    // Something new to answer while the chat is open: a warning tap (iOS's haptics: new interaction = warning).
    val hapticsView = jarvis.android.ui.design.hapticView()
    var seenCards by remember(key) { mutableStateOf(cards.map { it.id }.toSet()) }
    LaunchedEffect(cards.map { it.id }) {
        val ids = cards.map { it.id }.toSet()
        if ((ids - seenCards).isNotEmpty()) jarvis.android.ui.design.Haptics.warning(hapticsView)
        seenCards = ids
    }
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
    var expandedWork by remember(key) {
        mutableStateOf(if (openWork) foldWork(rows, false, emptySet()).filterIsInstance<FoldedRow.Work>().map { it.id }.toSet() else setOf())
    }
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
            session?.queued?.takeIf { it.isNotEmpty() }?.let { add(ChatLine.Queued(it)) }
            val items = jarvis.android.ui.chat.foldApiErrors(folded)
            for (i in items.indices.reversed()) when (val it = items[i]) {
                is jarvis.android.ui.chat.ChatItem.Row -> add(ChatLine.Line(it.row))
                is jarvis.android.ui.chat.ChatItem.Error -> add(ChatLine.Error(it.error, latest = i == items.lastIndex))
            }
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

    // Kept outside the page (Drafts): folding the phone moves the chat to another layout, and the words stay.
    var draft by Drafts.of(key.path)
    // Refusals already explained (the dialog shows once per message; the bubble keeps its red mark).
    var refusalsShown by remember(key) { mutableStateOf(setOf<String>()) }
    var notice by remember { mutableStateOf<String?>(null) }
    var terminal by remember { mutableStateOf(false) }
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
    // 拍照 (iOS CameraPicker): the system camera writes into cache/camera through our FileProvider; the photo then
    // goes in like a picked one (scaled, JPEG). The app declares CAMERA (pairing scans a QR code), so Android wants it
    // granted before another app's camera may be started for us.
    var shot by remember { mutableStateOf<android.net.Uri?>(null) }
    val takePhoto = androidx.activity.compose.rememberLauncherForActivityResult(
        androidx.activity.result.contract.ActivityResultContracts.TakePicture(),
    ) { ok ->
        val uri = shot ?: return@rememberLauncherForActivityResult
        shot = null
        if (!ok) return@rememberLauncherForActivityResult
        scope.launch {
            val p = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.Default) { jarvis.android.data.imagePayload(context, uri) }
            if (p == null) notice = "这张照片读不出来" else images = images + p
            runCatching { context.contentResolver.delete(uri, null, null) }
        }
    }
    fun openCamera() {
        val dir = java.io.File(context.cacheDir, "camera").apply { mkdirs() }
        dir.listFiles()?.forEach { it.delete() } // one shot at a time; what is left is from a cancelled one
        val f = java.io.File(dir, "photo-${System.currentTimeMillis()}.jpg")
        val uri = androidx.core.content.FileProvider.getUriForFile(context, context.packageName + ".chatfiles", f)
        shot = uri
        runCatching { takePhoto.launch(uri) }.onFailure { shot = null; notice = "这台手机上没有能拍照的应用" }
    }
    val askCamera = androidx.activity.compose.rememberLauncherForActivityResult(
        androidx.activity.result.contract.ActivityResultContracts.RequestPermission(),
    ) { granted -> if (granted) openCamera() else notice = "没有相机权限，可以到系统设置 › 应用 › Jarvis 打开" }
    // 语音输入 (iOS Dictation): what the phone's recognizer heard goes at the end of the box, to edit before sending.
    val dictate = jarvis.android.ui.chat.rememberDictation(
        onHeard = { heard -> draft = jarvis.android.ui.chat.withDictation(draft, heard) }, onError = { notice = it },
    )
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
    // /clear only for Claude in a herdr pane (iOS canClear): not Jarvis, not Codex / TRAEX.
    val canClear = !assistant && session?.canSend == true && session.canTerminal && session.paneId != null && !session.isCodex && (session.agent == null || session.agent == "claude")
    // An agent runtime's session may be read-only here (DSH…): nothing goes in, the composer says why.
    val writable = session?.canSend == true
    var confirmClear by remember { mutableStateOf(false) }
    var clearing by remember { mutableStateOf(false) }
    if (confirmClear) androidx.compose.material3.AlertDialog(
        onDismissRequest = { confirmClear = false },
        title = { Text("清空上下文？") },
        text = { Text("${session?.agentLabel ?: "Claude"} 会开一个新会话，这段对话留在历史里，页面会跳到新会话。") },
        confirmButton = {
            TextButton({
                confirmClear = false; clearing = true
                scope.launch {
                    val next = hub.clearContext(key)
                    clearing = false
                    if (next != null) onCleared?.invoke(next) else Toasts.show("已发送 /clear，新会话出现后在列表里打开")
                }
            }) { Text("清空", color = JTheme.colors.danger, maxLines = 1) }
        },
        dismissButton = { TextButton({ confirmClear = false }) { Text("取消", maxLines = 1) } },
    )

    fun send() {
        if (hub.session(key)?.canSend != true) return
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
    var closing by remember(key) { mutableStateOf(false) } // 关闭 Agent… from the ⋯ menu (iOS ChatView)
    if (closing && session != null) CloseAgentDialog(
        hub, session, machine?.displayName, onDismiss = { closing = false },
        onError = { Toasts.show(it, ToastStyle.ERROR) },
    )
    LaunchedEffect(env.cwd, machine != null) {
        val cwd = env.cwd ?: return@LaunchedEffect
        if (machine == null || assistant) return@LaunchedEffect
        gitCount = runCatching { hub.api.gitStatus(key.machineId, cwd) }.getOrNull()?.takeIf { it.isRepo }?.files?.size
    }
    val openTerminal: (() -> Unit)? = if (session?.isLive == true && session.canTerminal && !assistant) ({ terminal = true }) else null

    if (terminal && session?.canTerminal == true) {
        jarvis.android.terminal.TerminalScreen(hub, key, session?.displayTitle ?: key.sessionId.take(8), onClose = { terminal = false })
        return
    }

    filesTarget?.let { t ->
        // Over the chat, as a pushed page would be; the image viewer (below) can still open on top of it.
        FilesScreen(hub, t, onBack = { filesTarget = null }, onOpenImage = { viewer = it })
        viewer?.let { ImageViewerOverlay(hub, it, onClose = { viewer = null }, onEdited = null) }
        return
    }

    var info by remember { mutableStateOf(false) }
    val canSend = writable && uploading == null && (draft.isNotBlank() || images.isNotEmpty() || contexts.isNotEmpty())
    val interrupt: (() -> Unit)? = if (working && session?.canInterrupt == true) ({ scope.launch { runCatching { hub.api.interrupt(key) }.onFailure { notice = it.message } } }) else null
    val placeholder = when {
        !writable && session?.hasSource == true -> "此会话仅可查看"
        assistant -> "问 Jarvis"
        session?.isLive == false -> "发消息会在电脑上恢复这个会话"
        working -> "补充说明（会排队发送）"
        else -> "发给 ${session?.agentLabel ?: "Claude"}"
    }
    val attachItems: @Composable androidx.compose.foundation.layout.ColumnScope.(() -> Unit) -> Unit = { close ->
        DropdownMenuItem({ Text("照片", maxLines = 1) }, {
            close()
            pickPhotos.launch(androidx.activity.result.PickVisualMediaRequest(androidx.activity.result.contract.ActivityResultContracts.PickVisualMedia.ImageOnly))
        }, enabled = images.size < jarvis.android.data.MAX_IMAGES, leadingIcon = { Icon(JIcons.photo, null) })
        DropdownMenuItem({ Text("拍照", maxLines = 1) }, {
            close()
            if (androidx.core.content.ContextCompat.checkSelfPermission(context, android.Manifest.permission.CAMERA) ==
                android.content.pm.PackageManager.PERMISSION_GRANTED
            ) openCamera() else askCamera.launch(android.Manifest.permission.CAMERA)
        }, enabled = images.size < jarvis.android.data.MAX_IMAGES, leadingIcon = { Icon(JIcons.camera, null) })
        if (!assistant) DropdownMenuItem({ Text("文件（传到电脑上）", maxLines = 1) }, { close(); pickFiles.launch(arrayOf("*/*")) }, leadingIcon = { Icon(JIcons.attach, null) })
        // Jarvis' quick messages go out at once; /clear asks first.
        HorizontalDivider()
        jarvis.android.ui.chat.SlashCommand.all.forEach { cmd ->
            DropdownMenuItem({ Text(cmd.title, maxLines = 1) }, { close(); Outbox.send(hub, key, cmd.text) }, leadingIcon = { Icon(JIcons.send, null) })
        }
        if (canClear) DropdownMenuItem(
            { Text("清空上下文 /clear", color = JTheme.colors.danger, maxLines = 1) },
            { close(); confirmClear = true }, enabled = !working && !clearing, leadingIcon = { Icon(JIcons.clear, null, tint = JTheme.colors.danger) },
        )
    }
    val top = if (inShell) WindowInsets.statusBars.asPaddingValues().calculateTopPadding() + JSize.shellBar else 0.dp

    CompositionLocalProvider(LocalChatEnv provides env, LocalLinkSource provides key, LocalShellChat provides inShell) {
        // A wide window keeps the conversation to a readable column, centered (iOS `ChatView.readableWidth`).
        Column(Modifier.fillMaxSize().background(JTheme.colors.page).imePadding().wrapContentWidth(Alignment.CenterHorizontally).widthIn(max = JSize.readable)) {
            if (!inShell) ChatTopBar(
                title = title, session = session, machine = machine?.displayName, assistant = assistant,
                onBack = onBack.takeIf { backButton }, onTitle = { info = true },
            ) { close ->
                if (openTerminal != null) DropdownMenuItem({ Text("终端", maxLines = 1) }, { close(); openTerminal() }, leadingIcon = { Icon(JIcons.terminal, null) })
                val cwd = env.cwd
                if (!assistant && cwd != null && machine != null) {
                    DropdownMenuItem({ Text("文件", maxLines = 1) }, {
                        close(); filesTarget = FilesTarget(key.machineId, cwd, cwd, FilesTarget.Mode.DIR)
                    }, leadingIcon = { Icon(JIcons.folder, null) })
                    gitCount?.let { n ->
                        DropdownMenuItem({ Text(if (n > 0) "Git（$n）" else "Git", maxLines = 1) }, {
                            close(); filesTarget = FilesTarget(key.machineId, cwd, cwd, FilesTarget.Mode.GIT)
                        }, leadingIcon = { Icon(JIcons.agents, null) })
                    }
                }
                if (!assistant && cwd != null) DropdownMenuItem({ Text("拷贝目录", maxLines = 1) }, {
                    close()
                    jarvis.android.ui.media.MediaShare.copyText(context, cwd, "目录")
                    Toasts.show("已拷贝", ToastStyle.SUCCESS)
                }, leadingIcon = { Icon(JIcons.copy, null) })
                DropdownMenuItem({ Text("会话信息", maxLines = 1) }, { close(); info = true }, leadingIcon = { Icon(JIcons.info, null) })
                // A session an agent runtime holds read-only can be taken over live (iOS 实时接入).
                if (session?.canAttach == true) DropdownMenuItem({ Text("实时接入", maxLines = 1) }, {
                    close()
                    scope.launch {
                        runCatching { hub.api.attach(key); hub.refresh() }
                            .onSuccess { Toasts.show("已实时接入", ToastStyle.SUCCESS) }
                            .onFailure { notice = "接入失败：${it.message}" }
                    }
                }, leadingIcon = { Icon(JIcons.terminal, null) })
                if (!assistant && session?.isLive == true && session.canTerminal) {
                    HorizontalDivider()
                    DropdownMenuItem({ Text("关闭 Agent…", color = JTheme.colors.danger, maxLines = 1) }, { close(); closing = true },
                        leadingIcon = { Icon(JIcons.close, null, tint = JTheme.colors.danger) })
                }
            }
            Box(Modifier.weight(1f)) {
                if (state.loading && rows.isEmpty()) {
                    CircularProgressIndicator(Modifier.align(Alignment.Center).size(JSize.icon), strokeWidth = JSize.rule, color = JTheme.colors.secondaryText)
                } else {
                    LazyColumn(
                        state = list, reverseLayout = true, modifier = Modifier.fillMaxSize(),
                        verticalArrangement = Arrangement.spacedBy(WorkBridge * 2, Alignment.Bottom),
                        contentPadding = PaddingValues(start = JSpace.page, end = JSpace.page, top = top + JSpace.s, bottom = JSpace.s),
                    ) {
                        items(lines, key = { it.key }, contentType = { contentType(it) }) { line ->
                            when (line) {
                                is ChatLine.Pending -> PendingBubble(line.p, key)
                                is ChatLine.Live -> Markdown(line.text, Modifier.fillMaxWidth().padding(vertical = JSpace.xxs))
                                is ChatLine.Working -> WorkingIndicator(line.label)
                                is ChatLine.Queued -> QueueStrip(line.items) { i, delete ->
                                    scope.launch {
                                        // Pull everything back and re-queue the rest, as iOS (session.queue.edit).
                                        runCatching { hub.api.editQueue(key, line.items.indices.filter { it != i }) }
                                            .onSuccess { pulled ->
                                                if (!delete && i in pulled.indices) draft = if (draft.isEmpty()) pulled[i] else draft + "\n" + pulled[i]
                                                Toasts.show(if (delete) "已从队列删除" else "已撤回到输入框，改好再发送", ToastStyle.SUCCESS)
                                            }
                                            .onFailure { notice = "没改成：${it.message}" }
                                    }
                                }
                                is ChatLine.Line -> FoldedRowView(line.row, key, working, toggleWork, openTerminal)
                                // 重试 only when the error is the last thing, the agent is not working (someone may have
                                // retried already) and trying again can help (not a login or a bad request), as iOS.
                                is ChatLine.Error -> jarvis.android.ui.chat.ApiErrorCard(
                                    line.e,
                                    retry = if (line.latest && !working && line.e.info.transient != false) {
                                        { Outbox.send(hub, key, "继续") }
                                    } else null,
                                )
                                ChatLine.Older -> Box(Modifier.fillMaxWidth().padding(JSpace.s), contentAlignment = Alignment.Center) {
                                    CircularProgressIndicator(Modifier.size(JSize.iconXS), strokeWidth = JSize.rule, color = JTheme.colors.secondaryText)
                                }
                            }
                        }
                    }
                }
                androidx.compose.animation.AnimatedVisibility(
                    farFromEnd, Modifier.align(Alignment.BottomCenter).padding(bottom = JSpace.m),
                    enter = androidx.compose.animation.fadeIn() + androidx.compose.animation.scaleIn(), exit = androidx.compose.animation.fadeOut() + androidx.compose.animation.scaleOut(),
                ) {
                    CircleButton(JIcons.expand, "回到最新", { follow = true; scope.launch { list.scrollToItem(0) } }, size = JSize.wellXL)
                }
            }
            // What the agent waits on, pinned above the input box (iOS InteractionPanel): each folds to one line.
            if (cards.isNotEmpty()) {
                // While typing, the keyboard takes half the screen: the cards get less, so the conversation stays in view.
                val share = if (androidx.compose.foundation.layout.WindowInsets.isImeVisible) 0.25f else 0.55f
                val maxH = (androidx.compose.ui.platform.LocalConfiguration.current.screenHeightDp * share).dp
                Column(
                    Modifier.fillMaxWidth().heightIn(max = maxH).verticalScroll(rememberScrollState()).padding(horizontal = JSpace.m, vertical = JSpace.xs),
                    verticalArrangement = Arrangement.spacedBy(JSpace.s),
                ) { cards.forEach { ix -> androidx.compose.runtime.key(ix.id) { InteractionCard(hub, ix) } } }
            }
            Column(
                Modifier.fillMaxWidth().navigationBarsPadding().padding(horizontal = JSpace.m).padding(top = JSpace.xs, bottom = JSpace.s),
                verticalArrangement = Arrangement.spacedBy(JSpace.s),
            ) {
                notice?.let {
                    Row(Modifier.padding(horizontal = JSpace.xs), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        Icon(JIcons.error, null, Modifier.size(JSize.markS), tint = JTheme.colors.danger)
                        Text(it, color = JTheme.colors.danger, style = JTheme.type.caption, maxLines = 3)
                    }
                }
                session?.takeIf { it.starting == true || it.startError != null }?.let { StartingNote(it.startError, it.agentLabel) }
                // Why nothing can be typed: the runtime's own words for where the session lives.
                if (session?.hasSource == true && !writable) Text(
                    session.sourceSubtitle.orEmpty(), style = JTheme.type.caption, color = JTheme.colors.secondaryText,
                    modifier = Modifier.padding(horizontal = JSpace.xs),
                )
                if (!inShell) session?.takeIf { it.canSend && !it.isCodex && (it.agent == null || it.agent == "claude") }?.let {
                    // Photos and quotes in the box stay for the message: /model goes out on its own.
                    SessionModeBar(hub, it, onPickModel = { id -> Outbox.send(hub, key, "/model $id") })
                }
                session?.suggestion?.takeIf { it.isNotBlank() && draft.isEmpty() && !working }?.let { sug -> SuggestionPill(sug) { draft = sug } }
                if (contexts.isNotEmpty()) Row(
                    Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
                ) {
                    contexts.forEach { ref -> ContextChip(ref) { Links.removeContext(key, ref) } }
                }
                if (images.isNotEmpty()) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    images.forEachIndexed { i, img ->
                        Box {
                            val bmp = remember(img.data) {
                                runCatching { android.util.Base64.decode(img.data, android.util.Base64.DEFAULT).let { BitmapFactory.decodeByteArray(it, 0, it.size) } }.getOrNull()
                            }
                            bmp?.let { Image(it.asImageBitmap(), "第 ${i + 1} 张", Modifier.size(JSize.box64).clip(RoundedCornerShape(JRadius.control)), contentScale = androidx.compose.ui.layout.ContentScale.Crop) }
                            Box(
                                Modifier.align(Alignment.TopEnd).padding(JSpace.xxs).size(JSize.iconXS).clip(CircleShape)
                                    .background(androidx.compose.ui.graphics.Color.Black.copy(alpha = 0.55f)).clickable { images = images.filterIndexed { j, _ -> j != i } },
                                contentAlignment = Alignment.Center,
                            ) { Icon(JIcons.close, "移除这张", Modifier.size(JSize.markXS), tint = androidx.compose.ui.graphics.Color.White) }
                        }
                    }
                }
                uploading?.let { name ->
                    Row(Modifier.padding(horizontal = JSpace.xs), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                        CircularProgressIndicator(Modifier.size(JSize.markS), strokeWidth = JSize.rule, color = JTheme.colors.secondaryText)
                        Text("正在传「$name」到电脑上…", style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 1)
                    }
                }
                // Typed "/": the quick messages over the box; picking one puts its message in (/clear asks first).
                jarvis.android.ui.chat.SlashCommand.matching(draft, canClear && !working && !clearing).takeIf { it.isNotEmpty() }?.let { cmds ->
                    jarvis.android.ui.chat.SlashPicker(cmds) { cmd ->
                        if (cmd.word == jarvis.android.ui.chat.SlashCommand.CLEAR) { draft = ""; confirmClear = true } else draft = cmd.text
                    }
                }
                if (inShell) {
                    ShellComposer(
                        draft, { draft = it }, placeholder, canSend = canSend,
                        actions = ComposerActions(
                            send = { send() }, stop = interrupt, attach = attachItems.takeIf { writable }, voice = onVoice, dictate = dictate.takeIf { writable },
                            modes = session?.takeIf { it.agent == null || it.agent == "claude" }?.let { s -> { close -> ModeMenuItems(hub, s, close) } },
                        ),
                        focus = focus, hasAttachments = images.isNotEmpty() || contexts.isNotEmpty(),
                        modesWarn = session?.permissionMode == "bypassPermissions",
                    )
                } else {
                    ChatComposer(
                        draft, { draft = it }, placeholder, canSend = canSend,
                        actions = ComposerActions(send = { send() }, stop = interrupt, attach = attachItems.takeIf { writable }, dictate = dictate.takeIf { writable }),
                        focus = focus, enabled = writable,
                    )
                }
            }
        }
        MessageMenuHost(menu)
        menu.selecting?.let { text -> SelectTextDialog(text) { menu.selecting = null } }
        if (info) SessionInfoSheet(hub, key, session, machine?.displayName, onDismiss = { info = false })
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
        Modifier.height(JSize.wellM).surface(CircleShape, floating = true).padding(start = JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(JIcons.text, null, Modifier.size(JSize.markS), tint = JTheme.colors.accent)
        Spacer(Modifier.size(JSpace.xs))
        Text(ref.title, style = JTheme.type.labelMedium, color = JTheme.colors.label, maxLines = 1,
            overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = JSize.box220))
        Box(Modifier.size(JSize.wellM).clip(CircleShape).clickable(onClick = onRemove), contentAlignment = Alignment.Center) {
            Icon(JIcons.close, "不带这段上下文", Modifier.size(JSize.markS), tint = JTheme.colors.secondaryText)
        }
    }
}

/** Claude's suggested next message (iOS `SuggestionPill`): ✦ in the AI's color, the words, 采用 fills the input box. */
@Composable
private fun SuggestionPill(text: String, onUse: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.wellL).surface(CircleShape, floating = true).clickable(onClick = onUse).padding(horizontal = JSpace.m),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        Icon(JIcons.ai, null, Modifier.size(JSize.markS), tint = JTheme.colors.ai)
        Text("建议", style = JTheme.type.labelStrong, color = JTheme.colors.ai, maxLines = 1)
        Text(text, style = JTheme.type.secondary, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        Text("采用", style = JTheme.type.secondaryStrong, color = JTheme.colors.accent, maxLines = 1)
    }
}

/**
 * The Agents chat's top bar (iOS `ChromeHeader`): a round back button, the title as a capsule in the middle — the
 * state's dot, 「space › tab」 or the title, and the state in words when it is one to act on — and ⋯ (终端, 文件,
 * Git, 拷贝目录, 会话信息). A tap on the capsule opens 会话信息. While the hub is out of reach the capsule says so.
 */
@Composable
private fun ChatTopBar(
    title: String, session: jarvis.android.data.Session?, machine: String?, assistant: Boolean,
    onBack: (() -> Unit)?, onTitle: () -> Unit, menu: @Composable androidx.compose.foundation.layout.ColumnScope.(() -> Unit) -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    val link by ConnectionWatch.label.collectAsState()
    Box(Modifier.fillMaxWidth().statusBarsPadding().height(JSize.shellBar).padding(horizontal = JSpace.page)) {
        if (onBack != null) CircleButton(JIcons.back, "返回", onBack, Modifier.align(Alignment.CenterStart))
        Row(
            Modifier.align(Alignment.Center).padding(horizontal = JSize.tapTarget + JSpace.s).height(JSize.wellL)
                .surface(CircleShape, floating = true).clickable(onClick = onTitle).padding(horizontal = JSpace.m),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
        ) {
            val l = link
            if (l != null) {
                if (l != "需要重新配对") CircularProgressIndicator(Modifier.size(JSize.markXS), strokeWidth = JSize.lineMedium, color = JTheme.colors.secondaryText)
                Text(l, style = JTheme.type.secondaryMedium, color = JTheme.colors.secondaryText, maxLines = 1)
            } else {
                session?.let { StatusDot(it.agentState, JSize.dot) }
                val place = session?.let { s ->
                    listOfNotNull(s.spaceName?.takeIf { it.isNotBlank() }, s.slotName?.takeIf { it.isNotBlank() }?.let { if (it.all(Char::isDigit)) "#$it" else it })
                        .joinToString(" › ").ifEmpty { null }
                }
                Text(if (assistant) title else place ?: title, style = JTheme.type.secondaryStrong, color = JTheme.colors.label, maxLines = 1,
                    overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                val st = session?.agentState
                if (st == AgentState.NeedsYou || st == AgentState.Working) Text(st.label(), style = JTheme.type.caption, color = JTheme.tone(st).text, maxLines = 1)
            }
        }
        Box(Modifier.align(Alignment.CenterEnd)) {
            CircleButton(JIcons.more, "更多", { open = true })
            DropdownMenu(open, onDismissRequest = { open = false }) { menu { open = false } }
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

/** A session Jarvis is still starting (node/starting.go): what is sent now goes in once it is ready. Or why it did not start. */
@Composable
internal fun StartingNote(error: String?, agent: String) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth()
            .background(if (error == null) c.fillWell else c.danger.copy(alpha = 0.1f), RoundedCornerShape(JRadius.card))
            .padding(horizontal = JSpace.m, vertical = JSpace.s)
            .then(Modifier.clearAndSetSemantics { contentDescription = if (error == null) "正在启动 $agent" else "没有启动起来：$error" }),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        if (error == null) CircularProgressIndicator(Modifier.size(JSize.markS), strokeWidth = JSize.rule, color = c.secondaryText)
        else Icon(JIcons.error, null, Modifier.size(JSize.markS), tint = c.danger)
        Text(
            if (error == null) "正在启动 $agent，消息会在它就绪后发出" else "没有启动起来：$error",
            style = JTheme.type.caption, color = if (error == null) c.secondaryText else c.danger, maxLines = 3,
        )
    }
}

/**
 * Messages typed while Claude works and not taken yet (iOS QueueStrip): numbered, each with 撤回编辑 (back into the
 * box), 拷贝 and 删除. Editing goes through session.queue.edit, which pulls the queue out of claude and puts the rest back.
 */
@Composable
internal fun QueueStrip(items: List<String>, onAct: (index: Int, delete: Boolean) -> Unit) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val c = JTheme.colors
    Column(
        Modifier.fillMaxWidth().padding(vertical = JSpace.xs).clip(RoundedCornerShape(JRadius.card)).background(c.fillWell).padding(JSpace.m),
        verticalArrangement = Arrangement.spacedBy(JSpace.sm),
    ) {
        Text("排队中 ${items.size} 条 · Claude 忙完会依次处理", style = JTheme.type.caption, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
        items.forEachIndexed { i, q ->
            var open by remember { mutableStateOf(false) }
            Box {
                Row(
                    Modifier.fillMaxWidth().surface(RoundedCornerShape(JRadius.control)).clickable { open = true }
                        .padding(horizontal = JSpace.m, vertical = JSpace.s),
                    verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(JSpace.s),
                ) {
                    Text("${i + 1}", style = JTheme.type.labelStrong, color = c.accent, maxLines = 1)
                    Text(q, style = JTheme.type.secondary, color = c.label, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                }
                DropdownMenu(open, onDismissRequest = { open = false }) {
                    DropdownMenuItem({ Text("撤回编辑", maxLines = 1) }, { open = false; onAct(i, false) }, leadingIcon = { Icon(JIcons.edit, null) })
                    DropdownMenuItem({ Text("拷贝", maxLines = 1) }, {
                        open = false; jarvis.android.ui.media.MediaShare.copyText(ctx, q, "消息"); Toasts.show("已拷贝", ToastStyle.SUCCESS)
                    }, leadingIcon = { Icon(JIcons.copy, null) })
                    DropdownMenuItem({ Text("删除", color = c.danger, maxLines = 1) }, { open = false; onAct(i, true) },
                        leadingIcon = { Icon(JIcons.delete, null, tint = c.danger) })
                }
            }
        }
    }
}
