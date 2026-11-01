package jarvis.android.ui

import androidx.activity.compose.BackHandler
import jarvis.android.ui.settings.SettingsRoute
import jarvis.android.ui.settings.SettingsPage
import androidx.compose.ui.Modifier
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Chat
import androidx.compose.material.icons.filled.Computer
import androidx.compose.material.icons.filled.Inbox
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import kotlinx.coroutines.launch
import jarvis.android.JarvisApp
import jarvis.android.MainActivity
import jarvis.android.data.AgentState
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.PairingEngine
import jarvis.android.ui.machines.EngineScreen
import jarvis.android.data.SessionKey
import jarvis.android.ui.web.AppLink
import jarvis.android.ui.web.LinkHost
import jarvis.android.ui.web.Links

enum class Tab(val title: String) { Jarvis("Jarvis"), Agents("Agents"), Inbox("收件箱") }

/**
 * Pairing until a hub is paired; then two tabs, Jarvis (the assistant) and Agents, as on iOS. Agents can push a
 * space page and / or a chat on top of its list: a chat opened from a space page goes back to that page. A
 * notification opens its chat in the Agents tab (the assistant's conversations stay in Jarvis).
 */
@Composable
fun JarvisRoot(hub: Hub?, openSession: MutableState<SessionKey?>, openTab: MutableState<String?> = mutableStateOf(null)) {
    val conn = hub?.conn?.collectAsState()?.value
    // Not paired, the hub refused this device's token, or a pairing link was just opened: the pairing screen.
    if (hub == null || conn == ConnState.Unauthorized || MainActivity.pendingPair.value != null) {
        PairScreen(
            reason = if (hub != null && conn == ConnState.Unauthorized)
                "hub 不认这台手机的 token 了（在别的设备上撤销了，或者换了 hub），重新配对一下" else null,
        )
        return
    }
    // Right after pairing: which engine the hub's own machine uses (docs/engines.md). One must be chosen: back does
    // not skip the step (2026-11-01 user).
    PairingEngine.pending.value?.let { m ->
        val done = { PairingEngine.pending.value = null }
        BackHandler {}
        EngineScreen(hub, m.id, onDone = done, pairing = true, fallback = m)
        return
    }
    var tab by rememberSaveable { mutableStateOf(Tab.Agents) }
    var settings by rememberSaveable { mutableStateOf(false) }
    val settingsStack = rememberSaveable { mutableStateOf(emptyList<String>()) } // 设置's sub-pages (SettingsRoute)
    var space by rememberSaveable { mutableStateOf<String?>(null) }
    var inboxOpen by rememberSaveable { mutableStateOf<String?>(null) } // the inbox item whose detail is open
    var handoff by remember { mutableStateOf<String?>(null) } // inbox → Jarvis
    // A chat opened from anywhere shows in Agents. Only when it opens: two panes the tab bar stays beside an open chat,
    // so another tab can be chosen and the chat waits in Agents (single pane the bar is never beside a chat).
    var lastOpen by rememberSaveable { mutableStateOf<String?>(null) }
    if (openSession.value?.path != lastOpen) {
        if (openSession.value != null) tab = Tab.Agents
        lastOpen = openSession.value?.path
    }
    var askFocus by remember { mutableStateOf(false) } // 「问 Jarvis」 shortcut: focus the Jarvis composer
    openTab.value?.let {
        when (it) {
            "jarvis" -> { tab = Tab.Jarvis; settings = false }
            "ask" -> { tab = Tab.Jarvis; settings = false; askFocus = true }
            "agents" -> { tab = Tab.Agents; settings = false; space = null; openSession.value = null }
        }
        openTab.value = null
    }
    // One state for both layouts (docs/android.md「折叠屏 / 宽屏」): folding or unfolding keeps the page that was open.
    val nav = NavState(tab, settings, settingsStack.value, space, openSession.value, inboxOpen)
    val go: (NavState) -> Unit = { n ->
        tab = n.tab; settings = n.settings; settingsStack.value = n.settingsStack; space = n.space; inboxOpen = n.inbox
        if (openSession.value != n.chat) openSession.value = n.chat
        lastOpen = openSession.value?.path
    }
    val open = nav.chatShown
    // Pages keep their saved state (scroll, drafts, the Jarvis conversation) when folding / unfolding moves them
    // between the panes and the stack; a page that is no longer drawn drops it, as it always did.
    val keep = rememberSaveableStateHolder()
    val wasDrawn = remember { mutableSetOf<String>() }
    // The inbox detail belongs to the inbox: leaving the tab closes it (as when the sheet went with the screen).
    if (tab != Tab.Inbox && inboxOpen != null) inboxOpen = null

    val pending by hub.interactions.collectAsState()
    val byMachine by hub.sessions.collectAsState()
    val assistantKeys = byMachine.values.flatten().filter { it.assistant }.map { it.key }.toSet()
    val needsYou = pending.count { it.sessionKey !in assistantKeys }
    val unread = byMachine.values.flatten().count { !it.assistant && it.agentState == AgentState.Unread }
    val assistantWaiting = pending.any { it.sessionKey in assistantKeys } ||
        byMachine.values.flatten().any { it.assistant && it.agentState == AgentState.NeedsYou }
    val inbox by hub.inbox.collectAsState()
    val inboxAction = inbox.count { it.bucket == jarvis.android.data.InboxBucket.Action }
    val bar: @Composable () -> Unit = {
        NavigationBar {
            NavigationBarItem(
                selected = tab == Tab.Jarvis && !settings, onClick = { go(nav.selectTab(Tab.Jarvis)) },
                icon = { BadgedBox(badge = { if (assistantWaiting) Badge() }) { Icon(Icons.AutoMirrored.Filled.Chat, null) } },
                label = { Text(Tab.Jarvis.title, maxLines = 1) },
            )
            NavigationBarItem(
                selected = tab == Tab.Agents && !settings, onClick = { go(nav.selectTab(Tab.Agents)) },
                icon = {
                    BadgedBox(badge = { if (needsYou + unread > 0) Badge { Text("${needsYou + unread}") } }) { Icon(Icons.Default.Computer, null) }
                },
                label = { Text(Tab.Agents.title, maxLines = 1) },
            )
            NavigationBarItem(
                selected = tab == Tab.Inbox && !settings, onClick = { go(nav.selectTab(Tab.Inbox)) },
                icon = { BadgedBox(badge = { if (inboxAction > 0) Badge { Text("$inboxAction") } }) { Icon(Icons.Default.Inbox, null) } },
                label = { Text(Tab.Inbox.title, maxLines = 1) },
            )
        }
    }

    // A phone tool that changes data (a calendar event) waits for the user: ask on top of whatever is open.
    val asks by jarvis.android.JarvisApp.instance.phoneTools.pending.collectAsState()
    asks.firstOrNull()?.let { p ->
        androidx.compose.material3.AlertDialog(
            onDismissRequest = {},
            title = { Text("Claude 想在手机上操作") },
            text = { Text(p.summary) },
            confirmButton = { androidx.compose.material3.TextButton({ jarvis.android.JarvisApp.instance.phoneTools.decide(p.id, true) }) { Text("允许", maxLines = 1) } },
            dismissButton = { androidx.compose.material3.TextButton({ jarvis.android.JarvisApp.instance.phoneTools.decide(p.id, false) }) { Text("拒绝", maxLines = 1) } },
        )
    }

    // In-app links (CLAUDE.md「链接在 App 内打开」): jarvis:// links in a chat only navigate; the inbox's 交给 Jarvis
    // from a page starts an assistant conversation, like the inbox's own button.
    Links.navigate = { link ->
        when (link) {
            is AppLink.Session -> { settings = false; openSession.value = link.key }
            is AppLink.Tab -> {
                settings = false
                tab = when (link.name) { "jarvis" -> Tab.Jarvis; "inbox" -> Tab.Inbox; else -> Tab.Agents }
                if (tab == Tab.Agents) openSession.value = null
            }
        }
    }
    Links.handToJarvis = { prompt -> handoff = prompt; settings = false; tab = Tab.Jarvis }

    val onHandOffFromInbox: (HandOff) -> Unit = { h ->
        val to = h.to
        if (to == null) { handoff = h.prompt; tab = Tab.Jarvis }
        else {
            // To a running agent: its chat opens and the message goes out as if typed there.
            val app = JarvisApp.instance
            app.scope.launch {
                runCatching { hub.api.send(to, h.prompt, java.util.UUID.randomUUID().toString()) }.onFailure { e ->
                    kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.Main) {
                        android.widget.Toast.makeText(app, "没交过去：${e.message}", android.widget.Toast.LENGTH_LONG).show()
                    }
                }
            }
            tab = Tab.Agents
            openSession.value = to
        }
    }
    val chatHandToJarvis: (String) -> Unit = { handoff = it; openSession.value = null; settings = false; tab = Tab.Jarvis }

    BoxWithConstraints(Modifier.fillMaxSize()) {
        // By the window's width only, never the device (docs/android.md「折叠屏 / 宽屏」); a half-open fold splits on itself.
        val panes = SplitLayout.panes(maxWidth.value, rememberHinge())
        val drawn = if (panes == null) setOf(nav.singleTop.saveKey) else setOfNotNull(nav.list.saveKey, nav.detail?.saveKey)
        SideEffect {
            (wasDrawn - drawn).forEach(keep::removeState)
            wasDrawn.clear(); wasDrawn.addAll(drawn)
        }
        LinkHost {
            if (panes == null) {
                // One pane: the stack as it always was.
                when {
                    // A chat goes over 设置 too (opened from a machine's page, or a notification): back returns to where it was.
                    open != null -> {
                        BackHandler { openSession.value = null }
                        keep.SaveableStateProvider("chat/${open.path}") {
                            ChatScreen(
                                hub, open, onBack = { openSession.value = null },
                                // As the inbox's 交给 Jarvis: a new assistant conversation about the message.
                                onHandToJarvis = chatHandToJarvis,
                                onCleared = { openSession.value = it },
                            )
                        }
                    }
                    settings -> {
                        BackHandler { settings = false }
                        keep.SaveableStateProvider("settings") {
                            SettingsScreen(hub, onBack = { settings = false }, stack = settingsStack, onOpenSession = { openSession.value = it })
                        }
                    }
                    tab == Tab.Jarvis -> keep.SaveableStateProvider("jarvis") {
                        AssistantScreen(
                            hub, bottomBar = bar, incoming = handoff, onIncomingTaken = { handoff = null }, onOpenSession = { openSession.value = it },
                            focusComposer = askFocus, onFocusTaken = { askFocus = false },
                        )
                    }
                    tab == Tab.Inbox -> keep.SaveableStateProvider("inbox") {
                        InboxScreen(hub, bottomBar = bar, onHandOff = onHandOffFromInbox, openedId = inboxOpen, onOpen = { inboxOpen = it })
                    }
                    space != null -> {
                        BackHandler { space = null }
                        keep.SaveableStateProvider("space/$space") {
                            SpaceScreen(hub, space!!, onOpen = { openSession.value = it }, onBack = { space = null })
                        }
                    }
                    else -> keep.SaveableStateProvider("agents") {
                        AgentsScreen(
                            hub, onOpen = { openSession.value = it }, onOpenSpace = { space = it }, onSettings = { settingsStack.value = emptyList(); settings = true },
                            bottomBar = bar,
                        )
                    }
                }
            } else {
                TwoPaneRoot(
                    hub, nav, go, panes, keep, bar,
                    jarvis = {
                        AssistantScreen(
                            hub, bottomBar = {}, incoming = handoff, onIncomingTaken = { handoff = null }, onOpenSession = { openSession.value = it },
                            focusComposer = askFocus, onFocusTaken = { askFocus = false }, panes = panes,
                        )
                    },
                    settingsStack = settingsStack,
                    onHandOffFromInbox = onHandOffFromInbox,
                    chatHandToJarvis = chatHandToJarvis,
                )
            }
        }
    }
}

/** The key a page keeps its saved state under (Root's `keep`). */
private val Page.saveKey: String get() = when (this) {
    is Page.Chat -> "chat/${key.path}"
    is Page.Space -> "space/$id"
    Page.SettingsMenu, is Page.Settings -> "settings"
    Page.Jarvis -> "jarvis"
    Page.InboxList, is Page.InboxItem -> "inbox"
    Page.AgentsList -> "agents"
}

/**
 * Two panes (a wide window, the Find N5 open): the list on the left, what it opened on the right, the tab bar under
 * both (docs/android.md「折叠屏 / 宽屏」, iOS `SplitNavigation`). Items from the list replace the right pane; pages
 * opened inside it push, and its first page has no back button.
 */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun TwoPaneRoot(
    hub: Hub, nav: NavState, go: (NavState) -> Unit, panes: Panes,
    keep: androidx.compose.runtime.saveable.SaveableStateHolder,
    bar: @Composable () -> Unit,
    jarvis: @Composable () -> Unit,
    settingsStack: MutableState<List<String>>,
    onHandOffFromInbox: (HandOff) -> Unit,
    chatHandToJarvis: (String) -> Unit,
) {
    // Back pops the right pane (a chat, a 设置 page, the inbox item, the space), then leaves 设置.
    val back = nav.back()
    BackHandler(enabled = back != null) { back?.let(go) }
    // The bar steps aside while typing or with a link open, as in the Jarvis tab: the composer sits on the keyboard / the sheet.
    val barShown = !WindowInsets.isImeVisible && Links.current.value == null
    Column(Modifier.fillMaxSize()) {
        val body = Modifier.weight(1f).fillMaxWidth().then(
            if (barShown) Modifier.consumeWindowInsets(WindowInsets.navigationBars) else Modifier,
        )
        if (!nav.settings && nav.tab == Tab.Jarvis) {
            Box(body) { keep.SaveableStateProvider("jarvis") { jarvis() } }
        } else {
            TwoPanes(
                panes, body,
                list = {
                    when (nav.list) {
                        Page.SettingsMenu -> keep.SaveableStateProvider("settings") {
                            SettingsScreen(hub, onBack = { go(nav.copy(settings = false)) }, stack = settingsStack, menuOnly = true)
                        }
                        Page.InboxList -> keep.SaveableStateProvider("inbox") {
                            InboxScreen(
                                hub, bottomBar = {}, onHandOff = onHandOffFromInbox, openedId = nav.inbox,
                                onOpen = { id -> go(if (id == null) nav.copy(inbox = null) else nav.openInboxFromList(id)) },
                                detailInPane = true,
                            )
                        }
                        else -> keep.SaveableStateProvider("agents") {
                            AgentsScreen(
                                hub, onOpen = { go(nav.openChatFromList(it)) }, onOpenSpace = { go(nav.openSpaceFromList(it)) },
                                onSettings = { go(nav.copy(settings = true, settingsStack = emptyList(), chat = null)) },
                            )
                        }
                    }
                },
                detail = {
                    CompositionLocalProvider(LocalSplitDetail provides SplitDetail(isFirstPage = nav.detailIsFirst)) {
                        when (val d = nav.detail) {
                            is Page.Chat -> keep.SaveableStateProvider("chat/${d.key.path}") {
                                ChatScreen(
                                    hub, d.key, onBack = { go(nav.copy(chat = null)) }, embedded = barShown,
                                    onHandToJarvis = chatHandToJarvis,
                                    onCleared = { go(nav.copy(chat = it)) },
                                )
                            }
                            is Page.Settings -> {
                                val route = SettingsRoute.of(d.path)
                                if (route == null) SplitPlaceholder(EmptyDetail.Settings)
                                else SettingsPage(
                                    hub, route, push = { go(nav.pushSettings(it.path)) },
                                    pop = { go(nav.copy(settingsStack = nav.settingsStack.dropLast(1))) },
                                    onOpenSession = { go(nav.openChatFromDetail(it)) },
                                )
                            }
                            is Page.InboxItem -> InboxDetailPane(
                                hub, d.id, onClose = { go(nav.copy(inbox = null)) }, onHandOff = onHandOffFromInbox,
                            )
                            is Page.Space -> keep.SaveableStateProvider("space/${d.id}") {
                                SpaceScreen(hub, d.id, onOpen = { go(nav.openChatFromDetail(it)) }, onBack = { go(nav.copy(space = null)) })
                            }
                            else -> SplitPlaceholder(nav.emptyDetail)
                        }
                    }
                },
            )
        }
        if (barShown) bar()
    }
}
