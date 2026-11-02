package jarvis.android.ui.shell

import jarvis.android.ui.settings.SettingsRoute
import jarvis.android.ui.settings.SettingsPage
import jarvis.android.ui.design.LocalPaneRoot
import jarvis.android.ui.InboxDetailPane
import jarvis.android.ui.HandOff
import androidx.compose.runtime.CompositionLocalProvider
import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.slideOutVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.zIndex
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import jarvis.android.chat.ConnectionWatch
import jarvis.android.data.AgentState
import jarvis.android.data.Hub
import jarvis.android.data.InboxBucket
import jarvis.android.data.SessionKey
import jarvis.android.ui.AgentsListOptions
import jarvis.android.ui.AgentsScreen
import jarvis.android.ui.AssistantPage
import jarvis.android.ui.AssistantProfile
import jarvis.android.ui.ChatScreen
import jarvis.android.ui.InboxScreen
import jarvis.android.ui.JTheme
import jarvis.android.ui.NewSessionSheet
import jarvis.android.ui.SettingsScreen
import jarvis.android.ui.SpaceScreen
import jarvis.android.ui.VoiceScreen
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JMotion
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.surface
import jarvis.android.ui.knowledge.KnowledgeScreen
import jarvis.android.ui.machines.MachineDetailScreen
import jarvis.android.ui.machines.MachineMonitorScreen
import jarvis.android.ui.machines.MachinesScreen
import jarvis.android.ui.web.AppLink
import jarvis.android.ui.web.LinkHost
import jarvis.android.ui.web.Links
import kotlinx.coroutines.launch

/**
 * The ChatGPT-style shell (docs/android.md「外壳」, iOS `ShellView`): no tab bar. 助手 (the Jarvis assistant) and Agents
 * are two pages under the top switch, both kept alive (switching keeps each one's place); the drawer slides both
 * aside; 收件箱 / 定时任务 / 知识库 / 机器 push over them; 设置 is a full-screen sheet; a chat opens over everything.
 * Notifications, shortcuts and links still route through [openSession] / [openTab] and [Links].
 */
@Composable
fun ShellRoot(
    hub: Hub, openSession: MutableState<SessionKey?>, openTab: MutableState<String?>,
    /** Given by the screenshot tests (loaded ahead); otherwise made here. */
    assistant: AssistantModel? = null, options: AgentsListOptions? = null,
    startPage: ShellPage = ShellPage.Assistant, startRoutes: List<String> = emptyList(), startDrawer: Float = 0f,
    startShares: Map<PaneKind, Float> = emptyMap(),
    /** Screenshots: an inbox item open, 设置 open with these pages, a space page in the Agents pane. */
    startInbox: String? = null, startSettings: List<String>? = null, startSpace: String? = null,
) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val model = assistant ?: remember { AssistantModel(hub, jarvis.android.JarvisApp.instance.scope, ctx) }
    val listOptions = options ?: remember { AgentsListOptions(ctx) }
    var page by rememberSaveable { mutableStateOf(startPage) }
    var agentsBuilt by rememberSaveable { mutableStateOf(startPage == ShellPage.Agents) }
    var routes by rememberSaveable { mutableStateOf(startRoutes) }
    var settings by rememberSaveable { mutableStateOf(startSettings != null) }
    val settingsStack = rememberSaveable { mutableStateOf(startSettings.orEmpty()) }
    var newSession by remember { mutableStateOf(false) }
    var askFocus by remember { mutableStateOf(false) }
    val drawer = remember { DrawerState(startDrawer) }

    fun push(r: String) { routes = routes + r }
    fun pop() { routes = routes.dropLast(1) }
    fun go(p: ShellPage) { page = p; if (p == ShellPage.Agents) agentsBuilt = true }

    val byMachine by hub.sessions.collectAsState()
    val machines by hub.machines.collectAsState()
    val pending by hub.interactions.collectAsState()
    val inbox by hub.inbox.collectAsState()
    val mutes by hub.mutes.collectAsState()
    val all = byMachine.values.flatten()
    val assistantKeys = all.filter { it.assistant }.map { it.key }.toSet()
    // The agents waiting on you (an answer, an approval, a plan); muted ones do not call.
    val waiting = (pending.filter { it.sessionKey !in assistantKeys }.map { it.sessionKey } +
        all.filter { !it.assistant && it.agentState == AgentState.NeedsYou && !hub.isMuted(it) }.map { it.key }).toSet().size
    val inboxAction = inbox.count { it.bucket == InboxBucket.Action }

    // A chat asked for from outside (a notification, a link, the widget): the assistant's open on 助手, others over all.
    val open = openSession.value
    LaunchedEffect(open, byMachine.isNotEmpty()) {
        val k = open ?: return@LaunchedEffect
        val s = hub.session(k)
        if (s?.assistant == true) {
            model.open(k.sessionId); go(ShellPage.Assistant); routes = emptyList(); settings = false
            openSession.value = null
        }
    }
    openTab.value?.let {
        when (it) {
            "jarvis" -> { go(ShellPage.Assistant); settings = false; routes = emptyList() }
            "ask" -> { go(ShellPage.Assistant); settings = false; routes = emptyList(); askFocus = true }
            "agents" -> { go(ShellPage.Agents); settings = false; routes = emptyList(); openSession.value = null }
        }
        openTab.value = null
    }
    Links.navigate = { link ->
        when (link) {
            is AppLink.Session -> { settings = false; openSession.value = link.key }
            is AppLink.Tab -> {
                settings = false
                when (link.name) {
                    "inbox" -> { go(ShellPage.Assistant); routes = listOf("inbox") }
                    "jarvis" -> { go(ShellPage.Assistant); routes = emptyList() }
                    else -> { go(ShellPage.Agents); routes = emptyList(); openSession.value = null }
                }
            }
        }
    }
    val handToJarvis: (String) -> Unit = { prompt ->
        settings = false; routes = emptyList(); openSession.value = null; go(ShellPage.Assistant); model.start(prompt)
    }
    Links.handToJarvis = handToJarvis

    LaunchedEffect(Unit) { model.reload(); model.warm() }

    val toast: (String) -> Unit = { jarvis.android.ui.chat.Toasts.show(it, jarvis.android.ui.chat.ToastStyle.ERROR) }
    // The left pane of a wide window (the sidebar on 助手, the list on Agents): ☰ hides and shows it, and so does
    // dragging the divider far left; how wide it is, each page keeps.
    var leftPane by rememberSaveable { mutableStateOf(true) }
    val splitPrefs = remember { SplitPrefs(ctx) }
    var shares by remember { mutableStateOf(PaneKind.entries.associateWith { startShares[it] ?: splitPrefs.share(it) }) }
    fun keepShare(k: PaneKind, share: Float) { shares = shares + (k to share); splitPrefs.save(k, share) }
    // What a wide window's right panes show besides a chat (folding keeps them): the inbox item, the space page.
    val inboxOpen = rememberSaveable { mutableStateOf(startInbox) }
    // The open item belongs to the inbox: leaving it closes the item (it would pop up again next time).
    LaunchedEffect(routes) { if ("inbox" !in routes) inboxOpen.value = null }
    var paneSpace by rememberSaveable { mutableStateOf(startSpace) }
    // 交给 AI from the inbox: to Jarvis (a new conversation) or into a running agent, whose chat then opens.
    val inboxHandOff: (HandOff) -> Unit = { h ->
        val to = h.to
        if (to == null) handToJarvis(h.prompt)
        else {
            jarvis.android.JarvisApp.instance.scope.launch {
                runCatching { hub.api.send(to, h.prompt, java.util.UUID.randomUUID().toString()) }.onFailure { e ->
                    jarvis.android.ui.chat.Toasts.show("没交过去：${e.message}", jarvis.android.ui.chat.ToastStyle.ERROR)
                }
            }
            openSession.value = to
        }
    }

    LinkHost {
        BoxWithConstraints(
            Modifier.fillMaxSize().background(JTheme.colors.page)
                // A camera cutout or the navigation bar on a side (landscape): content stays clear of it.
                .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal)),
        ) {
            val width = maxWidth
            val wide = isWide(width)
            val density = LocalDensity.current
            // The folds are in window px; this box starts after the left inset.
            val inset = WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal).getLeft(density, LocalLayoutDirection.current)
            val hinges = rememberVerticalHinges().map { h -> with(density) { Hinge((h.left - inset).toDp(), (h.right - inset).toDp(), h.separating) } }
            fun splitOf(k: PaneKind) = paneSplit(width, hinges, shares[k])
            // A space opened beside the list follows a fold: a pushed page on a phone's width, the right pane when wide.
            LaunchedEffect(wide) {
                val pushedSpace = routes.singleOrNull()?.takeIf { it.startsWith("space/") }
                if (!wide && paneSpace != null) { routes = listOf("space/$paneSpace"); paneSpace = null }
                else if (wide && pushedSpace != null && open == null) { paneSpace = pushedSpace.removePrefix("space/"); routes = emptyList(); go(ShellPage.Agents) }
            }
            // On a wide window an Agent's chat opens in the Agents page's right pane (list beside it, as iOS's split
            // view); anywhere else (a pushed page, 设置, a phone's width) it goes over everything.
            val chatInPane = wide && routes.isEmpty() && !settings && open != null && hub.session(open)?.assistant != true
            LaunchedEffect(chatInPane, open) { if (chatInPane) go(ShellPage.Agents) }
            val atRoot = routes.isEmpty() && !settings && (open == null || chatInPane)

            val sidebar: @Composable () -> Unit = {
                val todos by hub.todos.collectAsState()
                val todosSupported by hub.todosSupported.collectAsState()
                Sidebar(
                    conversations = model.conversations(byMachine), model = model,
                    current = model.current.takeIf { page == ShellPage.Assistant }, inboxCount = inboxAction,
                    todoCount = todos.count { !it.isDone }, showTodos = todosSupported,
                    actions = SidebarActions(
                        openProfile = { go(ShellPage.Assistant); push("profile"); drawer.closeIn(scope) },
                        openInbox = { go(ShellPage.Assistant); push("inbox"); drawer.closeIn(scope) },
                        openTodos = { go(ShellPage.Assistant); push("todos"); drawer.closeIn(scope) },
                        openRoutines = { go(ShellPage.Assistant); push("routines"); drawer.closeIn(scope) },
                        openKnowledge = { go(ShellPage.Assistant); push("knowledge"); drawer.closeIn(scope) },
                        openMachines = { go(ShellPage.Assistant); push("machines"); drawer.closeIn(scope) },
                        openSettings = { settingsStack.value = emptyList(); settings = true; drawer.closeIn(scope) },
                        newConversation = { model.openNew(); go(ShellPage.Assistant); drawer.closeIn(scope) },
                        openConversation = { id -> model.open(id); go(ShellPage.Assistant); drawer.closeIn(scope) },
                    ),
                )
            }
            val assistantPane: @Composable () -> Unit = {
                AssistantPane(
                    hub, model, focusComposer = askFocus, onFocusTaken = { askFocus = false },
                    onOpenSession = { openSession.value = it }, onOpenInbox = { push("inbox") },
                    onError = toast, onVoice = model.voicePlugin?.let { { push("voice") } },
                )
            }
            val agentsList: @Composable () -> Unit = {
                AgentsScreen(
                    // Wide, the list opens into the right pane: a chat or a space page replaces what is there.
                    hub, onOpen = { if (wide) paneSpace = null; openSession.value = it },
                    onOpenSpace = { if (wide) { paneSpace = it; openSession.value = null } else push("space/$it") },
                    options = listOptions, inShell = true, onOpenMonitor = { push("machines") },
                    selected = open.takeIf { chatInPane },
                    onOpenTodo = { push("todo/$it") },
                )
            }
            var reselect by remember { mutableStateOf<ShellPage?>(null) }
            val topBar: @Composable () -> Unit = {
                Box(Modifier.zIndex(2f)) {
                    ShellTopBar(
                        page, onPage = ::go, waiting = waiting, inboxDot = inboxAction > 0,
                        onDrawer = { if (wide) leftPane = !leftPane else drawer.openIn(scope) },
                        trailing = {
                            if (page == ShellPage.Assistant) {
                                CircleButton(JIcons.compose, "新对话", { model.openNew() }, enabled = model.current != null)
                            } else {
                                CircleButton(JIcons.compose, "新建会话", { newSession = true }, enabled = machines.any { it.online })
                            }
                        },
                        onReselect = { reselect = it },
                        solid = wide,
                        reselectMenu = {
                            DropdownMenu(reselect != null, onDismissRequest = { reselect = null }) {
                                val close = { reselect = null }
                                if (reselect == ShellPage.Assistant) AssistantMenu(model, close, onKnowledge = { push("knowledge") },
                                    onProfile = { push("profile") }, onVoice = { push("voice") })
                                else AgentsMenu(listOptions, machines, close, onMonitor = { push("machines") })
                            }
                        },
                    )
                }
            }

            if (wide) {
                // Two panes under one bar (iOS's split view on an iPad), the divider where the user dragged it.
                Box(Modifier.fillMaxSize()) {
                    ShellLayer(shown = page == ShellPage.Assistant) {
                        Panes(
                            width, splitOf(PaneKind.Assistant),
                            left = if (leftPane) ({ Box(Modifier.padding(top = JSize.shellBar)) { sidebar() } }) else null,
                            onShare = { keepShare(PaneKind.Assistant, it) }, onCollapse = { leftPane = false },
                        ) {
                            Box(Modifier.fillMaxSize().readableColumn()) { assistantPane() }
                        }
                    }
                    if (agentsBuilt) ShellLayer(shown = page == ShellPage.Agents) {
                        // With nothing open the list always shows (the right pane would be empty otherwise).
                        val detail = chatInPane || paneSpace != null
                        Panes(
                            width, splitOf(PaneKind.Agents), left = if (leftPane || !detail) agentsList else null,
                            onShare = { keepShare(PaneKind.Agents, it) }, onCollapse = if (detail) ({ leftPane = false }) else null,
                        ) {
                            val key = open
                            val space = paneSpace
                            when {
                                chatInPane && key != null -> Box(Modifier.fillMaxSize().padding(top = JSize.shellBar)) {
                                    // Opened from the space page in this pane: back returns to it.
                                    ChatScreen(hub, key, onBack = { openSession.value = null }, onHandToJarvis = handToJarvis, backButton = space != null,
                                        onCleared = { openSession.value = it }, onOpenTodo = { push("todo/$it") })
                                }
                                space != null -> Box(Modifier.fillMaxSize().padding(top = JSize.shellBar)) {
                                    CompositionLocalProvider(LocalPaneRoot provides true) {
                                        SpaceScreen(hub, space, onOpen = { openSession.value = it }, onBack = { paneSpace = null })
                                    }
                                }
                                else -> PanePlaceholder(JIcons.agents, "选一个 Agent", "点左边的一个 Agent，它的对话在这边打开。")
                            }
                        }
                    }
                    topBar()
                    ConnectionPill(Modifier.align(Alignment.TopCenter).zIndex(3f))
                }
                // Back closes the chat in the right pane first, then the space page under it.
                BackHandler(enabled = !chatInPane && paneSpace != null && page == ShellPage.Agents && routes.isEmpty() && !settings) { paneSpace = null }
                BackHandler(enabled = chatInPane) { openSession.value = null }
            } else {
                DrawerLayout(
                    drawer, enabled = atRoot,
                    sidebar = { sidebar() },
                    page = {
                        Box(Modifier.fillMaxSize().background(JTheme.colors.page)) {
                            // Both pages stay built (like tabs); the hidden one is invisible and out of reach.
                            ShellLayer(shown = page == ShellPage.Assistant) { assistantPane() }
                            if (agentsBuilt) ShellLayer(shown = page == ShellPage.Agents) { agentsList() }
                            topBar()
                            ConnectionPill(Modifier.align(Alignment.TopCenter).zIndex(3f))
                        }
                    },
                )
            }

            // Pages pushed over the shell (iOS pushes them onto 助手's stack: the drawer's go to 助手 under them).
            AnimatedContent(
                routes, label = "route", contentKey = { it.lastOrNull() },
                transitionSpec = {
                    if (targetState.size > initialState.size) slideInHorizontally(JMotion.layout()) { it } togetherWith fadeOut(tween(JMotion.LAYOUT_MS))
                    else fadeIn(tween(JMotion.STATE_MS)) togetherWith slideOutHorizontally(JMotion.layout()) { it }
                },
            ) { stack ->
                val route = stack.lastOrNull()
                if (route != null) {
                    BackHandler { pop() }
                    // Wide, the inbox is two panes: the list, and the open item's detail beside it.
                    val inboxPanes = wide && route == "inbox"
                    Box(Modifier.fillMaxSize().background(JTheme.colors.page).pointerInput(Unit) {}.then(if (inboxPanes) Modifier else Modifier.readableColumn())) {
                        if (inboxPanes) {
                            BackHandler(enabled = inboxOpen.value != null) { inboxOpen.value = null }
                            Panes(width, splitOf(PaneKind.Inbox), left = {
                                InboxScreen(hub, onBack = ::pop, onHandOff = inboxHandOff, openId = inboxOpen, detailInPane = true)
                            }, onShare = { keepShare(PaneKind.Inbox, it) }, onCollapse = null) {
                                InboxDetailPane(hub, inboxOpen.value, onClose = { inboxOpen.value = null }, onHandOff = inboxHandOff)
                            }
                        } else RouteScreen(
                            hub, route, model, back = ::pop, push = ::push,
                            openSession = { openSession.value = it }, handToJarvis = handToJarvis, inboxOpen = inboxOpen, inboxHandOff = inboxHandOff,
                            onAsk = { text -> routes = emptyList(); go(ShellPage.Assistant); model.ask(text, toast) },
                        )
                    }
                }
            }

            AnimatedVisibility(settings, enter = slideInVertically(JMotion.layout()) { it }, exit = slideOutVertically(JMotion.layout()) { it }) {
                Box(Modifier.fillMaxSize().background(JTheme.colors.page).pointerInput(Unit) {}.then(if (wide) Modifier else Modifier.readableColumn())) {
                    BackHandler(enabled = settingsStack.value.isEmpty()) { settings = false }
                    if (wide) {
                        // Wide, 设置 is two panes: the menu, and the page it opened (with what that page pushed).
                        val pop = { settingsStack.value = settingsStack.value.dropLast(1) }
                        BackHandler(enabled = settingsStack.value.isNotEmpty(), onBack = pop)
                        Panes(width, splitOf(PaneKind.Settings), left = {
                            SettingsScreen(hub, onBack = { settings = false }, stack = settingsStack, onOpenSession = { openSession.value = it }, asSheet = true, menuOnly = true)
                        }, onShare = { keepShare(PaneKind.Settings, it) }, onCollapse = null) {
                            val top = settingsStack.value.lastOrNull()?.let(SettingsRoute::of)
                            if (top == null) PanePlaceholder(JIcons.settings, "选一项设置", "左边点一项，在这边打开。", underBar = false)
                            else CompositionLocalProvider(LocalPaneRoot provides (settingsStack.value.size == 1)) {
                                SettingsPage(hub, top, push = { settingsStack.value = settingsStack.value + it.path }, pop = pop, onOpenSession = { openSession.value = it })
                            }
                        }
                    } else {
                        SettingsScreen(hub, onBack = { settings = false }, stack = settingsStack, onOpenSession = { openSession.value = it }, asSheet = true)
                    }
                }
            }

            // A chat goes over everything (opened from a machine's page or a notification, or on a phone's width):
            // back returns to where it was.
            if (open != null && !chatInPane && hub.session(open)?.assistant != true) {
                BackHandler { openSession.value = null }
                Box(Modifier.fillMaxSize().background(JTheme.colors.page).pointerInput(Unit) {}) {
                    // /clear opens the new session in its place.
                    ChatScreen(hub, open, onBack = { openSession.value = null }, onHandToJarvis = handToJarvis, onCleared = { openSession.value = it }, onOpenTodo = { push("todo/$it") })
                }
            }
            jarvis.android.ui.chat.ToastHost(Modifier.zIndex(10f))

            // Back on Agents goes to 助手 (the first page), as a second tab does on Android.
            BackHandler(enabled = atRoot && !chatInPane && page == ShellPage.Agents && !drawer.isOpen) { go(ShellPage.Assistant) }
        }
    }

    if (newSession) {
        NewSessionSheet(hub, machines.filter { it.online }, onDismiss = { newSession = false }, onStarted = { newSession = false; openSession.value = it })
    }

}

/** A wide window's empty right pane (iOS `SplitPlaceholder`): what goes here, quietly, in its middle. */
@Composable
private fun PanePlaceholder(icon: androidx.compose.ui.graphics.vector.ImageVector, title: String, message: String, underBar: Boolean = true) {
    Box(Modifier.fillMaxSize().background(JTheme.colors.page).then(if (underBar) Modifier.padding(top = JSize.shellBar) else Modifier),
        contentAlignment = Alignment.Center) {
        jarvis.android.ui.design.EmptyState(icon, title, message)
    }
}

/** A page meant to be read keeps to [JSize.readable] on a wide window, centered (iOS `ChatView.readableWidth`). */
@Composable
internal fun Modifier.readableColumn(): Modifier = this.wrapContentWidth(Alignment.CenterHorizontally).widthIn(max = JSize.readable)

/**
 * One of the shell's two pages: the shown one on top, catching every touch (so none falls through to the other);
 * the other stays built underneath, invisible and out of reach.
 */
@Composable
private fun ShellLayer(shown: Boolean, content: @Composable () -> Unit) {
    Box(
        Modifier.fillMaxSize().zIndex(if (shown) 1f else 0f)
            .graphicsLayer { alpha = if (shown) 1f else 0f }
            .then(if (shown) Modifier.pointerInput(Unit) {} else Modifier.clearAndSetSemantics { }),
    ) { content() }
}

/** What the drawer and the switch push: 收件箱, 定时任务, 知识库, 机器 (and a machine's pages), a space, the voice page. */
@Composable
private fun RouteScreen(
    hub: Hub, route: String, model: AssistantModel, back: () -> Unit, push: (String) -> Unit,
    openSession: (SessionKey) -> Unit, handToJarvis: (String) -> Unit, inboxOpen: MutableState<String?>, inboxHandOff: (HandOff) -> Unit,
    onAsk: (String) -> Unit,
) {
    val machines by hub.machines.collectAsState()
    val scope = rememberCoroutineScope()
    val head = route.substringBefore('/')
    val rest = route.substringAfter('/', "")
    when (head) {
        "inbox" -> InboxScreen(hub, onBack = back, onHandOff = inboxHandOff, openId = inboxOpen)
        "todos" -> jarvis.android.ui.TodosScreen(hub, onBack = back, onOpenTodo = { push("todo/$it") })
        "todo" -> jarvis.android.ui.TodoDetailScreen(hub, rest, onBack = back, onOpenSession = openSession)
        "profile", "routines" -> AssistantProfile(
            hub, machineName = machines.firstOrNull { it.id == model.machine }?.displayName ?: model.machine,
            // A routine's run may be a conversation the list has not seen yet: read it again.
            onBack = back, onOpenConversation = { id -> scope.launch { model.reload() }; model.open(id); back() },
            start = if (head == "routines") AssistantPage.Routines else AssistantPage.Profile,
        )
        "knowledge" -> KnowledgeScreen(hub, onBack = back, onAsk = onAsk, onOpenSession = openSession)
        "machines" -> MachinesScreen(hub, back, onOpenMachine = { push("machine/$it") })
        "machine" -> MachineDetailScreen(
            hub, rest, back, onOpenSession = openSession, onOpenMonitor = { push("monitor/$rest") }, onOpenEngine = { push("engine/$rest") },
        )
        "engine" -> jarvis.android.ui.machines.EngineScreen(hub, rest, onDone = back)
        "monitor" -> MachineMonitorScreen(hub, rest, back)
        "space" -> SpaceScreen(hub, rest, onOpen = openSession, onBack = back)
        "voice" -> {
            val plugin = model.voicePlugin
            if (plugin != null) VoiceScreen(hub, plugin, onBack = back) else LaunchedEffect(Unit) { back() }
        }
        else -> LaunchedEffect(route) { back() }
    }
}

/** 助手's menu (tapping 助手 while it is selected; iOS: the face's menu). */
@Composable
private fun AssistantMenu(model: AssistantModel, close: () -> Unit, onKnowledge: () -> Unit, onProfile: () -> Unit, onVoice: () -> Unit) {
    val ctx = LocalContext.current
    DropdownMenuItem({ Text("知识库", maxLines = 1) }, { close(); onKnowledge() }, leadingIcon = { Icon(JIcons.knowledge, null) })
    DropdownMenuItem({ Text("认识 Jarvis", maxLines = 1) }, { close(); onProfile() }, leadingIcon = { Icon(JIcons.ai, null) })
    if (model.voicePlugin != null) DropdownMenuItem({ Text("语音对话", maxLines = 1) }, { close(); onVoice() }, leadingIcon = { Icon(JIcons.dictate, null) })
    model.current?.let { id ->
        HorizontalDivider()
        DropdownMenuItem({ Text("拷贝会话 ID", maxLines = 1) }, {
            close(); jarvis.android.ui.media.MediaShare.copyText(ctx, id, "会话 ID")
        }, leadingIcon = { Icon(JIcons.copy, null) })
    }
}

/** The list's options (tapping Agents while it is selected; iOS `AgentsListMenu`). */
@Composable
private fun AgentsMenu(o: AgentsListOptions, machines: List<jarvis.android.data.Machine>, close: () -> Unit, onMonitor: () -> Unit) {
    jarvis.android.data.Grouping.entries.forEach { g ->
        DropdownMenuItem({ Text(g.title, maxLines = 1) }, { close(); o.pick(g) },
            leadingIcon = { Icon(if (o.grouping == g) JIcons.done else g.icon(), null) })
    }
    HorizontalDivider()
    DropdownMenuItem({ Text("全部折叠", maxLines = 1) }, { close(); o.foldAll() }, leadingIcon = { Icon(JIcons.collapseAll, null) })
    DropdownMenuItem({ Text("全部展开", maxLines = 1) }, { close(); o.unfoldAll() }, leadingIcon = { Icon(JIcons.expandAll, null) })
    if (machines.size > 1) {
        HorizontalDivider()
        DropdownMenuItem({ Text("全部电脑", maxLines = 1) }, { close(); o.pickMachine(null) },
            leadingIcon = { Icon(if (o.machineFilter == null) JIcons.done else JIcons.machines, null) })
        machines.forEach { m ->
            DropdownMenuItem({ Text(m.displayName + if (m.online) "" else "（离线）", maxLines = 1) }, { close(); o.pickMachine(m.id) },
                leadingIcon = { Icon(if (o.machineFilter == m.id) JIcons.done else JIcons.machine, null) })
        }
    }
    HorizontalDivider()
    DropdownMenuItem({ Text("机器监控", maxLines = 1) }, { close(); onMonitor() }, leadingIcon = { Icon(JIcons.cpu, null) })
}

private fun jarvis.android.data.Grouping.icon() = when (this) {
    jarvis.android.data.Grouping.Space -> JIcons.space
    jarvis.android.data.Grouping.State -> JIcons.notifications
    jarvis.android.data.Grouping.Machine -> JIcons.machine
    jarvis.android.data.Grouping.Todo -> JIcons.todo
}

/** Weak network: one small pill under the top bar for both pages (docs/ios.md「连接状态」). */
@Composable
private fun ConnectionPill(modifier: Modifier) {
    val label by ConnectionWatch.label.collectAsState()
    val l = label ?: return
    Row(
        modifier.statusBarsPadding().padding(top = JSize.shellBar).height(JSize.icon).surface(CircleShape, floating = true).padding(horizontal = JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (l != "需要重新配对") CircularProgressIndicator(Modifier.size(JSize.markXS), strokeWidth = JSize.lineMedium, color = JTheme.colors.secondaryText)
        Text("  $l", style = JTheme.type.labelStrong, color = JTheme.colors.secondaryText, maxLines = 1)
    }
}
