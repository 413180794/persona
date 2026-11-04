package persona.feature.chat

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.compose.currentStateAsState
import persona.core.chatlist.*
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.lines.ChatLine
import persona.core.domain.sort.READ_ONLY
import persona.core.domain.sort.canApprove
import persona.core.domain.sort.canChangeModes
import persona.core.domain.sort.canInterrupt
import persona.core.domain.sort.canSend
import persona.core.domain.sort.canTerminal
import persona.core.domain.sort.canTypeClaudeCommands
import persona.feature.chat.media.ChatImageViewer
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.internal.GeneratedComponentManager
import androidx.compose.ui.platform.LocalContext
import java.util.UUID
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The shell supplies repositories and optional terminal/navigation affordances, never wire clients. */
@OptIn(ExperimentalMaterial3Api::class, FlowPreview::class)
@Composable
fun ChatScreen(
    sessionKey: SessionKey, repository: ChatRepository, sender: MessageSender,
    interactions: InteractionCenter, sessions: SessionsRepository, onBack: () -> Unit,
    onTerminal: ((SessionKey) -> Unit)? = null,
    chrome: ChatChromeBindings = ChatChromeBindings(),
    pendingBadge: @Composable () -> Unit = { PendingBadge(PendingStatus.NeedsReview) },
    readingKey: ChatReadingKey? = null,
    anchorStore: ChatAnchorStore? = null,
    renderStyle: ChatRenderStyle = ChatRenderStyle.Agent,
    textUpdates: Map<String, ChatTextUpdate> = emptyMap(),
    composerService: ChatComposerService? = null,
    composerState: ChatComposerState? = null,
    onDraftChanged: ((ChatComposerDraft) -> Unit)? = null,
    onContextCleared: ((SessionKey) -> Unit)? = null,
    statusBindings: ChatStatusBindings = ChatStatusBindings(),
    embedded: Boolean = false,
    showTopBar: Boolean = true,
    historyReadOnly: Boolean = false,
    historyFooter: (@Composable () -> Unit)? = null,
) {
    PersonaTheme(darkTheme = MaterialTheme.colorScheme.background.luminance() < 0.5f) {
        val storage = repository as? ChatLocalStorage
        val local by (storage?.localStorage ?: remember { kotlinx.coroutines.flow.MutableStateFlow<ChatStorageSnapshot?>(null) })
            .collectAsStateWithLifecycle()
        val scopedReadingKey = readingKey ?: local?.let { ChatReadingKey(it.scope, sessionKey.path) }
        key(sessionKey, scopedReadingKey) {
            val pageSender = remember(sender, historyReadOnly) { if (historyReadOnly) ReadOnlyMessageSender(sender) else sender }
            ChatPage(sessionKey, repository, pageSender, interactions, sessions, onBack, onTerminal, chrome, scopedReadingKey, anchorStore ?: local?.anchors,
                renderStyle, textUpdates,
                composerService, composerState ?: rememberChatComposerState(), onDraftChanged, onContextCleared, statusBindings,
                embedded, showTopBar, historyReadOnly, historyFooter)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class, FlowPreview::class)
@Composable
private fun ChatPage(
    key: SessionKey, repository: ChatRepository, sender: MessageSender,
    interactions: InteractionCenter, sessions: SessionsRepository, onBack: () -> Unit,
    onTerminal: ((SessionKey) -> Unit)?, chrome: ChatChromeBindings,
    readingKey: ChatReadingKey?, anchorStore: ChatAnchorStore?,
    renderStyle: ChatRenderStyle, textUpdates: Map<String, ChatTextUpdate>,
    composerService: ChatComposerService?, composer: ChatComposerState,
    onDraftChanged: ((ChatComposerDraft) -> Unit)?, onContextCleared: ((SessionKey) -> Unit)?,
    statusBindings: ChatStatusBindings,
    embedded: Boolean, showTopBar: Boolean,
    historyReadOnly: Boolean, historyFooter: (@Composable () -> Unit)?,
) {
    val scope = rememberCoroutineScope()
    val recovery = remember(key, statusBindings.recovery) { ChatStatusRecovery(statusBindings.recovery, scope, sessions::refresh) }
    val source = remember(repository, key) { repository.observe(key) }
    val snapshot by source.collectAsStateWithLifecycle(ChatSnapshot())
    val liveTextUpdates = remember(snapshot.items, snapshot.streamingItemIds, textUpdates) {
        snapshot.items.filter { it.id in snapshot.streamingItemIds }.associate {
            it.id to ChatTextUpdate(it.text.orEmpty(), streaming = true)
        } + textUpdates
    }
    val application = LocalContext.current.applicationContext
    val renamer = chrome.renamer ?: remember(application) {
        if (application is GeneratedComponentManager<*>)
            EntryPointAccessors.fromApplication(application, ChatRenameServices::class.java).sessionRenamer()
        else null
    }
    val sessionSource = remember(renamer, sessions) { renamer?.project(sessions.sessions) ?: sessions.sessions }
    // Read once per source, not on every recomposition: only the first value seeds the collected state.
    val initialSessions = remember(sessionSource) { sessions.sessions.value }
    val sessionList by sessionSource.collectAsStateWithLifecycle(initialSessions)
    val machines by sessions.machines.collectAsStateWithLifecycle()
    val allRequests by interactions.pending.collectAsStateWithLifecycle()
    val allMessages by sender.outbox.collectAsStateWithLifecycle()
    val parent = sessionList.firstOrNull { it.machineId == key.machineId && it.subagents.orEmpty().any { child -> child.sessionId == key.sessionId } }
    val child = parent?.subagents?.firstOrNull { it.sessionId == key.sessionId }
    val readOnly = historyReadOnly || child != null || key.sessionId.contains('@')
    var lastSession by remember { mutableStateOf<Session?>(null) }
    val session = sessionList.firstOrNull { it.key == key } ?: child?.let {
        parent?.copy(sessionId = it.sessionId, title = it.description?.takeIf(String::isNotBlank) ?: it.type,
            status = if (it.running) "working" else "done", state = if (it.running) "working" else "unread", subagents = emptyList(),
            queued = null, loop = null, background = null, pendingInteractions = 0)
    } ?: lastSession
    SideEffect { if (session != null) lastSession = session }
    // What the session's source lets the phone do (docs/protocol.md「Codex 来源与能力」). Older sessions, and one whose
    // metadata has not arrived, can do everything: the hub and the node refuse what is not allowed anyway.
    val canSend = session?.canSend ?: true
    val canApprove = session?.canApprove ?: true
    val canTerminal = session?.canTerminal ?: true
    val connection = chrome.connection?.state?.collectAsStateWithLifecycle()?.value
    val controls = remember(key, chrome.actions, renamer) { ChatChromeState(chrome.actions, scope, sessions::refresh, onBack, renamer) }
    var headerOverlay by remember { mutableStateOf(false) }
    var modeOverlay by remember { mutableStateOf(false) }
    var composerOverlay by remember { mutableStateOf(false) }
    val chromeOverlay = headerOverlay || modeOverlay || composerOverlay
    var requestCollapsed by rememberSaveable { mutableStateOf(false) }
    val machine = machines.firstOrNull { it.id == key.machineId }
    var resolved by remember { mutableStateOf(emptySet<String>()) }
    var interactionFeedback by remember { mutableStateOf<InteractionSubmission?>(null) }
    var uncertain by remember { mutableStateOf(emptySet<String>()) }
    var busyId by remember { mutableStateOf<String?>(null) }
    // The assistant's tasks (docs/unified-entry.md): their agents' questions are answered here too, oldest first.
    val tasks = chrome.tasks
    val taskTodos by (tasks?.todos ?: remember { kotlinx.coroutines.flow.MutableStateFlow(emptyList<persona.core.contracts.Todo>()) })
        .collectAsStateWithLifecycle()
    val taskKeys = remember(taskTodos, key) {
        taskTodos.filter { it.origin == key.sessionId && it.doneAt == null }
            .flatMap { t -> t.activeAgents.map { SessionKey(it.machineId, it.sessionId) } }.toSet()
    }
    val requests = allRequests.filter { (it.sessionKey == key || it.sessionKey in taskKeys) && it.id !in resolved }
        .sortedBy { if (it.id == chrome.requestId) 0 else 1 }
    val messages = allMessages.filter {
        it.session == key && snapshot.items.none { item -> item.id == it.clientMessageId || MessageEcho.tag(item) == it.clientMessageId }
    }
    val currentMessageCount by rememberUpdatedState(messages.size)
    val composerDeliveries by (composerService?.deliveries ?: remember {
        kotlinx.coroutines.flow.MutableStateFlow(emptyMap<String, ComposerDelivery>())
    }).collectAsStateWithLifecycle()
    var submittedDraft by remember { mutableStateOf<Pair<String, ChatComposerDraft>?>(null) }
    var clearingPane by rememberSaveable { mutableStateOf<String?>(null) }
    var linkContexts by rememberSaveable(stateSaver = ChatLinkContextsSaver) { mutableStateOf(emptyList<ChatLinkContext>()) }
    var submittedContexts by remember { mutableStateOf(emptyList<ChatLinkContext>()) }
    var notice by remember { mutableStateOf<String?>(null) }
    val deliveryActions = remember(sender) { ChatDeliveryActions(sender) }
    fun deliveryAction(message: OutboxMessage, action: DeliveryAction) {
        if (readOnly) return
        scope.launch {
            try {
                if (!deliveryActions.perform(message.clientMessageId, action, composer::restoreMessage))
                    notice = "消息状态已变化，请按最新状态操作"
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { notice = "未能更新消息状态，原消息已保留，请再核对一次" }
        }
    }
    // The shell presents transient notices once; persistent sync errors stay inline.
    val feedback = persona.core.designsystem.LocalAppFeedback.current
    LaunchedEffect(notice) { notice?.let { if (feedback?.notice(it) == true) notice = null } }
    val currentDraftCallback by rememberUpdatedState(onDraftChanged)
    LaunchedEffect(composer) { snapshotFlow { composer.draft }.collect { currentDraftCallback?.invoke(it) } }
    LaunchedEffect(sessionList, clearingPane) {
        clearingPane?.let { pane ->
            sessionList.firstOrNull { it.machineId == key.machineId && it.paneId == pane && it.key != key && it.isLive }?.let {
                clearingPane = null
                if (onContextCleared != null) onContextCleared(it.key)
                else notice = "上下文已清空，请从会话列表打开新会话"
            }
        }
    }
    // The new session gets 20 seconds to show up in the same pane; after that the list has to do, and /clear is offered again.
    LaunchedEffect(clearingPane) {
        if (clearingPane == null) return@LaunchedEffect
        delay(CLEAR_WAIT_MILLIS)
        clearingPane = null
        // A /clear still waiting to go out, or refused, says so on its own bubble.
        if (allMessages.none { it.session == key && it.text == SlashCommand.CLEAR && it.state != DeliveryState.Sent })
            notice = "已发送 /clear，新会话出现后在列表里打开"
    }
    var sending by remember { mutableStateOf(false) }
    var overlay by remember { mutableStateOf(false) }
    var terminalHelp by remember { mutableStateOf(false) }
    var statusSheet by remember { mutableStateOf(false) }
    var taskPageVisible by remember { mutableStateOf(false) }
    var fullText by remember { mutableStateOf<String?>(null) }
    var linkVisible by remember { mutableStateOf(false) }
    val chatList = rememberChatListState(readingKey, anchorStore)
    val search = remember { ChatSearchState() }
    val openWork = chatList.position.expanded
    var timelineVersion by remember { mutableIntStateOf(0) }
    val timeline = remember { ChatTimeline() }
    val liveId = if (session?.status == "working") snapshot.items.lastOrNull { it.role == "user" && it.kind == "text" }?.id else null
    // Which items the lines were last built from: until the first build finishes, the page is still loading, not empty.
    var projectedFrom by remember { mutableStateOf<List<Item>?>(null) }
    val projection by produceState(persona.core.domain.lines.ChatSnapshot(emptyList(), emptyList(), persona.core.domain.lines.TaskIndex()),
        snapshot.items, liveId, openWork, timelineVersion) {
        val items = snapshot.items
        value = withContext(Dispatchers.Default) { timeline.lines(items, liveId, openWork.toSet()) }
        projectedFrom = items
    }
    val feeds by (tasks?.feeds ?: remember { kotlinx.coroutines.flow.MutableStateFlow(emptyMap<String, List<persona.core.contracts.FeedItem>>()) })
        .collectAsStateWithLifecycle()
    val feed = feeds[key.sessionId].orEmpty()
    LaunchedEffect(key, tasks) { tasks?.load(key.sessionId); tasks?.loadTodayIfMain(key.sessionId) }
    val lines = remember(projection, search.selected, feed, tasks) {
        searchVisibleLines(projection.lines, search.selected?.itemId)
            .let { if (tasks != null) persona.core.domain.tasks.AssistantLines.lines(it, feed) else it }.asReversed()
    }
    val searchableItems by produceState(emptyList<Item>(), snapshot.items, timelineVersion, search.visible) {
        if (search.visible) value = withContext(Dispatchers.Default) { timeline.searchItems(snapshot.items) }
    }
    // Photos sent just before a message go into that message's bubble instead of a row of their own.
    val contentUserImages = remember(lines) {
        buildMap<String, List<Item>> {
            lines.asReversed().zipWithNext().forEach { (photos, user) ->
                if (photos is ChatLine.UserImages && (user is ChatLine.MergedUsers ||
                        user is ChatLine.Row && user.row.type == "user")) {
                    put(user.id, photos.images.map { it.item })
                }
            }
        }
    }
    val contentHiddenImages = remember(lines, contentUserImages) {
        lines.asReversed().zipWithNext().mapNotNull { (photos, user) ->
            photos.id.takeIf { photos is ChatLine.UserImages && user.id in contentUserImages }
        }.toSet()
    }
    val contentWorkGroups = remember(lines) { lines.filterIsInstance<ChatLine.Work>().associateBy { it.id } }
    val currentItems by rememberUpdatedState(snapshot.items)
    val currentLive by rememberUpdatedState(liveId)
    val currentLines by rememberUpdatedState(lines)
    val list = chatList.list
    var reportedSeen by remember { mutableStateOf<Item?>(null) }
    val newestVisibleItem by remember {
        derivedStateOf {
            list.layoutInfo.visibleItemsInfo.mapNotNull { visible ->
                currentLines.getOrNull(visible.index - currentMessageCount)?.lastItem()
            }.maxWithOrNull(chatItemOrder)
        }
    }
    val readThrough = listOfNotNull(reportedSeen, snapshot.items.firstOrNull { it.id == snapshot.lastSeenMessageId }).maxWithOrNull(chatItemOrder)
    val context = androidx.compose.ui.platform.LocalContext.current
    val loader = remember(context, repository, readingKey?.scope) { chatImageLoader(context.applicationContext, repository) }
    val markdownCache = remember { ChatMarkdownCache() }
    // Only fresh Agent replies preload their links (ChatLinks.toPreload): paging old history never does.
    val preloadUrls by produceState(emptyList<String>(), snapshot.items) {
        value = withContext(Dispatchers.Default) { ChatLinks.toPreload(snapshot.items, java.time.Instant.now()) }
    }
    // The items whose latest turn has its Markdown parsed. An unread conversation opens on its last question and waits
    // for this: a reply drawn as plain text and then as Markdown grows after the list was placed, and in the reversed
    // list that pushes the question off the top (the newest reply is the one that keeps its place).
    var markdownReadyFor by remember { mutableStateOf<List<Item>?>(null) }
    LaunchedEffect(snapshot.items) {
        // Replies render each text part under "<item id>/<part index>" (MessageContent), so prewarm those keys.
        val items = snapshot.items
        val (latest, earlier) = withContext(Dispatchers.Default) { markdownPrewarm(items) }
        latest.forEach { markdownCache.parse(it.key, it.text) }
        markdownReadyFor = items
        earlier.forEach { markdownCache.parse(it.key, it.text) }
    }
    DisposableEffect(loader) { onDispose { loader.shutdown() } }
    var folding by remember { mutableStateOf(emptySet<String>()) }
    var foldErrors by remember { mutableStateOf(emptyMap<String, String>()) }
    val terminal: (() -> Unit)? = if (readOnly || !canTerminal) null else onTerminal?.let { action -> { action(key) } }
    val lifecycle by LocalLifecycleOwner.current.lifecycle.currentStateAsState()

    fun followEnd() {
        chatList.returnToLatest()
    }
    fun expand(item: Item) {
        val fold = item.fold ?: return
        if (item.id in folding) return
        folding = folding + item.id; foldErrors = foldErrors - item.id
        scope.launch {
            try {
                val page = repository.expandFold(key, item.seq, fold.throughSeq)
                require(page.isNotEmpty() && page.none { it.kind == "fold" })
                timeline.unfold(item, page); timelineVersion++
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { foldErrors = foldErrors + (item.id to "载入失败") }
            finally { folding = folding - item.id }
        }
    }
    fun toggle(id: String) {
        val wasOpen = id in openWork
        chatList.setExpanded(if (wasOpen) openWork - id else openWork + id)
        if (!wasOpen) (projection.lines.firstOrNull { it.id == id } as? ChatLine.Work)?.steps
            ?.filterIsInstance<persona.core.domain.lines.WorkItem.Folded>()?.forEach { expand(it.item.item) }
    }
    fun send(command: String? = null) {
        if (readOnly || !canSend) return
        if (sending || submittedDraft != null) return
        if (command == null && ((!composer.hasContent && linkContexts.isEmpty()) || !composer.ready)) return
        // Routed to a task (「对「xx」说」): straight to its agent, not to Persona (docs/unified-entry.md「指向谁」).
        if (command == null && tasks != null && linkContexts.isEmpty() && composer.attachments.isEmpty() &&
            tasks.sendRouted(key.sessionId, composer.value.text.trim()) { notice = operationFeedback(it).message }) {
            composer.clearIfUnchanged(composer.draft)
            return
        }
        val draft = composer.draft
        val refs = if (command == null) linkContexts else emptyList()
        val wireText = command ?: linkMessage(composer.message(), refs)
        val id = UUID.randomUUID().toString()
        if (command == null) { submittedDraft = id to draft; submittedContexts = refs }
        if (command == "/clear") clearingPane = session?.paneId
        sending = true
        followEnd()
        scope.launch {
            try { sender.send(key, wireText, id) }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) {
                submittedDraft = null; submittedContexts = emptyList()
                if (command == SlashCommand.CLEAR) clearingPane = null
                if (feedback?.failure(e) != true) notice = operationFeedback(e).message
            }
            finally { sending = false }
        }
    }
    // Chat content (group A): the message menu, the image viewer, photos and paths in messages, the API error retry.
    val messageMenu = remember { MessageMenuState() }
    var viewer by remember { mutableStateOf<ViewerRequest?>(null) }
    val photos = remember(chrome.files, key, session?.cwd) { chrome.files?.let { AttachedPhotos(it, key.machineId, session?.cwd.orEmpty()) } }
    // A 「继续」 already on its way counts as working: the card offers no second one.
    val retryId = retryLine(projection.lines, session?.status == "working" || messages.isNotEmpty(), !readOnly && canSend)
    val currentOpenPath by rememberUpdatedState(chrome.onOpenPath)
    // Paths in messages link only when there on the machine (PathLinks.kt): asked of the session's directory.
    val pathPresence = remember(chrome.files, key, session?.cwd, chrome.onOpenPath != null) {
        val files = chrome.files
        val cwd = session?.cwd.orEmpty()
        if (files == null || cwd.isBlank() || chrome.onOpenPath == null) null
        else PathPresence(scope, { paths -> files.stat(key.machineId, cwd, paths) })
    }
    val currentSend by rememberUpdatedState<(String) -> Unit>({ send(it) })
    // Stays the same object while the conversation streams: rows read it, and a new one would redraw them all.
    val rowActions = remember(key, repository, photos, retryId, chrome.onOpenPath != null, pathPresence) {
        ChatRowActions(key, fullText = { item -> repository.fullText(key, item.id) },
            openPath = if (chrome.onOpenPath != null) ({ path -> currentOpenPath?.invoke(path) }) else null,
            paths = pathPresence,
            photos = photos, pictures = { chatViewerImages(key, currentItems, repository) },
            openImages = { images, index -> viewer = ViewerRequest(images, index) },
            menu = messageMenu, retryLineId = retryId, onRetry = { currentSend("继续") })
    }
    val taskHost = tasks?.let { store ->
        AssistantTaskHost(store, key.sessionId, sessionList, { id -> machines.firstOrNull { it.id == id }?.name?.takeIf(String::isNotBlank) ?: id },
            onOpen = { k -> chrome.onOpenSession?.invoke(k) }, onError = { notice = operationFeedback(it).message },
            say = { text -> scope.launch { runCatching { sender.send(key, text, UUID.randomUUID().toString()) } } })
    }
    LaunchedEffect(allMessages, submittedDraft) {
        submittedDraft?.let { (id, draft) ->
            if (allMessages.any { it.clientMessageId == id }) {
                composer.clearIfUnchanged(draft)
                linkContexts = linkContexts.filterNot { ref -> submittedContexts.any { it.id == ref.id } }
                submittedContexts = emptyList()
                submittedDraft = null
            }
        }
    }
    LaunchedEffect(interactionFeedback) {
        if (interactionFeedback != null) { delay(3_000); interactionFeedback = null }
    }
    LaunchedEffect(allRequests) {
        val pending = allRequests.map { it.id }.toSet()
        resolved = resolved.intersect(pending); uncertain = uncertain.intersect(pending)
    }
    val lastUserId = snapshot.items.lastOrNull { it.role == "user" && it.kind == "text" }?.id
    ChatCompletionFeedback(lastUserId, session?.status,
        lifecycle == Lifecycle.State.RESUMED && !chrome.covered && !overlay && !chromeOverlay && !linkVisible &&
            !statusSheet && !terminalHelp && !taskPageVisible && fullText == null && !search.visible &&
            !search.pending && !search.returning && chatList.restored && !chatList.programmaticScroll &&
            !snapshot.loading && snapshot.verified && requests.isEmpty() && snapshot.error == null &&
            !snapshot.ended && !snapshot.replaced && (connection == null || connection == ConnectionState.Online))
    val unreadEntry = if (session?.agentState == AgentState.Unread) lines.firstOrNull { line ->
        line.holds(lastUserId) || (line is ChatLine.MergedUsers && line.users.any { it.id == lastUserId })
    }?.let { ChatListEntry(it.id, lastLineAtTop = true) } else null
    ChatListEffects(chatList, messages.asReversed().map { "outbox-${it.clientMessageId}" } + lines.map { it.id },
        ready = !snapshot.loading && (snapshot.items.isEmpty() || lines.isNotEmpty()) &&
            (unreadEntry == null || markdownReadyFor == snapshot.items), entry = unreadEntry,
        hasOlder = snapshot.hasOlder, loadingOlder = snapshot.loadingOlder,
        onFreeze = { timeline.freeze(currentItems, currentLive); timelineVersion++ },
        onFollow = { timeline.follow(); timelineVersion++ },
        loadOlder = {
            try { repository.loadOlder(key) } catch (e: CancellationException) { throw e }
            catch (e: Exception) { if (feedback?.failure(e) != true) notice = operationFeedback(e).message }
        })
    ChatSearchNavigation(search, chatList, lines)
    LaunchedEffect(list, chrome.covered, search.visible, search.pending, search.returning, chatList.restored, chatList.programmaticScroll, overlay, chromeOverlay, fullText, linkVisible, statusSheet, terminalHelp, taskPageVisible, lifecycle, snapshot.verified) {
        if (chrome.covered || search.visible || search.pending || search.returning || !chatList.restored || chatList.programmaticScroll || overlay || chromeOverlay || fullText != null || linkVisible || statusSheet || terminalHelp || taskPageVisible || lifecycle != Lifecycle.State.RESUMED || !snapshot.verified) return@LaunchedEffect
        snapshotFlow {
            if (list.isScrollInProgress) null else list.layoutInfo.visibleItemsInfo.mapNotNull { visible ->
                currentLines.getOrNull(visible.index - currentMessageCount)?.seenItem()
            }.maxWithOrNull(chatItemOrder)
        }.debounce(450).distinctUntilChanged().collect { item ->
            if (item != null && (reportedSeen == null || chatItemOrder.compare(item, reportedSeen!!) > 0)) try {
                repository.markSeen(key, item.id)
                reportedSeen = item
            } catch (e: CancellationException) { throw e } catch (_: Exception) { }
        }
    }
    val status = chatStatusState(snapshot, session, machine, messages, requests, busyId != null || controls.busy,
        operationUnknown = uncertain.isNotEmpty() || controls.pending != null)
    val replacement = if (snapshot.replaced) sessionList.firstOrNull { candidate ->
        session != null && candidate.key != key && SessionRuntimeState.map(session, listOf(candidate), true).replaced
    } else null
    CompositionLocalProvider(LocalChatMarkdownCache provides markdownCache, LocalChatRowActions provides rowActions,
        LocalContentColor provides PersonaTheme.palette.ink) {
        ChatLinkHost(Modifier.fillMaxSize().background(PersonaTheme.palette.background)
            .then(if (embedded) Modifier else Modifier.imePadding()), preloadUrls, list.isScrollInProgress,
            onContext = { if (readOnly || !canSend) notice = "当前对话只读，不能附加上下文" else linkContexts = linkContexts + it },
            onEdit = { selection, instruction ->
                if (readOnly || !canSend) notice = "当前对话只读，不能发送修改要求"
                else {
                    sender.send(key, ChatLinkContext.editMessage(selection, instruction), selection.id)
                    followEnd()
                }
            }, onVisibilityChanged = { linkVisible = it; if (it) { fullText = null; statusSheet = false } }) { linkInset ->
            BoxWithConstraints(Modifier.fillMaxSize().padding(bottom = linkInset)) {
                val panelMax = maxHeight * 0.55f
                JPage(
                    title = session?.displayTitle ?: "对话",
                    topBar = {
                        Column {
                            if (showTopBar) ChatHeader(session, machine, sessionList, controls, connection, readOnly, onBack,
                                chrome.onOpenSession, chrome.onConnectionDetails, onOverlay = { headerOverlay = it },
                                onFiles = chrome.onFiles, onGit = chrome.onGit, gitLabel = chrome.gitLabel,
                                statusState = status, onDetails = { statusSheet = true },
                                onOpenTodo = chrome.onOpenTodo,
                                onSearch = { search.open(chatList) { timeline.freeze(currentItems, currentLive); timelineVersion++ } }) {
                                // A session without a terminal (a Codex thread, an agent runtime's) has nothing to open or explain.
                                if (!readOnly && canTerminal && renderStyle != ChatRenderStyle.Assistant) JIconButton(PhIcons.TerminalWindow, if (terminal != null) "打开终端" else "终端处理说明",
                                    terminal ?: { terminalHelp = true })
                            }
                            // The space's other tabs under the title (iOS SiblingTabs); not for the assistant's conversation.
                            if (showTopBar && renderStyle != ChatRenderStyle.Assistant) ChatSiblingTabs(session, sessionList, chrome.onOpenSession)

                            ChatTasks(projection.tasks, lines, chatList,
                                onFreeze = { timeline.freeze(currentItems, currentLive); timelineVersion++ },
                                onVisibilityChanged = { taskPageVisible = it },
                                onExpandFold = ::expand, foldErrors = foldErrors,
                                // While the link card peeks, the composer needs the room more (glass laid them over each other).
                                showBar = linkInset.value <= 0f)
                        }
                    },
                    bottomBar = {
                        Column {
                            ChatSessionStatusLine(status, session, snapshot.turnDurationSeconds, projection.turns.lastOrNull()?.toolCount ?: 0,
                                onDetails = { statusSheet = true }, terminal = terminal, draftSaveError = statusBindings.draftSaveError)
                            AnimatedVisibility(interactionFeedback != null, exit = fadeOut()) {
                                // Inset like the status line and the composer around it, not edge to edge.
                                JNotice(interactionFeedback?.notice.orEmpty(), Modifier.padding(horizontal = Tokens.Space.m))
                            }
                            if (!readOnly) InteractionPanel(requests, machine, panelMax, busyId, uncertain, { interaction, resolution ->
                                if (busyId == null && interaction.id !in uncertain && interaction.id !in resolved &&
                                    allRequests.any { it == interaction } &&
                                    !persona.core.domain.answer.RequestExpiry.expired(interaction.expiresAt, java.time.Instant.now())) {
                                    busyId = interaction.id
                                    scope.launch {
                                        try {
                                            val result = submitInteraction { interactions.resolve(interaction.id, resolution) }
                                            if (result.withdraw) {
                                                resolved = resolved + interaction.id
                                                uncertain = uncertain - interaction.id
                                                interactionFeedback = result
                                            } else uncertain = uncertain + interaction.id
                                            notice = if (result == InteractionSubmission.Unknown) result.notice else null
                                            if (result == InteractionSubmission.PromptChanged || result == InteractionSubmission.Unknown) {
                                                try { interactions.refresh() } catch (e: CancellationException) { throw e } catch (_: Exception) { }
                                            }
                                        } finally { busyId = null }
                                    }
                                }
                            }, terminal, { overlay = it }, linkVisible = linkVisible,
                                collapsed = requestCollapsed, onCollapsedChange = { requestCollapsed = it },
                                blockedReason = if (canApprove) null else approvalBlockedReason(session),
                                terminalHelp = canTerminal)
                            (if (feedback == null) notice ?: snapshot.error else snapshot.error)?.let {
                                Text(it, Modifier.padding(horizontal = Tokens.Space.m), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                            }
                            if (!historyReadOnly && !readOnly) ChatStartingNote(session)
                            if (historyReadOnly) historyFooter?.invoke()
                            else if (readOnly) Text("子 Agent 对话只读", Modifier.padding(Tokens.Space.m), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                            else {
                                // What waits in the agent's queue sits right above the composer, while the composer shows (Agents only, as iOS).
                                // Not while a "/" quick message is being picked: the list opens above the field, and with the
                                // keyboard up the queue, the list and the status line together climbed over the top bar.
                                if (renderStyle != ChatRenderStyle.Assistant && (requests.isEmpty() || requestCollapsed) &&
                                    SlashCommand.suggestions(composer.value.text, clear = true).isEmpty())
                                    ChatQueueStrip(session, chrome.actions, composer, { notice = it }, sessions::refresh)
                                // A source that only lets the phone read says so where the typing would be.
                                if (!canSend && session != null && (requests.isEmpty() || requestCollapsed)) ChatSourceNote(session)
                                taskHost?.let { AssistantTaskBar(it) }
                                ChatComposer(key, composer, session?.status == "working", sending || submittedDraft != null,
                                    // /clear and /model type into Claude's pane: Claude in a terminal engine only (positive check).
                                    canClear = clearingPane == null && session?.canTypeClaudeCommands == true,
                                    service = composerService, onSend = { send() }, onCommand = { send(it) }, onNotice = { notice = it },
                                    extraContent = linkContexts.isNotEmpty(), visible = requests.isEmpty() || requestCollapsed,
                                    enabled = canSend, placeholder = if (canSend) "输入消息" else READ_ONLY,
                                    // Permission mode and effort for Claude's sessions, Persona's included; the model for Claude in a pane.
                                    leading = if (session?.canChangeModes == true) ({
                                        ChatModeBar(session, controls, connection == null || connection == ConnectionState.Online,
                                            onOverlay = { modeOverlay = it }, onPickModel = { model -> send("/model $model") })
                                    }) else null,
                                    modeHint = if (session?.canChangeModes == true) chatModeHint(session) else null,
                                    accessory = {
                                        if (linkContexts.isEmpty()) ComposerSuggestion(session, composer)
                                        if (linkContexts.isNotEmpty()) Column(Modifier.heightIn(max = 140.dp).verticalScroll(rememberScrollState())) {
                                            // One line each while the link card peeks: the whole cards left no room for the composer.
                                            linkContexts.forEach { ref -> ChatContextCard(ref, compact = linkInset.value > 0f) { linkContexts = linkContexts.filterNot { it.id == ref.id } } }
                                        }
                                    }, onOverlay = { composerOverlay = it }, agentName = session?.agentLabel ?: "Claude",
                                    canInterrupt = session?.canInterrupt ?: true)
                            }
                        }
                    },
                ) { padding ->
                    val horizontalPadding = pagePadding
                    Column(Modifier.fillMaxSize()) {
                        if (search.selected != null) ReturnChip { search.returnToOrigin(chatList) }
                        search.notice?.let { Text(it, Modifier.padding(horizontal = horizontalPadding), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
                        Box(Modifier.weight(1f)) {
                            ChatList(state = chatList, modifier = Modifier.fillMaxSize(), contentPadding = PaddingValues(
                                start = horizontalPadding, end = horizontalPadding,
                                top = padding.calculateTopPadding() + 12.dp,
                                bottom = padding.calculateBottomPadding() + 12.dp,
                            )) {
                                items(messages.asReversed(), key = { "outbox-${it.clientMessageId}" }, contentType = { "outbox" }) { message ->
                                    val delivery = composerDeliveries[message.clientMessageId]
                                    val deliveryState = if (message.state in setOf(DeliveryState.Refused, DeliveryState.Rejected))
                                        delivery?.state ?: message.state else message.state
                                    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                                        UserBubble(composerMessagePreview(message.text))
                                        OutboxStatus(message.copy(state = deliveryState,
                                            error = delivery?.refusal?.code ?: message.error),
                                            busy = message.clientMessageId in deliveryActions.busyIds,
                                            onAction = if (readOnly) null else { action -> deliveryAction(message, action) },
                                            onDetails = { statusSheet = true })
                                        if (!readOnly && (deliveryState == DeliveryState.Rejected || deliveryState == DeliveryState.Refused)) {
                                            // Edit puts the words back in the box and drops the refused bubble (nothing reached the computer).
                                            ComposerRefusalActions(message, composerService, delivery, terminal,
                                                edit = { deliveryAction(message, DeliveryAction.Edit) }, notice = { notice = it })
                                        }
                                    }
                                }
                                items(lines, key = { it.id }, contentType = { it.contentType() }) { line ->
                                    ChatSearchHighlight(search, line.id) {
                                        androidx.compose.runtime.CompositionLocalProvider(LocalAssistantTaskHost provides taskHost) {
                                        ChatLineContent(line, key, loader,
                                        line.recordedDuration(snapshot.recordedTurnDurations),
                                        ::toggle, ::expand, folding, foldErrors,
                                        lastUserId = lastUserId,
                                        onLastLine = { extent -> chatList.reportLastLine(line.id, extent) }, onFullText = { item ->
                                        scope.launch {
                                            try { fullText = repository.fullText(key, item.id) }
                                            catch (e: CancellationException) { throw e }
                                            catch (e: Exception) { if (feedback?.failure(e) != true) notice = operationFeedback(e).message }
                                        }
                                    }, style = renderStyle, textUpdates = liveTextUpdates,
                                        userImages = contentUserImages[line.id].orEmpty(),
                                        imagesInUserBubble = line.id in contentHiddenImages,
                                        workRunning = (line as? ChatLine.Step)?.let { contentWorkGroups[it.groupId]?.live } == true,
                                        lastWorkStep = (line as? ChatLine.Step)?.let {
                                            contentWorkGroups[it.groupId]?.steps?.lastOrNull()?.id == it.step.id
                                        } == true)
                                        }
                                    }
                                }
                                if (snapshot.loadingOlder) item(key = "loading-older", contentType = "loading") { JLoading() }
                                if (snapshot.hasOlder && !snapshot.loadingOlder) item(key = "older", contentType = "older") {
                                    JTextButton("加载更早的记录", { scope.launch { try { repository.loadOlder(key) } catch (e: CancellationException) { throw e } catch (e: Exception) { if (feedback?.failure(e) != true) notice = operationFeedback(e).message } } })
                                }
                            }
                            // Under the top bar: in glass the list (and this box) runs under it, up to the status bar.
                            WorkCollapseOverlay(lines, list, messages.size, ::toggle, Modifier.align(Alignment.TopStart)
                                .padding(start = horizontalPadding, top = padding.calculateTopPadding() + Tokens.Space.s))
                            val settling = snapshot.loading || projectedFrom == null || (snapshot.items.isNotEmpty() && projectedFrom !== snapshot.items)
                            if (settling && lines.isEmpty() && messages.isEmpty()) JLoading(Modifier.align(Alignment.Center).padding(padding).padding(horizontal = horizontalPadding))
                            else if (!settling && lines.isEmpty() && messages.isEmpty() && snapshot.error == null)
                                JEmptyState("还没有消息", if (readOnly) "这个会话暂时没有对话记录。" else "发一条消息，开始这段对话。",
                                    Modifier.align(Alignment.Center).padding(padding).padding(horizontal = horizontalPadding))
                            if (!chatList.followsEnd) JumpToLatest(timeline.newMessageCount(snapshot.items, newestVisibleItem, readThrough),
                                onClick = chatList::returnToLatest,
                                modifier = Modifier.align(Alignment.BottomEnd).padding(end = horizontalPadding, bottom = padding.calculateBottomPadding() + 16.dp))
                        }
                    }
                }
            }
            if (search.visible) ChatSearchScreen(search, searchableItems, projection.turns.size, snapshot.hasOlder,
                repository, key, onClose = { search.close(chatList) })
            if (terminalHelp) TerminalHelpDialog { terminalHelp = false }
            if (statusSheet) ChatStatusSheet(status, session, messages, deliveryActions.busyIds,
                onAction = if (readOnly) null else ::deliveryAction, onDismiss = { statusSheet = false },
                replacement = replacement,
                onOpenReplacement = if (replacement != null && chrome.onOpenSession != null) {
                    { statusSheet = false; chrome.onOpenSession.invoke(replacement.key) }
                } else null,
                onConnectionDetails = chrome.onConnectionDetails, terminal = terminal,
                recordedDuration = snapshot.turnDurationSeconds, notice = notice ?: snapshot.error,
                recovery = recovery, machine = machine, draftSaveError = statusBindings.draftSaveError, readOnly = readOnly,
                onOpenSession = chrome.onOpenSession?.let { open -> { target -> statusSheet = false; open(target) } })
            fullText?.let { FullTextReader(it, onDismiss = { fullText = null }) }
            val menuTitle = session?.displayTitle ?: "对话"
            MessageMenuSheet(messageMenu, messageMenuWrites(readOnly, session),
                onQuote = composer::quote, onEdit = composer::edit,
                onContext = { text -> linkContexts = linkContexts + messageContext(menuTitle, text); notice = "已作为上下文，随下一条消息发送" },
                onHandToPersona = chrome.onHandToPersona?.takeIf { renderStyle != ChatRenderStyle.Assistant }
                    ?.let { hand -> { text: String -> hand(handoffMessage(menuTitle, text)) } },
                onNotice = { notice = it })
            viewer?.let { request ->
                ChatImageViewer(request.images, request.index, onDismiss = { viewer = null },
                    onEdited = if (readOnly) null else ({ attachment -> composer.beginEdit(); composer.attachments += attachment }),
                    photoLimitReached = composer.attachments.count { it.image } >= 4)
            }
        }
    }
}
