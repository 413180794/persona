package jarvis.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.*
import jarvis.core.designsystem.AppFeedback
import jarvis.core.navigation.*
import jarvis.feature.chat.*
import jarvis.feature.files.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow

@Composable
internal fun ShellChat(route: ChatRoute, session: ShellSession, navigator: Navigator, feedback: AppFeedback, requestId: String? = null,
    onSessionClosed: (SessionKey) -> Unit = {}) {
    // The services of the session's own hub group, which need not be the selected one (docs/android.md「多 hub」).
    val source = chatWiringSource()
    val activeWiring by remember(source, route.machineId) { source.bindingsFor(SessionKey(route.machineId, route.sessionId)) }
        .collectAsStateWithLifecycle()
    val selectedWiring by source.current.collectAsStateWithLifecycle()
    val wiring = activeWiring?.takeIf { it.scope.path == session.identity || selectedWiring?.scope?.path == session.identity }
    val onClosed by rememberUpdatedState(onSessionClosed)
    var closed by remember(route.machineId, route.sessionId) { mutableStateOf(false) }
    val sourceActions = wiring?.actions ?: session.actions
    val actions = remember(sourceActions) { sourceActions?.let { delegate ->
        object : ChatActions by delegate {
            private fun confirmed(session: jarvis.core.contracts.Session, action: ChatAction, result: ChatActionResult): ChatActionResult {
                if (action == ChatAction.Close && result == ChatActionResult.Done) {
                    closed = true
                    onClosed(session.key)
                }
                return result
            }
            override suspend fun execute(session: jarvis.core.contracts.Session, action: ChatAction) =
                confirmed(session, action, delegate.execute(session, action))
            override suspend fun verify(session: jarvis.core.contracts.Session, action: ChatAction) =
                confirmed(session, action, delegate.verify(session, action))
        }
    } }
    if (session.demo == null && wiring == null) {
        GroupGoneRedirect(route, session, navigator)
        androidx.compose.material3.CircularProgressIndicator()
        return
    }
    val key = SessionKey(route.machineId, route.sessionId)
    val sessions by session.sessions.sessions.collectAsStateWithLifecycle()
    val metadata = sessions.firstOrNull { it.key == key }
    val assistant = route.kind == ConversationKind.Assistant || metadata?.assistant == true
    val editor = rememberChatDraftEditor(wiring, key)
    val storedComposer by (editor?.composer ?: remember { MutableStateFlow<ChatComposerState?>(null) }).collectAsStateWithLifecycle()
    val draftError by (editor?.error ?: remember { MutableStateFlow<String?>(null) }).collectAsStateWithLifecycle()
    val composer = storedComposer ?: rememberChatComposerState()
    val focus = androidx.compose.ui.platform.LocalFocusManager.current
    var pane by rememberSaveable { mutableStateOf<FilesPane?>(null) }
    var insertionFocused by remember { mutableStateOf(false) }
    var git by remember { mutableStateOf<jarvis.core.contracts.api.SessionGitStatus?>(null) }
    val cwd = metadata?.cwd.orEmpty()
    LaunchedEffect(cwd, session.files, pane) {
        if (!assistant && cwd.isNotBlank()) {
            try { git = session.files?.status(route.machineId, cwd) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { git = null }
        }
    }
    val filesAvailable = !assistant && cwd.isNotBlank() && session.files != null
    fun open(p: FilesPane) { insertionFocused = composer.focused; focus.clearFocus(); pane = p }
    if (editor != null && storedComposer == null) return
    Box(Modifier.fillMaxSize()) {
        // Keep the composer and conversation mounted while browsing the session directory.
        ChatScreen(key, session.chats, session.sender, session.interactions, session.sessions, { if (!closed) navigator.back() },
            onTerminal = if (session.terminal != null && !assistant) ({ navigator.navigate(TerminalRoute(it.machineId, it.sessionId)) }) else null,
            chrome = ChatChromeBindings(
                onFiles = if (filesAvailable) ({ open(FilesPane.Files) }) else null,
                onGit = if (filesAvailable && git?.isRepo == true) ({ open(FilesPane.Git) }) else null,
                gitLabel = "Git${git?.files?.size?.takeIf { it > 0 }?.let { " · $it" }.orEmpty()}",
                actions = actions, connection = wiring?.connection ?: session.connection, requestId = requestId, covered = pane != null,
                onOpenSession = { navigator.navigate(ChatRoute(it.machineId, it.sessionId)) },
                onConnectionDetails = { navigator.navigate(SettingsPageRoute(SettingsPage.Diagnostics)) }),
            renderStyle = if (assistant) ChatRenderStyle.Assistant else ChatRenderStyle.Agent,
            composerState = composer, composerService = wiring?.composer,
            onDraftChanged = editor?.let { it::changed },
            statusBindings = ChatStatusBindings(recovery = wiring?.recovery, draftSaveError = draftError),
            onContextCleared = { navigator.navigate(ChatRoute(it.machineId, it.sessionId)) })
        if (pane != null && filesAvailable) {
            BackHandler { pane = null }
            SessionFilesScreen(route, cwd, requireNotNull(session.files), { pane = null }, { path ->
                composer.value = insertFilePath(composer.value, path, insertionFocused)
                pane = null
                composer.focusRequest++
            }, initialPane = pane!!)
        }
    }
}

/**
 * The session's hub group went away under the page: it merged into another group (its machines lost their suffix)
 * or was disconnected (docs/android.md「多 hub」). The page moves to the same session under its new id, else goes back.
 */
@Composable
private fun GroupGoneRedirect(route: ChatRoute, session: ShellSession, navigator: Navigator) {
    val namespace = jarvis.core.contracts.GroupKeys.namespaceOf(route.machineId) ?: return
    val groups by session.groups.groups.collectAsStateWithLifecycle()
    val sessions by session.sessions.sessions.collectAsStateWithLifecycle()
    if (groups.isEmpty() || groups.any { it.namespace == namespace }) return
    val plain = jarvis.core.contracts.GroupKeys.raw(route.machineId)
    val moved = sessions.firstOrNull { it.sessionId == route.sessionId && jarvis.core.contracts.GroupKeys.raw(it.machineId) == plain }
    LaunchedEffect(route, moved?.machineId) {
        // The merged group's list may still be on its way: give the session a moment to show up under its new id.
        if (moved == null) kotlinx.coroutines.delay(2_000)
        navigator.back()
        moved?.let { navigator.navigate(ChatRoute(it.machineId, it.sessionId, kind = route.kind)) }
    }
}
