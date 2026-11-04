package persona.feature.assistant

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material3.Text
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.Session
import persona.core.contracts.Machine
import persona.core.contracts.SessionKey
import persona.core.contracts.api.AssistantManagement
import persona.core.contracts.api.AssistantRepository
import persona.core.contracts.api.ChannelSources
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.contracts.api.MainConversationSender
import persona.core.contracts.api.PendingMainConversation
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.DraftRepository
import persona.core.contracts.api.DraftKey
import persona.core.contracts.api.ChatComposerService
import persona.feature.chat.ChatComposer
import persona.feature.chat.ChatComposerState
import persona.feature.chat.ChatDraftEditor
import persona.feature.chat.rememberChatComposerState
import persona.core.navigation.AppTab
import persona.core.navigation.ChatRoute
import persona.core.navigation.Navigator
import persona.feature.assistant.proactive.AssistantProactiveScreen
import persona.feature.assistant.settings.AssistantSettingsScreen
import persona.feature.assistant.settings.StoredTimezoneChoice
import persona.feature.assistant.settings.followPhoneZone
import persona.feature.assistant.settings.settingsApi
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import java.time.LocalTime
import java.util.UUID

/** Supply the shared chat content when available, or use the shell's ChatRoute entry. */
@Composable
fun AssistantScreen(
    navigator: Navigator,
    repository: AssistantRepository,
    messageSender: MessageSender,
    startConversation: suspend () -> SessionKey,
    pinnedConversations: Set<SessionKey> = emptySet(),
    connectionLabel: String? = null,
    hubOnline: Boolean = true,
    chatContent: (@Composable (SessionKey) -> Unit)? = null,
    demo: Boolean = false,
    machines: List<Machine> = LocalAssistantHost.current?.machines.orEmpty(),
    phoneToolsAvailable: Boolean? = LocalAssistantHost.current?.phoneToolsAvailable,
    management: AssistantManagement? = LocalAssistantHost.current?.management,
    assistantChatContent: (@Composable (SessionKey, AssistantChatOptions) -> Unit)? = LocalAssistantHost.current?.chatContent,
    requestedSession: SessionKey? = null,
    mainConversationSender: MainConversationSender? = LocalAssistantHost.current?.mainSender ?: messageSender as? MainConversationSender,
    onKnowledge: (() -> Unit)? = null,
    draftRepository: DraftRepository? = LocalAssistantHost.current?.drafts,
    draftScope: CoroutineScope? = LocalAssistantHost.current?.coroutineScope,
    composerService: ChatComposerService? = assistantComposerService(),
    /** The launcher's 「问 Persona」: put the cursor in the composer and bring up the keyboard. */
    composerFocus: ComposerFocusRequests = AssistantComposerFocus,
    /** 现在要你处理的事 over the home's suggestions; [onOpenNeed] opens a line where it lives (none: not shown). */
    needsYou: NeedsYou = NeedsYou(),
    onOpenNeed: ((NeedsYouLine) -> Unit)? = null,
    /** 「还有 N 件在收件箱」: the inbox tab when not given. */
    onOpenInbox: (() -> Unit)? = null,
    /** Pins or unpins a conversation (the hub's pins); none: the history offers no pinning. */
    onTogglePin: (suspend (SessionKey) -> Unit)? = null,
    /** 语音对话 in the title menu, when the hub runs a voice plugin. */
    onVoice: (() -> Unit)? = null,
) = PersonaTheme {
    val snapshot by repository.assistant.collectAsStateWithLifecycle()
    val outbox by (mainConversationSender?.outbox ?: messageSender.outbox).collectAsStateWithLifecycle()
    val mainMessages = outbox.filter { it.mainTarget || it.session == PendingMainConversation }
    val scope = rememberCoroutineScope()
    var drawerOpen by rememberSaveable { mutableStateOf(false) }
    var sidebarVisible by rememberSaveable { mutableStateOf(true) }
    val snackbar = remember { SnackbarHostState() }
    val feedback = persona.core.designsystem.LocalAppFeedback.current
    var query by rememberSaveable { mutableStateOf("") }
    val transientComposer = rememberChatComposerState()
    val draftEditor = remember(draftRepository, draftScope) {
        if (draftRepository != null && draftScope != null) ChatDraftEditor(draftRepository, draftScope, DraftKey.Assistant()) else null
    }
    DisposableEffect(draftEditor) { onDispose { draftEditor?.close() } }
    val savedComposer by (draftEditor?.composer ?: remember { MutableStateFlow<ChatComposerState?>(null) }).collectAsStateWithLifecycle()
    val draftError by (draftEditor?.error ?: remember { MutableStateFlow<String?>(null) }).collectAsStateWithLifecycle()
    val composer = savedComposer ?: transientComposer
    val draftReady = draftEditor == null || savedComposer != null
    LaunchedEffect(draftEditor, savedComposer) {
        savedComposer?.let { state -> snapshotFlow { state.draft }.collect { draftEditor?.changed(it) } }
    }
    var sending by remember { mutableStateOf(false) }
    var activeMachine by rememberSaveable { mutableStateOf<String?>(null) }
    var activeSession by rememberSaveable { mutableStateOf<String?>(null) }
    var viewingMainQueue by rememberSaveable { mutableStateOf(false) }
    var pendingClientId by rememberSaveable { mutableStateOf<String?>(null) }
    var pendingText by rememberSaveable { mutableStateOf<String?>(null) }
    var openedMain by rememberSaveable { mutableStateOf(false) }
    var proactiveOpen by rememberSaveable { mutableStateOf(false) }
    var settingsOpen by rememberSaveable { mutableStateOf(false) }
    val context = androidx.compose.ui.platform.LocalContext.current
    var ending by remember { mutableStateOf<Session?>(null) }
    var endingBusy by remember { mutableStateOf(false) }
    var ended by remember(management) { mutableStateOf(emptySet<SessionKey>()) }
    var assistantMachine by remember(management) { mutableStateOf("") }
    val hasChat = chatContent != null || assistantChatContent != null
    var menu by remember { mutableStateOf(false) }
    var info by rememberSaveable { mutableStateOf<String?>(null) }
    var showAllRecent by rememberSaveable { mutableStateOf(false) }
    var pinning by remember { mutableStateOf(false) }
    val clipboard = androidx.compose.ui.platform.LocalClipboardManager.current
    fun say(message: String) {
        if (feedback?.notice(message) != true) scope.launch { snackbar.showSnackbar(message) }
    }
    fun copyId(key: SessionKey) {
        clipboard.setText(androidx.compose.ui.text.AnnotatedString(key.sessionId))
        say("已拷贝会话 ID")
    }
    // Pins live on the hub (the Agents list shares them); one change at a time, a failure goes to the usual place.
    fun togglePins(keys: List<SessionKey>) {
        val toggle = onTogglePin ?: return
        if (pinning || keys.isEmpty()) return
        pinning = true
        scope.launch {
            try { keys.forEach { toggle(it) } }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) {
                if (feedback?.failure(error) != true) snackbar.showSnackbar(persona.core.designsystem.operationFeedback(error).message)
            }
            finally { pinning = false }
        }
    }
    val active = activeSession?.let { SessionKey(activeMachine.orEmpty(), it) }
    val groups = remember(snapshot, query, pinnedConversations, ended) {
        conversationGroups(snapshot.copy(conversations = snapshot.conversations.map {
            if (it.key in ended) it.copy(status = "offline", state = "offline") else it
        }), query, pinnedConversations)
    }
    // Persona's face (top bar and the conversation list) shows the conversation on screen, as on iOS: none on the
    // new-conversation page.
    val faceState = personaState(active?.let { key -> snapshot.conversations.firstOrNull { it.key == key } }, hubOnline,
        machines, assistantMachine)
    fun openConversation(key: SessionKey) {
        if (key == PendingMainConversation) {
            viewingMainQueue = true
            return
        }
        openedMain = true
        viewingMainQueue = false
        ended = ended - key
        activeMachine = key.machineId
        activeSession = key.sessionId
        if (!hasChat) {
            navigator.selectTab(AppTab.Jarvis)
            navigator.navigate(ChatRoute(key.machineId, key.sessionId))
        }
    }
    fun newConversation() {
        if (sending) return
        openedMain = true
        viewingMainQueue = false
        activeMachine = null
        activeSession = null
        pendingClientId = null
        pendingText = null
        scope.launch { drawerOpen = false }
    }
    fun startNewConversation() {
        if (sending) return
        if (snapshot.mainSession == null) {
            newConversation()
            return
        }
        sending = true
        scope.launch {
            try {
                // Explicit branches are separate from all home and external main-message entry points.
                openConversation(startConversation())
                drawerOpen = false
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                scope.launch { snackbar.showSnackbar("暂时无法新建对话，输入已保留") }
            } finally {
                sending = false
            }
        }
    }
    fun submit() {
        if (sending || !draftReady || !composer.hasContent || !composer.ready) return
        val sentDraft = composer.draft
        val text = composer.message()
        sending = true
        val submitted = text.trim()
        openedMain = true
        if (pendingClientId == null || pendingText != submitted) {
            pendingClientId = UUID.randomUUID().toString()
            pendingText = submitted
        }
        scope.launch {
            try {
                sendAssistantMessage(mainConversationSender, messageSender, active, submitted, requireNotNull(pendingClientId))
                // Clear only after the outbox commit; retries keep their original operation id.
                composer.clearIfUnchanged(sentDraft)
                draftEditor?.changed(composer.draft)
                pendingClientId = null
                pendingText = null
                if (active == null) viewingMainQueue = true
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                if (feedback?.failure(error) != true) scope.launch {
                    snackbar.showSnackbar(persona.core.designsystem.operationFeedback(error).message)
                }
            } finally {
                sending = false
            }
        }
    }
    LaunchedEffect(mainMessages, viewingMainQueue) {
        if (viewingMainQueue || (!openedMain && mainMessages.isNotEmpty() && requestedSession == null)) {
            openedMain = true
            viewingMainQueue = true
            mainMessages.firstOrNull { it.session != PendingMainConversation }?.session?.let { target ->
                openConversation(target)
                assistantAttempt { repository.refresh() }
            }
        }
    }
    LaunchedEffect(repository) {
        try { repository.refresh() }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { /* Cached conversations remain available while offline. */ }
    }
    LaunchedEffect(management) {
        if (management != null) {
            assistantAttempt { management.warm() }
            assistantAttempt { assistantMachine = management.notes().machineId }
            // Routines and the assistant's "today" follow the phone's time zone unless the user picked one.
            assistantAttempt { followPhoneZone(management.settingsApi(), StoredTimezoneChoice(context)) }
        }
    }
    LaunchedEffect(requestedSession) {
        requestedSession?.let { openedMain = true; proactiveOpen = false; settingsOpen = false; openConversation(it) }
    }
    LaunchedEffect(snapshot.mainSession, snapshot.conversations) {
        if (!demo && !openedMain && requestedSession == null && mainMessages.isEmpty()) {
            val initial = snapshot.mainSession ?: snapshot.conversations
                .sortedWith(compareByDescending<Session> { it.updated }.thenBy { it.key.path }).firstOrNull()?.key
            initial?.let(::openConversation)
        }
    }
    val keyboard = androidx.compose.ui.platform.LocalSoftwareKeyboardController.current
    val focusTarget by rememberUpdatedState(composer)
    LaunchedEffect(composerFocus) {
        composerFocus.requests.collect { at ->
            if (at == null || !composerFocus.take(at)) return@collect
            // The home's composer (an embedded conversation keeps its composer inside the shared chat page); what is
            // sent there goes to the main conversation, as from any home entry.
            proactiveOpen = false; settingsOpen = false; drawerOpen = false
            if (activeSession != null && hasChat && !demo) newConversation()
            // After the tab's first layout and the saved draft: the text field has to be attached (the old app waited 300 ms).
            kotlinx.coroutines.withTimeoutOrNull(3_000) { snapshotFlow { draftEditor == null || savedComposer != null }.first { it } }
            kotlinx.coroutines.delay(300)
            focusTarget.focusRequest++
            kotlinx.coroutines.delay(100)
            keyboard?.show()
        }
    }
    BackHandler(drawerOpen || active != null || viewingMainQueue) {
        if (drawerOpen) scope.launch { drawerOpen = false } else newConversation()
    }
    val sidebar: @Composable () -> Unit = {
        AssistantDrawer(faceState, query, { query = it }, groups, active, snapshot.mainSession != null,
            onEnd = if (management != null) ({ ending = it }) else null,
            onMain = {
                openedMain = true
                snapshot.mainSession?.let(::openConversation) ?: run { newConversation(); viewingMainQueue = true }
                scope.launch { drawerOpen = false }
            },
            onOpen = { openConversation(it); scope.launch { drawerOpen = false } },
            onInfo = {
                if (it == "知识库" && onKnowledge != null) onKnowledge()
                else if (management != null && it in listOf("资料页", "启动器")) settingsOpen = true
                else if (management != null && it == "定时任务") proactiveOpen = true
                else info = it
                scope.launch { drawerOpen = false }
            },
            onNew = ::startNewConversation,
            onHome = { newConversation() },
            onSettings = { scope.launch { drawerOpen = false }; navigator.selectTab(AppTab.Settings) },
            onPin = if (onTogglePin != null) ({ session -> togglePins(listOf(session.key)) }) else null,
            // The hub's pins also hold the Agents list's sessions: only Persona's own conversations are unpinned here.
            onUnpinAll = if (onTogglePin != null) ({
                togglePins(snapshot.conversations.map { it.key }.distinct().filter { it in pinnedConversations && it != snapshot.mainSession })
            }) else null,
            onCopy = { copyId(it.key) },
            showAllRecent = showAllRecent,
            onShowAllRecent = { showAllRecent = true },
        )
    }
    val body: @Composable (Boolean) -> Unit = { wide ->
        val hasComposer = active == null || !hasChat || demo
        JPage(
            title = LocalPersonaName.current,
            modifier = Modifier.fillMaxSize().imePadding(),
            topBar = {
                CompositionLocalProvider(LocalRootLeading provides {
                    JPersonaButton(faceState, "对话列表", {
                        if (wide) sidebarVisible = !sidebarVisible else drawerOpen = true
                    })
                }) {
                    JTopBar(
                        title = LocalPersonaName.current, subtitle = connectionLabel,
                        titleBusy = !connectionLabel.isNullOrBlank(), onTitleClick = { menu = true },
                        actions = {
                            JIconButton(PhIcons.NotePencil, "新对话", ::startNewConversation, enabled = !sending)
                            JIconButton(PhIcons.UserCircle, "助手资料", {
                                if (management != null) settingsOpen = true else info = "助手设置"
                            })
                        },
                    )
                }
            },
            bottomBar = if (hasComposer || snackbar.currentSnackbarData != null) ({
                Column {
                    SnackbarHost(snackbar) { data ->
                        JNotice(data.visuals.message, Modifier.padding(horizontal = pagePadding),
                            actionLabel = "知道了", onAction = { data.dismiss() })
                    }
                    if (hasComposer) {
                        draftError?.let { JNotice(it, Modifier.padding(horizontal = pagePadding), status = Status.NeedsYou) }
                        val uploadTarget = active ?: snapshot.mainSession
                            ?: SessionKey(assistantMachine, PendingMainConversation.sessionId)
                        ChatComposer(uploadTarget, composer, working = false, sending = sending || !draftReady,
                            canClear = false, service = composerService?.takeIf { uploadTarget.machineId.isNotBlank() },
                            onSend = ::submit, onCommand = { composer.insert(it, composer.focused) },
                            onNotice = { message ->
                                if (feedback?.notice(message) != true) scope.launch { snackbar.showSnackbar(message) }
                            }, enabled = draftReady && !sending, placeholder = "问 ${LocalPersonaName.current}…")
                    }
                }
            }) else null,
            // An embedded conversation is a page of its own and softens the edges under these bars itself.
            edges = !(active != null && hasChat),
        ) { padding ->
            if (active != null && hasChat) {
                // The conversation fills the page and scrolls under the bar above it and the dock below, like any
                // page, instead of being cut off at a line under the bar; the chrome insets give it their room.
                // (The demo notice sits under the bar, so there the conversation starts below it.)
                val host = ChromeInsets(bottom = padding.calculateBottomPadding(), handlesNavigationBar = true)
                Column(Modifier.fillMaxSize().then(if (demo) Modifier.padding(top = padding.calculateTopPadding()) else Modifier)) {
                    if (demo) AssistantDemoNotice()
                    Box(Modifier.weight(1f)) {
                        CompositionLocalProvider(LocalChromeInsets provides if (demo) host else host.copy(top = padding.calculateTopPadding())) {
                            if (assistantChatContent != null) assistantChatContent(active, AssistantChatOptions())
                            else chatContent?.invoke(active)
                        }
                    }
                }
            } else if (viewingMainQueue || (active != null && !hasChat)) {
                val visible = if (active != null) outbox.filter { it.session == active } else mainMessages
                AssistantPendingConversation(visible, snapshot.conversations.firstOrNull { it.key == active },
                    Modifier.fillMaxSize(), contentPadding = padding, demo = demo)
            } else {
                AssistantHome(enabled = !sending && draftReady && composer.ready, machines = machines,
                    phoneToolsAvailable = phoneToolsAvailable, contentPadding = padding, demo = demo,
                    onSuggestion = {
                        composer.value = androidx.compose.ui.text.input.TextFieldValue(it, androidx.compose.ui.text.TextRange(it.length))
                        submit()
                    }, needsYou = needsYou, onNeed = onOpenNeed,
                    onMore = onOpenInbox ?: { navigator.selectTab(AppTab.Inbox) })
            }
        }
    }
    // A run or a follow-up opened from 主动做事 (here or inside the settings page) shows its conversation in place.
    val openFromSchedule: (SessionKey) -> Unit = {
        proactiveOpen = false; settingsOpen = false; openedMain = true; openConversation(it)
        scope.launch { assistantAttempt { repository.refresh() } }
    }
    if (settingsOpen && management != null) {
        AssistantSettingsScreen(management, machines, onBack = { settingsOpen = false }, inPlace = true, onMoved = {
            // The conversations and the upload target now belong to the other machine.
            assistantAttempt { repository.refresh() }
            assistantAttempt { assistantMachine = management.notes().machineId }
        }, onOpenConversation = openFromSchedule)
        return@PersonaTheme
    }
    if (proactiveOpen && management != null) {
        // Shown in place of the conversation: back on its first page closes it here (its own pages come first).
        BackHandler { proactiveOpen = false }
        AssistantProactiveScreen(onBack = { proactiveOpen = false }, onOpenConversation = openFromSchedule,
            management = management, machines = machines)
        return@PersonaTheme
    }
    BoxWithConstraints(Modifier.fillMaxSize()) {
        if (maxWidth >= 840.dp) {
            Row(Modifier.fillMaxSize()) {
                // The list of conversations sits on the same background as the page beside it, not a panel of its own.
                if (sidebarVisible) Box(Modifier.width(300.dp).fillMaxHeight()) {
                    PageBackdrop()
                    Box(Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding()) { sidebar() }
                }
                Box(Modifier.weight(1f)) { body(true) }
            }
        } else {
            body(false)
            if (drawerOpen) JSheet(onDismiss = { drawerOpen = false }, title = "对话列表") {
                Box(Modifier.fillMaxWidth().fillMaxHeight(0.9f)) { sidebar() }
            }
        }
    }
    if (menu) JActionSheet(
        title = LocalPersonaName.current, onDismiss = { menu = false },
        actions = buildList {
            listOf("助手设置", "知识库").forEach { label ->
                add(JAction(label, onClick = {
                    if (label == "知识库" && onKnowledge != null) onKnowledge()
                    else if (label == "助手设置" && management != null) settingsOpen = true
                    else info = label
                }))
            }
            onVoice?.let { open -> add(JAction("语音对话", onClick = open)) }
            add(JAction("会话信息", onClick = { info = "会话信息" }))
            active?.let { key -> add(JAction("拷贝会话 ID", separatorBefore = true, onClick = { copyId(key) })) }
        },
    )
    ending?.let { session ->
        JDialog(onDismiss = { if (!endingBusy) ending = null }, title = "结束这个对话？",
            text = "「${session.conversationTitle()}」会停止运行。历史记录仍会保留。",
            destructive = true, confirmEnabled = !endingBusy,
            confirm = "结束对话" to {
                endingBusy = true
                scope.launch {
                    try {
                        requireNotNull(management).endConversation(session.key)
                        ended = ended + session.key
                        ending = null
                        if (active == session.key) newConversation()
                        assistantAttempt { repository.refresh() }
                    } catch (cancelled: CancellationException) { throw cancelled }
                    catch (_: Exception) { snackbar.showSnackbar("暂时没能结束对话，请刷新列表后重试") }
                    finally { endingBusy = false }
                }
            }, dismiss = "取消" to { if (!endingBusy) ending = null })
    }
    info?.let { title ->
        JDialog(onDismiss = { info = null }, title = title,
            text = when (title) {
                "定时任务" -> "体验模式里没有定时任务。连上自己的 hub 以后，可以在这里管理 Persona 的定时任务、跟进和运行记录。"
                "知识库" -> "在这里查看人、群和项目的资料。知识库内容尚未接入。"
                "资料页" -> "资料页连接尚未配置。接入后可管理定时任务、跟进、关于我和记忆。"
                "助手设置" -> "体验模式里没有助手设置。连上自己的 hub 以后，可以选 Persona 在哪台机器上运行，改关于我和记忆。"
                "会话信息" -> if (active == null) "当前还没有打开对话。" else "当前对话：${active.sessionId}"
                else -> "启动器配置尚未接入。"
            }, confirm = "知道了" to { info = null }, dismiss = null)
    }
}

private data class Suggestion(val icon: PhIcon, val title: String, val detail: String, val prompt: String)
private val suggestions = listOf(
    Suggestion(PhIcons.Tray, "分拣收件箱", "先看 @我 和要回复的", "帮我整理收件箱，先看 @我 和需要回复的消息。"),
    Suggestion(PhIcons.Terminal, "Agent 进度", "各台电脑上在做什么", "看看各台电脑上的 Agent 在做什么。"),
    Suggestion(PhIcons.CalendarBlank, "今天的日程", "读手机日历", "看一下我今天的安排，合并手机日历和飞书日历，按时间列出来。"),
    Suggestion(PhIcons.Clock, "5 点提醒我", "到点在手机上跟进", "请在今天 17 点提醒我，先问我要提醒什么；如果时间已经过了，请先确认日期。"),
    Suggestion(PhIcons.BookOpen, "查知识库", "人、群、项目", "帮我查知识库，先问我想了解什么。"),
    Suggestion(PhIcons.FileText, "总结一篇文档", "粘贴链接就行", "帮我总结一篇文档，接下来我会发链接。"),
)

@Composable
@OptIn(ExperimentalLayoutApi::class)
private fun AssistantHome(
    enabled: Boolean, machines: List<Machine>, phoneToolsAvailable: Boolean?, contentPadding: PaddingValues,
    demo: Boolean, onSuggestion: (String) -> Unit,
    needsYou: NeedsYou = NeedsYou(), onNeed: ((NeedsYouLine) -> Unit)? = null, onMore: () -> Unit = {},
) {
    LazyVerticalGrid(
        columns = GridCells.Fixed(2), modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = pagePadding, end = pagePadding,
            top = contentPadding.calculateTopPadding() + 16.dp, bottom = contentPadding.calculateBottomPadding() + 24.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (demo) item(span = { GridItemSpan(2) }) { AssistantDemoNotice() }
        item(span = { GridItemSpan(2) }) {
            Column(Modifier.padding(vertical = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(greeting(LocalTime.now().hour), style = PersonaTheme.type.largeTitle, color = PersonaTheme.palette.ink)
                Text("消息、日程和手头的事，都可以交给我。", style = PersonaTheme.type.voice, color = PersonaTheme.palette.ink2)
                Text("发出的内容会进入主对话，离线时先保存在本机。", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
            }
        }
        // What waits on the user comes before what Persona could do: the suggestions stay under it.
        if (onNeed != null && !needsYou.isEmpty) item(key = "needs-you", span = { GridItemSpan(2) }) {
            NeedsYouSection(needsYou, onNeed, onMore)
        }
        items(suggestions) { suggestion ->
            JCard(Modifier.fillMaxWidth().heightIn(min = 144.dp), padding = 16.dp,
                onClick = if (enabled) ({ onSuggestion(suggestion.prompt) }) else null) {
                JIcon(suggestion.icon, null, tint = PersonaTheme.palette.accent)
                Spacer(Modifier.height(12.dp))
                Text(suggestion.title, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                Spacer(Modifier.height(4.dp))
                Text(suggestion.detail, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        item(span = { GridItemSpan(2) }) {
            Column {
                JSectionHeader("在线设备")
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    machines.filter { it.online }.distinctBy { it.id }.forEach { machine ->
                        JChip(machine.displayName, icon = PhIcons.Desktop)
                    }
                    JChip(when (phoneToolsAvailable) {
                        true -> "这台手机"
                        false -> "手机工具暂不可用"
                        null -> "手机权限待确认"
                    }, icon = PhIcons.DeviceMobile)
                }
            }
        }
    }
}

@Composable
private fun AssistantDrawer(
    face: PersonaState,
    query: String, onQuery: (String) -> Unit, groups: ConversationGroups, active: SessionKey?, hasMain: Boolean,
    onEnd: ((Session) -> Unit)?,
    onMain: () -> Unit, onOpen: (SessionKey) -> Unit, onInfo: (String) -> Unit,
    onNew: () -> Unit, onHome: () -> Unit, onSettings: () -> Unit,
    onPin: ((Session) -> Unit)? = null, onUnpinAll: (() -> Unit)? = null, onCopy: (Session) -> Unit = {},
    showAllRecent: Boolean = false, onShowAllRecent: () -> Unit = {},
) {
    Column(Modifier.fillMaxHeight().padding(horizontal = 12.dp)) {
        LazyColumn(Modifier.weight(1f), contentPadding = PaddingValues(bottom = 12.dp)) {
            item {
                Row(Modifier.padding(vertical = 12.dp), verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    PersonaFace(face)
                    Text(LocalPersonaName.current, style = PersonaTheme.type.title, color = PersonaTheme.palette.ink)
                }
            }
            item { JSearchField(query, onQuery, "搜索对话") }
            item { JSectionHeader("主对话") }
            if (groups.main != null) item {
                // The main conversation is always first: it has no pin of its own.
                ConversationRow(groups.main, main = true, selected = groups.main.key == active,
                    onOpen = { onOpen(groups.main.key) }, onEnd = onEnd?.let { action -> { action(groups.main) } },
                    onCopy = { onCopy(groups.main) })
            } else if (query.isBlank()) item {
                JGroupItem(GroupPosition.Single) {
                    DrawerItem(if (hasMain) "回主对话" else "开始主对话", PhIcons.ChatCircle, onMain)
                }
            }
            if (groups.pinned.isNotEmpty()) item(key = "pinned-header") {
                var pinMenu by remember { mutableStateOf(false) }
                JSectionHeader("置顶", count = groups.pinned.size, action = if (onUnpinAll != null) ({
                    JIconButton(PhIcons.DotsThree, "置顶操作", { pinMenu = true })
                }) else null)
                if (pinMenu && onUnpinAll != null) JActionSheet(
                    listOf(JAction("全部取消置顶", PhIcons.PushPin, onClick = onUnpinAll)), { pinMenu = false }, title = "置顶",
                )
            }
            itemsIndexed(groups.pinned, key = { _, session -> session.key.path }) { index, session ->
                ConversationRow(session, main = false, selected = session.key == active,
                    position = GroupPosition.of(index, groups.pinned.size),
                    onOpen = { onOpen(session.key) }, onEnd = onEnd?.let { action -> { action(session) } },
                    pinned = true, onPin = onPin?.let { pin -> { pin(session) } }, onCopy = { onCopy(session) })
            }
            // The most recent 30, then one row for the rest (a search shows everything it finds).
            val recent = recentRows(groups.recent, query, showAllRecent)
            val recentCount = recent.shown.size + if (recent.hidden > 0) 1 else 0
            if (groups.recent.isNotEmpty()) item(key = "recent-header") { JSectionHeader("最近", count = groups.recent.size) }
            itemsIndexed(recent.shown, key = { _, session -> session.key.path }) { index, session ->
                ConversationRow(session, main = false, selected = session.key == active,
                    position = GroupPosition.of(index, recentCount),
                    onOpen = { onOpen(session.key) }, onEnd = onEnd?.let { action -> { action(session) } },
                    pinned = false, onPin = onPin?.let { pin -> { pin(session) } }, onCopy = { onCopy(session) })
            }
            if (recent.hidden > 0) item(key = "recent-all") {
                JGroupItem(GroupPosition.of(recentCount - 1, recentCount)) {
                    DrawerItem("查看全部 ${groups.recent.size} 个", PhIcons.ChatsCircle, onShowAllRecent)
                }
            }
            if (groups.main == null && groups.pinned.isEmpty() && groups.recent.isEmpty()) {
                item { JEmptyState(if (query.isBlank()) "还没有对话" else "没有找到匹配的对话",
                    if (query.isBlank()) "发一句话，开始和 Persona 聊聊。" else "换个关键词再试试。") }
            }
            item { JSectionHeader("更多") }
            item { JGroupItem(GroupPosition.of(0, 6)) { DrawerItem("返回首页", PhIcons.House, onHome) } }
            item { JGroupItem(GroupPosition.of(1, 6)) { DrawerItem("资料页", PhIcons.UserCircle) { onInfo("资料页") } } }
            item { JGroupItem(GroupPosition.of(2, 6)) { DrawerItem("定时任务", PhIcons.Clock) { onInfo("定时任务") } } }
            item { JGroupItem(GroupPosition.of(3, 6)) { DrawerItem("知识库", PhIcons.BookOpen) { onInfo("知识库") } } }
            item { JGroupItem(GroupPosition.of(4, 6)) { DrawerItem("会话信息", PhIcons.Info) { onInfo("会话信息") } } }
            item { JGroupItem(GroupPosition.of(5, 6)) { DrawerItem("启动器", PhIcons.RocketLaunch) { onInfo("启动器") } } }
        }
        Row(Modifier.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            JButton("新对话", onNew, Modifier.weight(1f), icon = PhIcons.NotePencil)
            JIconButton(PhIcons.Gear, "设置", onSettings)
        }
    }
}

/**
 * One conversation of the history. Its menu (⋯, or a long press) pins or unpins it ([onPin]; the main conversation
 * has none), copies its session id, and ends it while it runs ([onEnd]).
 */
@Composable
private fun ConversationRow(
    session: Session, main: Boolean, selected: Boolean, onOpen: () -> Unit, onEnd: (() -> Unit)?,
    position: GroupPosition = GroupPosition.Single,
    pinned: Boolean = false, onPin: (() -> Unit)? = null, onCopy: () -> Unit = {},
) {
    var menu by remember { mutableStateOf(false) }
    // The channel its latest message came in from (WeChat, the Feishu bot…), as a small mark after the title.
    val channels = LocalChannels.current
    val channel = remember(session.channels, channels) { ChannelSources.latest(session.channels, channels) }
    JGroupItem(position) {
        // The time goes on the line under the title, with the state: beside the title it left the title two or three
        // characters in the wide window's 300dp list (2026-11-02), and a narrow drawer cut it short too.
        JRow(session.conversationTitle(), onClick = onOpen, onLongClick = { menu = true },
            titleAccessory = channel?.let { c -> { JChannelMark(c, size = MarkSize.Inline) } },
            subtitle = session.lastText?.takeIf { it.isNotBlank() },
            leading = { JIcon(if (selected) PhIcons.CheckCircle else PhIcons.ChatCircle, null,
                selected = selected, tint = if (selected) PersonaTheme.palette.accent else PersonaTheme.palette.ink2) },
            trailing = { JIconButton(PhIcons.DotsThree, "对话操作", { menu = true }) }, chevron = false)
        Row(Modifier.padding(start = 16.dp, end = 16.dp, bottom = 10.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            if (main) JTag("主对话")
            StatusLabel(session.conversationStatus())
            conversationTime(session.updated)?.let { time ->
                Text(time, Modifier.weight(1f), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3,
                    maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.End)
            }
        }
    }
    if (menu) JActionSheet(
        title = session.conversationTitle(), onDismiss = { menu = false },
        actions = conversationActions(pinned, session.isLive, onPin, onCopy, onEnd),
    )
}

/** The history row's menu: 置顶 / 取消置顶 (when it can be pinned), 拷贝会话 ID, then 结束对话 while it runs. */
internal fun conversationActions(pinned: Boolean, live: Boolean, onPin: (() -> Unit)?, onCopy: () -> Unit, onEnd: (() -> Unit)?): List<JAction> =
    buildList {
        onPin?.let { add(JAction(if (pinned) "取消置顶" else "置顶", PhIcons.PushPin, onClick = it)) }
        add(JAction("拷贝会话 ID", PhIcons.Copy, onClick = onCopy))
        if (onEnd != null && live) add(JAction("结束对话", PhIcons.Stop, destructive = true, separatorBefore = true, onClick = onEnd))
    }

@Composable
private fun DrawerItem(title: String, icon: PhIcon, onClick: () -> Unit) {
    JRow(title, leadingIcon = icon, onClick = onClick)
}

@Composable
internal fun AssistantDemoNotice() = JNotice("内容仅保存在本次体验中。", title = "体验模式", icon = PhIcons.Info)

@Composable
internal fun AssistantDemo(navigator: Navigator) {
    val repository = remember { DemoAssistantRepository() }
    val messages by repository.outbox.collectAsStateWithLifecycle()
    AssistantScreen(navigator, repository, repository, repository::start, repository.pinned, demo = true,
        chatContent = { session ->
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                items(messages.filter { it.session == session }, key = { it.clientMessageId }) { message ->
                    UserBubble(message.text)
                }
                item { Text("这是示例对话。接入助手后，回复会显示在这里。", style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2) }
            }
        })
}
