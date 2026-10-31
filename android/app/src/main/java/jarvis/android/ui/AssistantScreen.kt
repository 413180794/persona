package jarvis.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.LibraryBooks
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberDrawerState
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
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import kotlinx.coroutines.launch
import java.time.Instant

/**
 * The Jarvis tab (iOS AssistantView, docs/assistant.md): the personal assistant, a headless Claude on one of the
 * computers. It opens on the main conversation; the drawer lists the others and starts a new one. A new
 * conversation starts from a home screen with a few suggestions, its first message shown at once while it spins up.
 */
@OptIn(ExperimentalMaterial3Api::class, androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun AssistantScreen(
    hub: Hub, bottomBar: @Composable () -> Unit,
    /** Something another tab handed to Jarvis (the inbox's 交给 Jarvis): started as a new conversation, then cleared. */
    incoming: String? = null, onIncomingTaken: () -> Unit = {},
    /** Opens an Agent's chat (the Agents tab): the knowledge base's 开 Agent and a project's Agents. */
    onOpenSession: (SessionKey) -> Unit = {},
    /** The 「问 Jarvis」 shortcut: put the cursor in the composer (conversation or home), then cleared. */
    focusComposer: Boolean = false, onFocusTaken: () -> Unit = {},
) {
    val byMachine by hub.sessions.collectAsState()
    var state by remember { mutableStateOf<ApiClient.AssistantState?>(null) }
    var loadError by remember { mutableStateOf<String?>(null) }
    // null = the home screen (a new conversation); otherwise the conversation shown.
    var current by rememberSaveable { mutableStateOf<String?>(null) }
    var chose by rememberSaveable { mutableStateOf(false) } // the user picked a conversation (or 新对话) themselves
    var pending by remember { mutableStateOf<String?>(null) }
    var startError by remember { mutableStateOf<String?>(null) }
    val drawer = rememberDrawerState(DrawerValue.Closed)
    var profile by rememberSaveable { mutableStateOf(false) }
    var knowledge by rememberSaveable { mutableStateOf(false) }
    val machines by hub.machines.collectAsState()
    val scope = rememberCoroutineScope()

    suspend fun reload() {
        runCatching { hub.api.assistant() }.onSuccess { state = it; loadError = null }.onFailure { loadError = it.message }
    }
    LaunchedEffect(Unit) {
        reload()
        runCatching { hub.api.warmAssistant() } // the first message then skips claude's startup
    }
    // First open: the main conversation, else the most recent one (iOS picks up where you left off).
    LaunchedEffect(state) {
        val s = state ?: return@LaunchedEffect
        if (!chose && current == null) current = s.main?.takeIf { m -> s.conversations.any { it.sessionId == m } } ?: s.conversations.newest()?.sessionId
    }

    val machine = state?.machineId.orEmpty()
    // Conversations come from the assistant state, kept fresh by the live session list (titles, states).
    val live = byMachine[machine].orEmpty().filter { it.assistant }.associateBy { it.sessionId }
    val conversations = (state?.conversations.orEmpty().map { live[it.sessionId] ?: it } + live.values)
        .distinctBy { it.sessionId }
        .sortedByDescending { it.updated ?: Instant.EPOCH }

    fun start(text: String) {
        val prompt = text.trim().ifEmpty { return }
        pending = prompt; startError = null
        scope.launch {
            runCatching { hub.api.startConversation(prompt, main = state?.main.isNullOrEmpty()) }
                .onSuccess { s -> reload(); current = s.sessionId; chose = true; pending = null }
                .onFailure { e -> startError = "没开起来：${e.message?.lineSequence()?.firstOrNull()}"; pending = null }
        }
    }

    // 问 Jarvis from the knowledge base (iOS AppModel.askJarvis): into the main conversation, else a new main one.
    fun ask(text: String) {
        knowledge = false; profile = false
        val main = state?.main?.takeIf { it.isNotEmpty() }
        if (main == null || machine.isEmpty()) { start(text); return }
        current = main; chose = true; startError = null
        scope.launch {
            runCatching { hub.api.send(SessionKey(machine, main), text, java.util.UUID.randomUUID().toString()) }
                .onFailure { e ->
                    // The conversation page is showing (not the home screen with its error line): say it there.
                    android.widget.Toast.makeText(jarvis.android.JarvisApp.instance, "没发给 Jarvis：${e.message?.lineSequence()?.firstOrNull()}", android.widget.Toast.LENGTH_LONG).show()
                }
        }
    }

    LaunchedEffect(incoming, state != null) {
        if (incoming != null && state != null) { profile = false; knowledge = false; start(incoming); onIncomingTaken() }
    }

    ModalNavigationDrawer(
        drawerState = drawer,
        drawerContent = {
            ModalDrawerSheet {
                Text("Jarvis", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(start = 24.dp, top = 24.dp, bottom = 8.dp))
                NavigationDrawerItem(
                    label = { Text("新对话") }, icon = { Icon(Icons.Default.Edit, null) }, selected = current == null,
                    onClick = { current = null; chose = true; scope.launch { drawer.close() } },
                    modifier = Modifier.padding(horizontal = 12.dp),
                )
                NavigationDrawerItem(
                    label = { Text("定时任务、记忆、关于我") }, icon = { Icon(Icons.Default.Person, null) }, selected = false,
                    onClick = { profile = true; scope.launch { drawer.close() } },
                    modifier = Modifier.padding(horizontal = 12.dp),
                )
                NavigationDrawerItem(
                    label = { Text("知识库") }, icon = { Icon(Icons.AutoMirrored.Filled.LibraryBooks, null) }, selected = false,
                    onClick = { knowledge = true; profile = false; scope.launch { drawer.close() } },
                    modifier = Modifier.padding(horizontal = 12.dp),
                )
                HorizontalDivider(Modifier.padding(vertical = 8.dp))
                Text("历史对话", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(start = 24.dp, bottom = 4.dp))
                LazyColumn {
                    items(conversations, key = { it.sessionId }) { c ->
                        val isMain = c.sessionId == state?.main
                        NavigationDrawerItem(
                            label = {
                                Column {
                                    Text(c.conversationTitle(), maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    Text(
                                        listOfNotNull("主对话".takeIf { isMain }, ago(c.updated).ifEmpty { null }).joinToString(" · "),
                                        style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    )
                                }
                            },
                            icon = if (isMain) ({ Icon(Icons.Default.Star, "主对话", Modifier.size(18.dp)) }) else null,
                            selected = c.sessionId == current,
                            onClick = { current = c.sessionId; chose = true; scope.launch { drawer.close() } },
                            modifier = Modifier.padding(horizontal = 12.dp),
                        )
                    }
                }
            }
        },
    ) {
        val historyButton: @Composable () -> Unit = { IconButton({ scope.launch { drawer.open() } }) { Icon(Icons.Default.Menu, "历史对话") } }
        val newButton: @Composable () -> Unit = { IconButton({ current = null; chose = true }) { Icon(Icons.Default.Edit, "新对话") } }
        val shown = current
        if (knowledge) {
            jarvis.android.ui.knowledge.KnowledgeScreen(hub, onBack = { knowledge = false }, onAsk = ::ask, onOpenSession = onOpenSession)
        } else if (profile) {
            AssistantProfile(
                hub, machineName = machines.firstOrNull { it.id == machine }?.displayName ?: machine,
                onBack = { profile = false },
                onOpenConversation = { id -> scope.launch { reload() }; current = id; chose = true; profile = false },
            )
        } else if (shown != null && pending == null && machine.isNotEmpty()) {
            // The tab bar sits under the conversation; while typing it goes away, so the composer sits right on the
            // keyboard (ChatScreen handles the keyboard inset itself: a Scaffold around it would add it twice).
            // Same while a link is open: at 60 % the sheet sits right under the composer (CLAUDE.md「链接在 App 内打开」).
            val typing = WindowInsets.isImeVisible || jarvis.android.ui.web.Links.current.value != null
            Column(Modifier.fillMaxSize()) {
                androidx.compose.foundation.layout.Box(Modifier.weight(1f)) {
                    ChatScreen(
                        hub, SessionKey(machine, shown), onBack = {}, assistant = true, navigation = historyButton, actions = newButton,
                        embedded = !typing, focusComposer = focusComposer, onFocusTaken = onFocusTaken,
                    )
                }
                if (!typing) bottomBar()
            }
        } else {
            Scaffold(
                topBar = { TopAppBar(title = { Text("Jarvis") }, navigationIcon = historyButton) },
                bottomBar = bottomBar,
            ) { pad ->
                AssistantHome(
                    Modifier.padding(pad), pending = pending, error = startError ?: loadError,
                    ready = state != null, onSend = ::start,
                    // Only once the home screen is where the tab stays (not while the main conversation is being picked).
                    focusComposer = focusComposer && pending == null && state != null && (chose || state?.conversations.isNullOrEmpty()),
                    onFocusTaken = onFocusTaken,
                )
            }
        }
    }
}

private fun List<Session>.newest() = maxByOrNull { it.updated ?: Instant.EPOCH }

/** A conversation's name in the history: its title, else its first words; a hand-off shows what it was about. */
fun Session.conversationTitle(): String {
    val t = title.takeIf { it.isNotBlank() && it != "Claude Code" } ?: lastText?.lineSequence()?.firstOrNull { it.isNotBlank() }?.take(30) ?: return "对话"
    // Started from 交给 Jarvis: the first message is a <jarvis-context title="…"> tag (claude names it after it).
    Regex("""<jarvis-context[^>]*title="([^"]*)"""").find(t)?.let { m ->
        return "交给 Jarvis：" + m.groupValues[1].replace("&quot;", "\"").replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&")
    }
    return if (t.startsWith("<jarvis-context")) "交给 Jarvis" else t
}

/** The same suggestions as the iOS home screen. */
private val SUGGESTIONS = listOf(
    "今天的安排" to "看一下我今天的安排：手机日历和飞书日历都查，合并后按时间列出来。",
    "要处理的邮件" to "我的邮箱里最近有哪些需要我处理或回复的邮件？挑重要的说。",
    "电脑在忙什么" to "我各台电脑上的 Agent 现在都在忙什么？哪些在等我、哪些跑完了还没看？",
    "飞书消息" to "总结一下我今天飞书上收到的重要消息，哪些需要我回复。",
    "会前准备" to "看看我今天接下来的会，挑最重要的一个帮我准备：议程、相关文档、上次的纪要和没完成的待办。",
)

@Composable
private fun AssistantHome(
    modifier: Modifier, pending: String?, error: String?, ready: Boolean, onSend: (String) -> Unit,
    focusComposer: Boolean = false, onFocusTaken: () -> Unit = {},
) {
    val focus = remember { androidx.compose.ui.focus.FocusRequester() }
    FocusComposer(focusComposer, focus, onFocusTaken)
    var draft by rememberSaveable { mutableStateOf("") }
    Column(modifier.fillMaxSize().imePadding()) {
        LazyVerticalGrid(
            GridCells.Fixed(2), Modifier.weight(1f),
            contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (pending != null) {
                // The first message shows at once, with a typing line, while the conversation starts.
                item(span = { GridItemSpan(2) }) {
                    Column(horizontalAlignment = Alignment.End, modifier = Modifier.fillMaxWidth().padding(top = 12.dp)) {
                        Card(shape = RoundedCornerShape(16.dp)) { Text(pending, Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) }
                        Spacer(Modifier.height(12.dp))
                        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
                            CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                            Text("  Jarvis 正在准备…", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                return@LazyVerticalGrid
            }
            item(span = { GridItemSpan(2) }) {
                Column(Modifier.padding(vertical = 16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(greeting(), style = MaterialTheme.typography.headlineSmall)
                    Text(
                        "可以问我你的日程、邮件、飞书消息，或者让我去各台电脑上看看 Agent 在忙什么。",
                        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
                }
            }
            items(SUGGESTIONS, key = { it.first }) { (title, prompt) ->
                Card(onClick = { onSend(prompt) }, enabled = ready, modifier = Modifier.fillMaxWidth().heightIn(min = 72.dp)) {
                    Text(title, style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(14.dp))
                }
            }
        }
        Row(Modifier.fillMaxWidth().padding(8.dp), verticalAlignment = Alignment.Bottom) {
            OutlinedTextField(
                draft, { draft = it }, placeholder = { Text("问 Jarvis…") }, enabled = pending == null,
                modifier = Modifier.weight(1f).heightIn(max = 180.dp).focusRequester(focus), shape = RoundedCornerShape(20.dp),
            )
            IconButton(onClick = { onSend(draft); draft = "" }, enabled = ready && pending == null && draft.isNotBlank()) {
                Icon(Icons.AutoMirrored.Filled.Send, "发送")
            }
        }
    }
}

private fun greeting(): String = when (java.time.LocalTime.now().hour) {
    in 5..10 -> "早上好"
    in 11..13 -> "中午好"
    in 14..18 -> "下午好"
    else -> "晚上好"
}

/** Focuses [focus] (and opens the keyboard) once when [on] turns true, then tells the caller it is done. */
@Composable
internal fun FocusComposer(on: Boolean, focus: androidx.compose.ui.focus.FocusRequester, onTaken: () -> Unit) {
    val keyboard = androidx.compose.ui.platform.LocalSoftwareKeyboardController.current
    LaunchedEffect(on) {
        if (!on) return@LaunchedEffect
        kotlinx.coroutines.delay(300) // after the tab's first layout: the text field has to be attached
        runCatching { focus.requestFocus(); keyboard?.show() }
        onTaken()
    }
}
