package jarvis.android.ui.shell

import android.content.Context
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.ui.conversationTitle
import jarvis.android.voice.voicePlugin
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import java.time.Instant

/**
 * The Jarvis assistant's state, shared by its page and the shell's drawer (iOS AppModel's assistant part,
 * docs/assistant.md): where it runs, its conversations (the hub's list kept fresh by the live sessions), the one on
 * screen, the main one, the ones pinned on this phone. A new conversation starts from the empty page; its first
 * message shows at once while it spins up ([pending]).
 */
@Stable
class AssistantModel(private val hub: Hub, private val scope: CoroutineScope, context: Context?) {
    private val prefs = context?.applicationContext?.getSharedPreferences("ui", Context.MODE_PRIVATE)

    var state by mutableStateOf<ApiClient.AssistantState?>(null)
        private set
    var loadError by mutableStateOf<String?>(null)
        private set
    /** The conversation on screen; null = the new-conversation page. */
    var current by mutableStateOf<String?>(null)
    /** The user picked a conversation (or 新对话) themselves: the first load no longer moves them. */
    var chose by mutableStateOf(false)
    /** The first message of a conversation being started (shown at once), or null. */
    var pending by mutableStateOf<String?>(null)
        private set
    var startError by mutableStateOf<String?>(null)
        private set
    /** The hub's voice plugin, when it runs one (docs/plugins.md): the top bar's menu offers 语音对话. */
    var voicePlugin by mutableStateOf<jarvis.android.voice.Plugin?>(null)
        private set
    /** Conversations pinned on this phone (iOS `assistant.pinned`); the main one always is. */
    var pinned by mutableStateOf(prefs?.getStringSet(PINNED, emptySet()).orEmpty().toSet())
        private set

    val machine: String get() = state?.machineId.orEmpty()
    val main: String? get() = state?.main?.takeIf { it.isNotEmpty() }

    suspend fun reload() {
        runCatching { hub.api.assistant() }.onSuccess { s ->
            state = s; loadError = null
            // First load: the main conversation, else the most recent one (iOS picks up where you left off).
            if (!chose && current == null) current = s.main?.takeIf { m -> s.conversations.any { it.sessionId == m } } ?: s.conversations.maxByOrNull { it.updated ?: Instant.EPOCH }?.sessionId
        }.onFailure { loadError = it.message }
    }

    /** The page is showing: get claude ready (the first message skips its startup) and look for a voice plugin. */
    suspend fun warm() {
        runCatching { hub.api.warmAssistant() }
        voicePlugin = runCatching { hub.api.voicePlugin() }.getOrNull()
    }

    /** Every conversation, newest first: the hub's list with the live sessions' titles and states over it. */
    fun conversations(byMachine: Map<String, List<Session>>): List<Session> {
        val live = byMachine[machine].orEmpty().filter { it.assistant }.associateBy { it.sessionId }
        return (state?.conversations.orEmpty().map { live[it.sessionId] ?: it } + live.values)
            .distinctBy { it.sessionId }
            .sortedByDescending { it.updated ?: Instant.EPOCH }
    }

    fun isPinned(id: String) = id == main || id in pinned

    fun togglePin(id: String) {
        pinned = if (id in pinned) pinned - id else pinned + id
        prefs?.edit()?.putStringSet(PINNED, pinned)?.apply()
    }

    fun unpinAll() {
        pinned = emptySet()
        prefs?.edit()?.remove(PINNED)?.apply()
    }

    fun open(id: String) { current = id; chose = true }

    fun openNew() { current = null; chose = true }

    /** A new conversation with [text] as its first message; the first one ever becomes the main conversation. */
    fun start(text: String) {
        val prompt = text.trim().ifEmpty { return }
        pending = prompt; startError = null
        scope.launch {
            runCatching { hub.api.startConversation(prompt, main = state?.main.isNullOrEmpty()) }
                .onSuccess { s -> reload(); current = s.sessionId; chose = true; pending = null }
                .onFailure { e -> startError = "没开起来：${e.message?.lineSequence()?.firstOrNull()}"; pending = null }
        }
    }

    /** 问 Jarvis from elsewhere (iOS AppModel.askJarvis): into the main conversation, else a new main one. */
    fun ask(text: String, onError: (String) -> Unit) {
        val m = main
        if (m == null || machine.isEmpty()) { start(text); return }
        current = m; chose = true; startError = null
        scope.launch {
            runCatching { hub.api.send(SessionKey(machine, m), text, java.util.UUID.randomUUID().toString()) }
                .onFailure { e -> onError("没发给 Jarvis：${e.message?.lineSequence()?.firstOrNull()}") }
        }
    }

    /** The title the top bar and the drawer show for [s]. */
    fun title(s: Session): String = if (s.sessionId == main) "主对话" else s.conversationTitle()

    companion object {
        private const val PINNED = "assistant.pinned"
    }
}
