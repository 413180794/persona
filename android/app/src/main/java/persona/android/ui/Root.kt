@file:OptIn(androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi::class)

package persona.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.navigation3.rememberViewModelStoreNavEntryDecorator
import androidx.navigation3.runtime.*
import androidx.navigation3.ui.NavDisplay
import androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfo
import androidx.compose.material3.adaptive.layout.PaneScaffoldDirective
import androidx.compose.material3.adaptive.layout.calculatePaneScaffoldDirective
import androidx.compose.material3.adaptive.layout.calculatePaneScaffoldDirectiveWithTwoPanesOnMediumWidth
import androidx.compose.material3.adaptive.navigation3.ListDetailSceneStrategy
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.window.core.layout.WindowSizeClass
import androidx.navigationevent.compose.LocalNavigationEventDispatcherOwner
import androidx.navigationevent.compose.rememberNavigationEventDispatcherOwner
import androidx.compose.animation.ContentTransform
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.ui.layout.layout
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInWindow
import persona.android.MainActivity
import persona.android.data.Hub
import persona.android.navigation.*
import persona.core.contracts.*
import persona.core.contracts.api.ConnectionState
import dev.chrisbanes.haze.HazeState
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import androidx.compose.foundation.background
import persona.core.domain.inbox.buildInboxList
import persona.core.navigation.*
import persona.feature.agents.agentsEntries
import persona.feature.agents.spaceEntries
import persona.feature.agents.AgentsDependencies
import persona.feature.agents.AgentsPreferencesSnapshot
import persona.feature.agents.AgentsWiring
import persona.feature.assistant.AssistantScreen
import persona.feature.assistant.assistantEntries
import persona.feature.chat.*
import persona.feature.inbox.InboxRepository
import persona.feature.inbox.inboxEntries
import persona.feature.knowledge.*
import persona.feature.machines.machinesEntries
import persona.feature.settings.*
import persona.feature.terminal.terminalEntries
import persona.feature.voice.voiceEntries
import persona.platform.notify.Notify
import persona.platform.phonetools.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Composable
fun PersonaRoot(
    hub: Hub?,
    openNavigation: MutableState<((Router) -> ResolvedRoute?)?>,
    services: SettingsServices,
    shell: AppServices,
    onPaired: () -> Unit = {},
) {
    val session by shell.session.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var diagnostics by remember { mutableStateOf(false) }
    // First pairing keeps the pairing flow, including its readiness checklist, until it reports ready.
    var entering by remember { mutableStateOf(hub == null) }
    LaunchedEffect(hub, session) {
        if (session?.demo != null) entering = false
        else if (hub == null) entering = true
    }
    val link = MainActivity.pendingPairLink.value
    LaunchedEffect(shell) {
        shell.feedback.notices.collect { notice ->
            snackbar.currentSnackbarData?.dismiss()
            // A notice with 撤销 waits its own few seconds; collecting the next one dismisses it, as for the others.
            launch {
                val result = snackbar.showSnackbar(notice.message, actionLabel = notice.action?.label, duration = SnackbarDuration.Short)
                if (result == SnackbarResult.ActionPerformed) notice.action?.run?.invoke()
            }
        }
    }
    CompositionLocalProvider(LocalAppFeedback provides shell.feedback) {
        Surface(Modifier.fillMaxSize()) {
            Box(Modifier.fillMaxSize()) {
                val active = session
                if (link != null || active == null && hub == null || entering && !shell.automation) {
                    val router = remember { Router() }
                    LaunchedEffect(openNavigation.value, active) {
                        openNavigation.value?.invoke(router)?.let { route ->
                            if (route.key == SettingsPageRoute(SettingsPage.Diagnostics)) diagnostics = true
                            if (route.key == PairingRoute || route.key is SettingsPageRoute) openNavigation.value = null
                        }
                    }
                    BackHandler(diagnostics || link != null && hub != null) {
                        if (diagnostics) diagnostics = false else MainActivity.pendingPairLink.value = null
                    }
                    if (diagnostics) SettingsScreen(services, initialPage = "diag")
                    // The pairing page scrolls by itself; the demo entry sits in its footer.
                    else PairingScreen(services.pairing, services.sessions, initialLink = link,
                        onReady = { MainActivity.pendingPairLink.value = null; entering = false; onPaired() },
                        onDiagnostics = { diagnostics = true }, diagnostics = services.diagnostics,
                        onCredentialsSaved = { MainActivity.pendingPairLink.value = null }, machines = services.machines,
                        footer = {
                            if (hub == null) JTextButton("没有 hub？先体验一下", onClick = { scope.launch {
                                try { shell.enterDemo(); MainActivity.pendingPairLink.value = null }
                                catch (error: Exception) {
                                    android.util.Log.w("JarvisShell", "Unable to start demo", error)
                                    shell.feedback.failure(error)
                                }
                            } })
                        })
                } else if (active == null) {
                    // The pairing is restored; its repositories are still being assembled.
                    Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background), contentAlignment = Alignment.Center) { JLoading() }
                } else key(active.identity) {
                    val displayed = LocalDeveloperTools.current?.session(active) ?: active
                    key(displayed.identity) {
                        // Demo mode has only the default face (docs/persona.md), like iOS.
                        val demo = displayed.demo != null
                        CompositionLocalProvider(
                            persona.core.designsystem.LocalPersonaDesign provides if (demo) persona.core.designsystem.PersonaDesign.Default else persona.core.designsystem.LocalPersonaDesign.current,
                            persona.core.designsystem.LocalPersonaPack provides if (demo) null else persona.core.designsystem.LocalPersonaPack.current,
                            persona.core.designsystem.LocalPersonaName provides if (demo) "Persona" else persona.core.designsystem.LocalPersonaName.current,
                        ) {
                            PersonaShell(displayed, openNavigation, services.copy(connection = displayed.connection, sessions = displayed.sessions, demo = displayed.demo != null), shell)
                        }
                    }
                    active.phone?.let { PhoneConfirmationHost(it.confirmations) }
                }
                SnackbarHost(snackbar, Modifier.align(Alignment.TopCenter).statusBarsPadding().padding(12.dp))
            }
        }
    }
}

@Composable
private fun PersonaShell(session: ShellSession, openNavigation: MutableState<((Router) -> ResolvedRoute?)?>, services: SettingsServices, shell: AppServices) {
    val context = LocalContext.current
    val store = remember(session.identity) { ShellPositionStore(context, session.identity) }
    // Automated captures start from each tab's root instead of whatever the previous capture left open;
    // they still save, since scripts/android.sh waits for the saved position to know the shell is up.
    val saved = remember(store) { if (shell.automation) ShellPosition() else store.load() }
    // With the embedded assistant chat (not in demo), assistant conversations open inside the Persona home.
    val embeddedAssistant = session.demo == null
    val initialLaunch = remember { openNavigation.value }
    val initialRoute = remember {
        initialLaunch?.invoke(Router { m, s ->
            val key = SessionKey(m, s)
            val assistant = session.assistant.assistant.value
            if (key == assistant.mainSession || assistant.conversations.any { it.key == key }) ConversationKind.Assistant
            else session.sessions.sessions.value.firstOrNull { it.key == key }?.let {
                if (it.assistant) ConversationKind.Assistant else ConversationKind.Agent
            }
        })
    }
    var selected by rememberSaveable { mutableStateOf(initialRoute?.tab ?: if (session.demo != null) AppTab.Jarvis else saved.tab) }
    var machine by rememberSaveable { mutableStateOf(saved.machine) }
    val assistantStack = rememberNavBackStack(AssistantRoute,
        *listOfNotNull(saved.chats[AppTab.Jarvis]?.takeIf { !embeddedAssistant }).toTypedArray())
    val agentsStack = rememberNavBackStack(AgentsRoute, *listOfNotNull(saved.chats[AppTab.Agents]).toTypedArray())
    val inboxStack = rememberNavBackStack(InboxRoute)
    val settingsStack = rememberNavBackStack(SettingsRoute)
    val stacks = mapOf(AppTab.Jarvis to assistantStack, AppTab.Agents to agentsStack, AppTab.Inbox to inboxStack, AppTab.Settings to settingsStack)
    val sessions by session.sessions.sessions.collectAsStateWithLifecycle()
    val assistant by session.assistant.assistant.collectAsStateWithLifecycle()
    val pending by session.interactions.pending.collectAsStateWithLifecycle()
    val inbox by session.inbox.snapshot.collectAsStateWithLifecycle()
    val connection by session.connection.state.collectAsStateWithLifecycle()
    val pins by session.assistant.pinned.collectAsStateWithLifecycle()
    val preferences by session.preferences.snapshots.collectAsStateWithLifecycle(AgentsPreferencesSnapshot())
    fun kind(key: SessionKey): ConversationKind? = when {
        key == assistant.mainSession || assistant.conversations.any { it.key == key } -> ConversationKind.Assistant
        else -> sessions.firstOrNull { it.key == key }?.let { if (it.assistant) ConversationKind.Assistant else ConversationKind.Agent }
    }
    val currentKind by rememberUpdatedState<(SessionKey) -> ConversationKind?>(::kind)
    var assistantRequest by remember { mutableStateOf<SessionKey?>(null) }
    var assistantPending by remember { mutableStateOf<SessionKey?>(null) }
    fun assistantTarget(key: NavKey): SessionKey? {
        if (!embeddedAssistant) return null
        val chat = when (key) { is ChatRoute -> key; is InteractionRoute -> key.chat; else -> null } ?: return null
        val conversation = SessionKey(chat.machineId, chat.sessionId)
        return conversation.takeIf { (chat.kind ?: currentKind(conversation)) == ConversationKind.Assistant }
    }
    /** Show an assistant conversation in the Persona home rather than as a second chat page above it. */
    fun openAssistant(key: NavKey, select: Boolean): Boolean {
        val conversation = assistantTarget(key) ?: return false
        if (select) {
            val stack = stacks.getValue(AppTab.Jarvis)
            while (stack.size > 1) stack.removeAt(stack.lastIndex)
            selected = AppTab.Jarvis
        }
        assistantPending = conversation
        return true
    }
    fun openResolved(route: ResolvedRoute): AppTab =
        if (openAssistant(route.key, true)) AppTab.Jarvis else route.open(stacks)
    remember {
        initialRoute?.let { route ->
            // The first frame already shows the requested conversation, not the home's default one.
            assistantTarget(route.key)?.let { assistantRequest = it } ?: route.open(stacks)
            route.pairingLink?.let { MainActivity.pendingPairLink.value = it }
            if (openNavigation.value === initialLaunch) openNavigation.value = null
        }
        true
    }
    LaunchedEffect(assistantPending) {
        val conversation = assistantPending ?: return@LaunchedEffect
        // Asking again for the conversation already requested must still reopen it in the home.
        if (assistantRequest == conversation) { assistantRequest = null; withFrameNanos { } }
        assistantRequest = conversation
        assistantPending = null
    }
    val navigator = remember { AppNavigator(stacks, { selected }, { selected = it }, chatTab = { route ->
        currentKind(SessionKey(route.machineId, route.sessionId))?.let { if (it == ConversationKind.Assistant) AppTab.Jarvis else AppTab.Agents }
    }, openAssistant = { chat, select -> openAssistant(chat, select) }) }
    val router = remember { Router { m, s -> currentKind(SessionKey(m, s)) } }
    val agentsWiring by session.agentsWiring.state.collectAsStateWithLifecycle()
    val navigationWiring = remember { MutableStateFlow(agentsWiring) }
    SideEffect {
        navigationWiring.value = agentsWiring.copy(operations = agentsWiring.operations?.let { bindings ->
            bindings.copy(onSessionClosed = { key -> bindings.onSessionClosed(key); navigator.sessionClosed(key) })
        })
    }
    val agents = remember(session, navigator) {
        object : AgentsDependencies by session.agents {
            override fun wiring() = object : AgentsWiring { override val state = navigationWiring }
        }
    }
    val assistantNavigator = remember {
        object : Navigator by navigator {
            override fun navigate(key: NavKey) {
                // Ignore late automatic navigation from an assistant entry that is leaving the screen.
                if (selected == AppTab.Jarvis) navigator.navigate(key)
            }
        }
    }
    LaunchedEffect(sessions, assistant, openNavigation.value) {
        navigator.restoreChatTabs()
        openNavigation.value?.invoke(router)?.let {
            selected = openResolved(it)
            it.pairingLink?.let { link -> MainActivity.pendingPairLink.value = link }
            openNavigation.value = null
        }
    }
    LaunchedEffect(store) {
        snapshotFlow { ShellPosition(selected, machine, stacks.mapNotNull { (tab, stack) ->
            stack.mapNotNull { when (it) { is ChatRoute -> it; is InteractionRoute -> it.chat; else -> null } }
                .lastOrNull()?.let { tab to it }
        }.toMap()) }.distinctUntilChanged().collect { position -> withContext(Dispatchers.IO) { store.save(position) } }
    }
    val visible = stacks.getValue(selected).lastOrNull()
    val visibleChat = when (visible) { is ChatRoute -> visible; is InteractionRoute -> visible.chat; else -> null }
    DisposableEffect(visibleChat, session.demo) {
        if (session.demo == null) Notify.viewing(visibleChat?.let { SessionKey(it.machineId, it.sessionId) })
        onDispose { if (session.demo == null) Notify.viewing(null) }
    }
    /** Swap a placeholder destination (an inbox deep link) for the page that shows it, keeping its position. */
    fun replace(from: NavKey, to: NavKey) {
        stacks.values.forEach { stack -> stack.indexOf(from).takeIf { it >= 0 }?.let { stack[it] = to } }
    }
    val tools = LocalDeveloperTools.current
    val decorators = AppTab.entries.associateWith { tab -> key(tab) {
        listOf(rememberSaveableStateHolderNavEntryDecorator<NavKey>(), rememberViewModelStoreNavEntryDecorator<NavKey>())
    } }
    val assistantRequests = pending.filter { currentKind(SessionKey(it.machineId, it.sessionId)) == ConversationKind.Assistant }
    val agentRequests = pending - assistantRequests.toSet()
    val agentNeeds = agentRequests.count { request ->
        val key = SessionKey(request.machineId, request.sessionId)
        val space = sessions.firstOrNull { it.key == key }?.spaceName?.let { "${key.machineId}/$it" }
        !preferences.mutes.mutes(key.path, space)
    }
    // Only when what the count reads changed: the shell recomposes on every push, and the whole list is built to count
    // one group. Of the sessions it reads only which space each is in (for mutes), not their state or activity.
    val inboxSpaces = inbox.sessions.map { it.id to it.spacePreferenceKey }
    val inboxNeeds = remember(inbox.records, inbox.interactions, inboxSpaces, inbox.mutes) {
        buildInboxList(inbox.records.map { it.item }, inbox.interactions, inbox.sessions, inbox.mutes)
            .groups.firstOrNull { it.id == "action" }?.count ?: 0
    }
    val badges = mapOf(AppTab.Agents to agentNeeds, AppTab.Jarvis to assistantRequests.size, AppTab.Inbox to inboxNeeds)
    // 现在要你处理的事 on the Persona home: the agents waiting (not Persona's own, not muted), then the inbox's 需要你.
    val needsYou = remember(agentRequests, sessions, preferences.mutes, inbox.records) {
        persona.feature.assistant.needsYou(agentRequests, sessions, preferences.mutes,
            inbox.records.map { persona.feature.assistant.NeedsYouMessage(it.item, lark = it.feedId != null) })
    }
    // 语音对话 is offered while the hub runs a voice plugin: asked each time the Persona tab shows while connected.
    val voicePlugin = persona.feature.voice.rememberVoicePlugin(session.voice,
        active = selected == AppTab.Jarvis && connection == ConnectionState.Online)
    // Links over the whole shell, jarvis:// links from conversations, 交给 Persona from pages (ShellLinks.kt).
    ShellLinkHost(session, navigator, shell.feedback) {
        val uriHandler = LocalUriHandler.current
        // Each page shows the connection state in its own title, so the shell adds no banner above it.
        val style = PersonaTheme.style
        val connected = connection == ConnectionState.Online
        val working = sessions.any { it.agentState == AgentState.Working }
        val ambience = when {
            !connected && connection != ConnectionState.Connecting && connection != ConnectionState.Updating -> Ambience.Alert
            (badges[AppTab.Agents] ?: 0) + (badges[AppTab.Inbox] ?: 0) > 0 -> Ambience.NeedsYou
            working -> Ambience.Working
            else -> Ambience.Calm
        }
        val demoBadge = if (session.demo != null) "体验" else null
        val items = navigationItems(badges, demoBadge)
        // The window decides the layout (WindowSizeClass and the fold posture), never the device model: split screen,
        // free-form windows, rotation and folding all just recompose this.
        val windowSize = LocalWindowInfo.current.containerSize
        val adaptive = currentWindowAdaptiveInfo().let { real -> shell.posture.value?.let { pretendFold(real, it, windowSize) } ?: real }
        val expanded = adaptive.windowSizeClass.isWidthAtLeastBreakpoint(WindowSizeClass.WIDTH_DP_EXPANDED_LOWER_BOUND)
        val hinge = adaptive.windowPosture.hingeList.firstOrNull { !it.isVertical && it.isSeparating }
        val windowLayout = WindowLayout(expanded = expanded, tabletop = adaptive.windowPosture.isTabletop && hinge != null,
            hingeTop = hinge?.bounds?.top ?: 0f, hingeBottom = hinge?.bounds?.bottom ?: 0f)
        val windowWidth = with(LocalDensity.current) { LocalWindowInfo.current.containerSize.width.toDp() }
        // Lists sit beside their details when expanded, and on medium widths with room for both (a tablet in portrait).
        // Panes share the window's one background; between them only a little space, no second surface or rule.
        // The scaffold must not move focus into the pane it shows (its default): focus entered the list pane's first
        // focusable, the search field, and with a hardware keyboard Agents and 收件箱 opened with the input method up.
        val directive = (if (!expanded && windowWidth >= 760.dp) calculatePaneScaffoldDirectiveWithTwoPanesOnMediumWidth(adaptive)
            else calculatePaneScaffoldDirective(adaptive)).let {
                PaneScaffoldDirective(
                    maxHorizontalPartitions = it.maxHorizontalPartitions,
                    horizontalPartitionSpacerSize = minOf(it.horizontalPartitionSpacerSize, 8.dp),
                    maxVerticalPartitions = it.maxVerticalPartitions,
                    verticalPartitionSpacerSize = it.verticalPartitionSpacerSize,
                    defaultPanePreferredWidth = it.defaultPanePreferredWidth,
                    defaultPanePreferredHeight = it.defaultPanePreferredHeight,
                    excludedBounds = it.excludedBounds,
                    shouldAutoFocusCurrentDestination = false,
                )
            }
        val wide = expanded || directive.maxHorizontalPartitions > 1
        // Each tab's list-detail scene (rememberShellListDetail) keeps its own divider. Back there closes the detail and
        // leaves the list with its placeholder: the default (pop until the panes change) emptied the whole stack in two
        // panes, where the placeholder keeps the panes as they were, and back left the app.
        val paneStore = remember { PaneSplitStore(context) }
        var paneAreaLeft by remember { mutableFloatStateOf(0f) }
        val paneDensity = LocalDensity.current.density
        // The vertical folds across the panes' area, in dp from its left edge: snap points for the divider, or (when
        // the two sides are apart) where the split is fixed.
        val paneHinges = adaptive.windowPosture.hingeList.filter { it.isVertical }.map { hinge ->
            PaneHinge((hinge.bounds.left - paneAreaLeft) / paneDensity, (hinge.bounds.right - paneAreaLeft) / paneDensity, hinge.isSeparating)
        }
        val expandedState = rememberUpdatedState(expanded)
        val navigationInset = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()
        val dockInsets = if (style == AppStyle.Glass) ChromeInsets(bottom = DockHeight + 10.dp + navigationInset, handlesNavigationBar = true)
            else ChromeInsets(handlesNavigationBar = true)
        val dock = rememberUpdatedState<@Composable (Modifier) -> Unit> { modifier ->
            AppNavigation(items, selected.name, onSelect = { key -> navigator.selectTab(AppTab.valueOf(key)) }, modifier)
        }
        val rootKeys: Set<Any> = remember { listOf<NavKey>(AssistantRoute, AgentsRoute, InboxRoute, SettingsRoute).map { NavEntry(it) {}.contentKey }.toSet() }
        // Root pages carry the dock themselves, so it moves with them in push and pop transitions and never shows
        // over a conversation; pushed pages get neither the dock nor room for it. Every page samples its own content
        // for glass, so a page never blurs what another page drew.
        val chrome = remember(style, dockInsets) {
            NavEntryDecorator<NavKey> { entry ->
                val glass = remember { HazeState() }
                // A web page shown over this page (the link card): glass must not sample its WebView, so the page goes
                // opaque while it shows, and the dock makes way for the card's bottom (LocalWebSheets).
                val webSheets = remember { mutableIntStateOf(0) }
                val web = webSheets.intValue > 0
                val level = LocalGlassLevel.current
                CompositionLocalProvider(LocalGlassSource provides glass, LocalWebSheets provides webSheets,
                    LocalGlassLevel provides if (web) GlassLevel.Solid else level) {
                    when {
                        entry.contentKey !in rootKeys || expandedState.value -> entry.Content()
                        style == AppStyle.Glass -> Box(Modifier.fillMaxSize()) {
                            // Without the dock the page keeps no room for it (a conversation over the card would float
                            // a dock's height above it).
                            CompositionLocalProvider(LocalChromeInsets provides if (web) ChromeInsets() else dockInsets) { entry.Content() }
                            if (!web) dock.value(Modifier.align(Alignment.BottomCenter))
                        }
                        else -> Column(Modifier.fillMaxSize().background(PersonaTheme.palette.background)) {
                            Box(Modifier.weight(1f)) { CompositionLocalProvider(LocalChromeInsets provides dockInsets) { entry.Content() } }
                            dock.value(Modifier)
                        }
                    }
                }
            }
        }
        val provider = entryProvider<NavKey> {
            assistantEntries(navigator) {
                AssistantScreen(assistantNavigator, session.assistant, session.sender, session.assistant::startConversation,
                    pins, connectionLabel = connection.title(),
                    hubOnline = connection == ConnectionState.Online || connection == ConnectionState.Updating, demo = session.demo != null,
                    requestedSession = assistantRequest, onKnowledge = { navigator.navigate(KnowledgeRoute) },
                    needsYou = needsYou, onOpenNeed = { line -> selected = openResolved(needsYouRoute(line)) },
                    onOpenInbox = { navigator.selectTab(AppTab.Inbox) },
                    onTogglePin = { key -> session.preferences.togglePin(key.path) },
                    onVoice = voicePlugin?.let { plugin -> { navigator.navigate(VoiceRoute(plugin.id, plugin.name)) } })
            }
            agentsEntries(navigator, agents, machine, { machine = it })
            spaceEntries(navigator, agents)
            chatEntries(navigator) { ShellChat(it, session, navigator, shell.feedback, onSessionClosed = navigator::sessionClosed) }
            entry<InteractionRoute> { ShellChat(it.chat, session, navigator, shell.feedback, it.requestId, navigator::sessionClosed) }
            inboxEntries(navigator, session.inbox)
            entry<InboxItemRoute> { InboxLink(it, session.inbox, navigator, ::replace) }
            entry<LarkChatRoute> { InboxLink(it, session.inbox, navigator, ::replace) }
            entry<SettingsRoute> {
                SettingsWithTools(if (shell.automation) null else tools, navigator) { ShellSettings(services, navigator, shell) }
            }
            entry<SettingsPageRoute> { route ->
                if (route.page == SettingsPage.PhoneData && session.phone != null) PhoneToolsScreen(session.phone, navigator)
                else if (route.page == SettingsPage.Assistant) persona.feature.assistant.settings.AssistantSettingsDestination(navigator, navigator::back)
                else ShellSettings(services, navigator, shell, route.page.toInitialPage())
            }
            entry<PairingRoute> { ShellSettings(services, navigator, shell, "pair") }
            entry<PhoneToolsRoute> { session.phone?.let { PhoneToolsScreen(it, navigator) }
                ?: UnavailablePage("当前模式没有可用的手机工具", navigator) }
            session.terminal?.let { terminalEntries(navigator, it) { key -> sessions.firstOrNull { s -> s.key == key }?.displayTitle } }
                ?: run { entry<TerminalRoute> { UnavailablePage("体验模式没有终端数据", navigator) } }
            session.metrics?.let { machinesEntries(navigator, it, session.sessions, session.machines) } ?: run {
                entry<MachinesRoute> { UnavailablePage("体验模式没有机器监控数据", navigator) }
                entry<MachineDetailRoute> { UnavailablePage("体验模式没有机器监控数据", navigator) }
            }
            session.knowledge?.let { knowledgeEntries(navigator, it, session.assistant, session.sender, uriHandler::openUri,
                onNewAgent = { start -> navigator.navigate(NewAgentRoute(start.machineId, start.cwd, start.prompt)) }) } ?: run {
                entry<KnowledgeRoute> { UnavailablePage("体验模式没有知识库数据", navigator) }
                entry<KnowledgeDetailRoute> { UnavailablePage("体验模式没有知识库数据", navigator) }
            }
            settingsDetailEntries(navigator, services)
            session.voice?.let { voiceEntries(navigator, it, session.identity, onModelConfiguration = { purpose -> navigator.navigate(ModelPurposeRoute(purpose)) }) } ?: run { entry<VoiceRoute> { UnavailablePage("体验模式没有语音对话", navigator) } }
            tools?.run { register(navigator) }
        }
        val panes: (NavKey) -> NavEntry<NavKey> = { key ->
            val entry = provider(key)
            NavEntry(key, entry.contentKey, entry.metadata + paneMetadata(key)) { entry.Content() }
        }
        // Tabs stay composed once visited: switching back shows the page as it was instead of rebuilding it (which
        // flashed empty and loading states). A hidden tab is measured but never placed or drawn, and cannot take back.
        val visited = remember { mutableStateListOf<AppTab>() }
        LaunchedEffect(selected) { if (selected !in visited) visited += selected }
        // Back on the first page of Agents, 收件箱 or 设置 goes to Persona; back on the Persona home leaves the app
        // (2026-11-01, the user's call; it used to go back through the tabs shown before).
        BackHandler(selected != AppTab.Jarvis && stacks.getValue(selected).size <= 1) { navigator.selectTab(AppTab.Jarvis) }
        // Turned on its side, a phone's camera cutout or its three-button navigation bar sits on the left or right: the
        // shell keeps clear of them, while the background still runs under them (2026-11-02, the old app's shell did so).
        val sides = WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal)
        val sideRoom = sides.getLeft(LocalDensity.current, LocalLayoutDirection.current) > 0 ||
            sides.getRight(LocalDensity.current, LocalLayoutDirection.current) > 0
        CompositionLocalProvider(LocalAmbience provides ambience, LocalWindowLayout provides windowLayout) {
          Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background)) {
            // One background for the window. Pages paint the same light at the same place (PageBackdrop), so in wide
            // windows the rail, the panes and the room between them read as one surface; cards are what separate them.
            // The strips beside a cutout or a side navigation bar show it too.
            if (wide || sideRoom) PageBackdrop()
            Row(Modifier.fillMaxSize().windowInsetsPadding(sides)) {
              if (expanded) AppNavigationRail(items, selected.name, onSelect = { key -> navigator.selectTab(AppTab.valueOf(key)) })
              BoxWithConstraints(Modifier.weight(1f).fillMaxHeight().onGloballyPositioned { paneAreaLeft = it.positionInWindow().x }) {
                AppTab.entries.forEach { tab ->
                    if (tab == selected || tab in visited) key(tab) {
                        val shown = tab == selected
                        val back = rememberNavigationEventDispatcherOwner(enabled = shown)
                        // The row of what the detail pane shows is marked in the list, however the page was opened.
                        val detail = detailOf(stacks.getValue(tab))
                        // Agents may close its list beside an open conversation; 收件箱 and 设置 keep theirs (as the old app did).
                        val listDetail = rememberShellListDetail(tab, directive, maxWidth, paneHinges, detailOpen = detail != null,
                            store = paneStore, collapsible = tab == AppTab.Agents)
                        CompositionLocalProvider(LocalNavigationEventDispatcherOwner provides back, LocalDetailKey provides detail) {
                            NavDisplay(backStack = stacks.getValue(tab), modifier = Modifier.fillMaxSize().then(if (shown) Modifier else Modifier.unplaced()),
                                onBack = navigator::back, entryDecorators = decorators.getValue(tab) + chrome,
                                sceneStrategies = listOf(listDetail),
                                // A pushed page slides in over the page it came from, which stays drawn underneath; going
                                // back, it slides away over the page below. Pages stay opaque the whole way (fading them
                                // showed both pages' text on top of each other) and nothing in between shows the background.
                                transitionSpec = { ContentTransform(slideInHorizontally(tween(300, easing = FastOutSlowInEasing)) { width -> width },
                                    slideOutHorizontally(tween(300, easing = FastOutSlowInEasing)) { width -> -width / 4 }) },
                                popTransitionSpec = { ContentTransform(slideInHorizontally(tween(260, easing = FastOutSlowInEasing)) { width -> -width / 4 },
                                    slideOutHorizontally(tween(260, easing = FastOutSlowInEasing)) { width -> width }, targetContentZIndex = -1f) },
                                predictivePopTransitionSpec = { _ -> ContentTransform(slideInHorizontally(tween(260)) { width -> -width / 4 },
                                    slideOutHorizontally(tween(260)) { width -> width }, targetContentZIndex = -1f) },
                                entryProvider = panes)
                        }
                    }
                }
              }
            }
          }
        }
    }
}

/**
 * A fold where the window's middle is, for captures on emulators whose system image reports no FoldingFeature
 * (debug launches only): "tabletop" is a horizontal half-open hinge, "book" a vertical one.
 */
private fun pretendFold(real: androidx.compose.material3.adaptive.WindowAdaptiveInfo, posture: String, size: androidx.compose.ui.unit.IntSize): androidx.compose.material3.adaptive.WindowAdaptiveInfo {
    val tabletop = posture == "tabletop"
    val bounds = if (tabletop) androidx.compose.ui.geometry.Rect(0f, size.height / 2f - 1f, size.width.toFloat(), size.height / 2f + 1f)
        else androidx.compose.ui.geometry.Rect(size.width / 2f - 1f, 0f, size.width / 2f + 1f, size.height.toFloat())
    val hinge = androidx.compose.material3.adaptive.HingeInfo(bounds, isFlat = false, isVertical = !tabletop, isSeparating = true, isOccluding = false)
    return androidx.compose.material3.adaptive.WindowAdaptiveInfo(real.windowSizeClass, androidx.compose.material3.adaptive.Posture(isTabletop = tabletop, hingeList = listOf(hinge)))
}

/**
 * Which pane each destination takes when the window has room for two: the first page of Agents, 收件箱 and 设置 is
 * a list, what they open is its detail. Everything else (the Persona home, its knowledge base) stays one page.
 */
@OptIn(ExperimentalMaterial3AdaptiveApi::class)
private fun paneMetadata(key: NavKey): Map<String, Any> = when (key) {
    AgentsRoute -> ListDetailSceneStrategy.listPane(detailPlaceholder = {
        DetailPlaceholder("选一个会话", "左边的会话和终端会在这里打开。", PhIcons.ChatsCircle)
    })
    InboxRoute -> ListDetailSceneStrategy.listPane(detailPlaceholder = {
        DetailPlaceholder("选一条消息", "飞书会话和邮件会在这里打开。", PhIcons.Tray)
    })
    SettingsRoute -> ListDetailSceneStrategy.listPane(detailPlaceholder = {
        DetailPlaceholder("设置", "选左边的一项，在这里打开。", PhIcons.GearSix)
    })
    else -> if (isDetailDestination(key)) ListDetailSceneStrategy.detailPane() else emptyMap()
}

/** Placed nowhere: measured and kept composed, never drawn and never hit. */
private fun Modifier.unplaced(): Modifier = layout { measurable, constraints ->
    val placeable = measurable.measure(constraints)
    layout(placeable.width, placeable.height) {}
}

/** The four destinations, in the same order and with labels in every style. */
private fun navigationItems(badges: Map<AppTab, Int>, demo: String?): List<NavItem> {
    fun item(tab: AppTab, label: String, icon: persona.core.designsystem.icons.PhIcon) =
        NavItem(tab.name, label, icon, badge = badges[tab] ?: 0, badgeText = if (tab == AppTab.Settings) demo else null)
    return listOf(
        item(AppTab.Jarvis, "Persona", PhIcons.Sparkle),
        item(AppTab.Agents, "Agents", PhIcons.SquaresFour),
        item(AppTab.Inbox, "收件箱", PhIcons.Tray),
        item(AppTab.Settings, "设置", PhIcons.GearSix),
    )
}

@Composable
private fun ShellSettings(services: SettingsServices, navigator: Navigator, shell: AppServices, page: String = "settings") {
    // Every settings page is its own destination, so a wide window shows it beside the list of settings.
    SettingsScreen(services, initialPage = page, onPhoneData = { navigator.navigate(PhoneToolsRoute) },
        onExitDemo = shell::exitDemo, onOpen = { navigator.navigate(SettingsPageRoute(it)) },
        onBack = if (page == "settings") null else navigator::back, onNavigate = navigator::navigate)
}

/** Inbox deep links wait for the item list, then become the page the inbox list would open. */
@Composable
private fun InboxLink(route: NavKey, inbox: InboxRepository, navigator: Navigator, replace: (NavKey, NavKey) -> Unit) {
    val snapshot by inbox.snapshot.collectAsStateWithLifecycle()
    var loaded by remember(route) { mutableStateOf(false) }
    LaunchedEffect(route, inbox) {
        try { inbox.refresh() }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { /* Cached records still resolve the link; a miss is reported below. */ }
        loaded = true
    }
    val destination = inboxDestination(route, snapshot.records, loaded)
    LaunchedEffect(destination) { (destination as? InboxDestination.Open)?.let { replace(route, it.key) } }
    if (destination == InboxDestination.Missing) UnavailablePage("没有找到这条事项，可能已经处理或被移除", navigator)
    else Box(Modifier.fillMaxSize().statusBarsPadding(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
}

/** Debug builds add one row under the settings page; release builds and automated captures show nothing. */
@Composable
private fun SettingsWithTools(tools: DeveloperTools?, navigator: Navigator, content: @Composable () -> Unit) {
    if (tools == null) return content()
    Column(Modifier.fillMaxSize()) {
        Box(Modifier.weight(1f)) { content() }
        HorizontalDivider()
        TextButton(onClick = { navigator.navigate(tools.home) }, Modifier.fillMaxWidth()) { Text(tools.homeTitle, maxLines = 1) }
    }
}

@Composable
internal fun UnavailablePage(message: String, navigator: Navigator) {
    Column(Modifier.fillMaxSize().statusBarsPadding().padding(24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(message)
        TextButton(onClick = navigator::back) { Text("返回", maxLines = 1) }
    }
}
