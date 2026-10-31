package jarvis.android.ui

import androidx.activity.compose.BackHandler
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
import jarvis.android.MainActivity
import jarvis.android.data.AgentState
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
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
    var tab by rememberSaveable { mutableStateOf(Tab.Agents) }
    var settings by rememberSaveable { mutableStateOf(false) }
    val settingsStack = rememberSaveable { mutableStateOf(emptyList<String>()) } // 设置's sub-pages (SettingsRoute)
    var space by rememberSaveable { mutableStateOf<String?>(null) }
    var handoff by remember { mutableStateOf<String?>(null) } // inbox → Jarvis
    val open = openSession.value
    if (open != null) tab = Tab.Agents
    var askFocus by remember { mutableStateOf(false) } // 「问 Jarvis」 shortcut: focus the Jarvis composer
    openTab.value?.let {
        when (it) {
            "jarvis" -> { tab = Tab.Jarvis; settings = false }
            "ask" -> { tab = Tab.Jarvis; settings = false; askFocus = true }
            "agents" -> { tab = Tab.Agents; settings = false; space = null; openSession.value = null }
        }
        openTab.value = null
    }

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
                selected = tab == Tab.Jarvis, onClick = { tab = Tab.Jarvis },
                icon = { BadgedBox(badge = { if (assistantWaiting) Badge() }) { Icon(Icons.AutoMirrored.Filled.Chat, null) } },
                label = { Text(Tab.Jarvis.title, maxLines = 1) },
            )
            NavigationBarItem(
                selected = tab == Tab.Agents, onClick = { tab = Tab.Agents },
                icon = {
                    BadgedBox(badge = { if (needsYou + unread > 0) Badge { Text("${needsYou + unread}") } }) { Icon(Icons.Default.Computer, null) }
                },
                label = { Text(Tab.Agents.title, maxLines = 1) },
            )
            NavigationBarItem(
                selected = tab == Tab.Inbox, onClick = { tab = Tab.Inbox },
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

    LinkHost {
        when {
            // A chat goes over 设置 too (opened from a machine's page, or a notification): back returns to where it was.
            open != null -> {
                BackHandler { openSession.value = null }
                ChatScreen(hub, open, onBack = { openSession.value = null })
            }
            settings -> {
                BackHandler { settings = false }
                SettingsScreen(hub, onBack = { settings = false }, stack = settingsStack, onOpenSession = { openSession.value = it })
            }
            tab == Tab.Jarvis -> AssistantScreen(
                hub, bottomBar = bar, incoming = handoff, onIncomingTaken = { handoff = null }, onOpenSession = { openSession.value = it },
                focusComposer = askFocus, onFocusTaken = { askFocus = false },
            )
            tab == Tab.Inbox -> InboxScreen(hub, bottomBar = bar, onHandToJarvis = { handoff = it; tab = Tab.Jarvis })
            space != null -> {
                BackHandler { space = null }
                SpaceScreen(hub, space!!, onOpen = { openSession.value = it }, onBack = { space = null })
            }
            else -> AgentsScreen(
                hub, onOpen = { openSession.value = it }, onOpenSpace = { space = it }, onSettings = { settingsStack.value = emptyList(); settings = true },
                bottomBar = bar,
            )
        }
    }
}
