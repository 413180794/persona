package persona.android

import android.app.Application
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.IntentCompat
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.AndroidEntryPoint
import dagger.hilt.android.lifecycle.HiltViewModel
import persona.core.designsystem.PersonaTheme
import persona.core.contracts.SessionKey
import persona.core.contracts.api.AssistantRepository
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.SessionsRepository
import persona.feature.agents.AgentsPreferences
import persona.feature.agents.AgentsPreferencesSnapshot
import persona.core.domain.sort.AgentSession
import persona.core.domain.sort.agentRank
import java.util.UUID
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.first

@AndroidEntryPoint
class ShareActivity : ComponentActivity() {
    @Inject lateinit var preferences: AgentsPreferences
    @OptIn(ExperimentalMaterial3Api::class)
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val model = ViewModelProvider(this)[ShareViewModel::class.java]
        model.read(intent)
        setContent {
            PersonaTheme {
                val assistant by model.assistant.assistant.collectAsStateWithLifecycle()
                val sessions by model.sessions.sessions.collectAsStateWithLifecycle()
                val machines by model.sessions.machines.collectAsStateWithLifecycle()
                val sorting by preferences.snapshots.collectAsStateWithLifecycle(AgentsPreferencesSnapshot())
                val sheet = rememberModalBottomSheetState(skipPartiallyExpanded = true)
                LaunchedEffect(model.completed) {
                    model.completed?.let {
                        Toast.makeText(this@ShareActivity, it, Toast.LENGTH_SHORT).show()
                        finish()
                    }
                }
                ModalBottomSheet(onDismissRequest = { finish() }, sheetState = sheet) {
                    Column(
                        Modifier.fillMaxWidth().verticalScroll(rememberScrollState())
                            .padding(horizontal = 24.dp).padding(bottom = 24.dp),
                        verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Text("发给 Jarvis", style = MaterialTheme.typography.titleLarge)
                        Surface(color = MaterialTheme.colorScheme.surfaceContainer, shape = MaterialTheme.shapes.medium) {
                            Column(Modifier.fillMaxWidth().padding(16.dp)) {
                                Text(model.payload.title.ifBlank { "分享内容" }, maxLines = 2, overflow = TextOverflow.Ellipsis)
                                if (model.payload.text.isNotBlank()) {
                                    Text(model.payload.text, maxLines = 4, overflow = TextOverflow.Ellipsis,
                                        style = MaterialTheme.typography.bodyMedium)
                                }
                                if (model.payload.uris.isNotEmpty()) Text("${model.payload.uris.size} 个附件")
                            }
                        }
                        OutlinedTextField(
                            value = model.note,
                            onValueChange = model::updateNote,
                            modifier = Modifier.fillMaxWidth(),
                            enabled = !model.locked && !model.busy,
                            placeholder = { Text("补一句（可选）") },
                            maxLines = 4,
                        )
                        Text("发到", style = MaterialTheme.typography.labelLarge)
                        ShareTarget("主对话", "Jarvis", model.target == null,
                            !model.busy && !model.locked && model.paired) { model.select(null) }
                        Column(Modifier.heightIn(max = 200.dp).verticalScroll(rememberScrollState())) {
                            sessions.filter { it.isLive && !it.assistant && it.key != assistant.mainSession }
                                .distinctBy { it.key }
                                .map { session ->
                                    val version = "${session.updatedAt.orEmpty()}|${session.rev ?: ""}"
                                    val visible = if (session.agentState == persona.core.contracts.AgentState.Unread &&
                                        sorting.seen[session.key.path] == version) session.copy(state = "idle") else session
                                    AgentSession(visible, session.stateSince, session.subagents.orEmpty().any { it.running == true })
                                }
                                .sortedWith(compareBy<AgentSession> { agentRank(it, sorting.mutes) }
                                    .thenByDescending { it.sortDate }.thenBy { it.session.sessionId }.thenBy { it.id })
                                .forEach { row ->
                                    val session = row.session
                                    ShareTarget(session.displayTitle,
                                        machines.firstOrNull { it.id == session.machineId }?.displayName ?: session.agentLabel,
                                        model.target == session.key, !model.busy && !model.locked) { model.select(session.key) }
                                }
                        }
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text("作为上下文附在消息里", Modifier.weight(1f))
                            Switch(model.asContext, model::updateContext, enabled = !model.busy && !model.locked)
                        }
                        if (model.loading) Text("正在获取对话…")
                        model.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                        if (model.busy) {
                            LinearProgressIndicator(Modifier.fillMaxWidth())
                            Text(model.progress)
                        }
                        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                            TextButton(onClick = { finish() }) { Text("取消", maxLines = 1) }
                            if (model.locked || !model.paired) {
                                Button(onClick = {
                                    startActivity(mainActivityIntent(this@ShareActivity).apply {
                                        model.sentTarget?.let { putExtra(MainActivity.EXTRA_SESSION, it.path) }
                                    })
                                    finish()
                                }, enabled = !model.busy) {
                                    Text(if (model.paired) "查看对话" else "打开 Jarvis", maxLines = 1)
                                }
                            } else {
                                if (model.error != null && !model.busy) {
                                    TextButton(onClick = model::refresh) { Text("刷新", maxLines = 1) }
                                }
                                Button(onClick = { model.send(model.target) },
                                    enabled = !model.busy && model.payload.hasContent && model.paired) {
                                    Text("发送", maxLines = 1)
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ShareTarget(title: String, subtitle: String, selected: Boolean, enabled: Boolean, select: () -> Unit) {
    Row(Modifier.fillMaxWidth().selectable(selected, enabled = enabled, role = Role.RadioButton, onClick = select)
        .padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        RadioButton(selected, onClick = null, enabled = enabled)
        Column(Modifier.padding(start = 12.dp).weight(1f)) {
            Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(subtitle, style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

internal data class SharePayload(val title: String = "", val text: String = "", val uris: List<Uri> = emptyList()) {
    val hasContent get() = title.isNotBlank() || text.isNotBlank() || uris.isNotEmpty()
}

@HiltViewModel
class ShareViewModel @Inject constructor(
    application: Application,
    val assistant: AssistantRepository,
    val sessions: SessionsRepository,
    private val queue: persona.android.platform.ShareQueue,
    private val saved: SavedStateHandle,
) : ViewModel() {
    private val app = application as PersonaApp
    private var owner by mutableStateOf<persona.android.data.Hub?>(null)
    val paired get() = owner != null
    internal var payload by mutableStateOf(SharePayload())
        private set
    var note by mutableStateOf(saved.get<String>("note").orEmpty())
        private set
    var asContext by mutableStateOf(saved.get<Boolean>("context") ?: true)
        private set
    var target by mutableStateOf(saved.get<String>("targetMachine")?.let {
        SessionKey(it, saved.get<String>("targetSession").orEmpty())
    })
        private set
    var sentTarget: SessionKey? = null
        private set
    var locked by mutableStateOf(saved.get<Boolean>("submitted") ?: false)
        private set
    var busy by mutableStateOf(false)
        private set
    var loading by mutableStateOf(false)
        private set
    var progress by mutableStateOf("")
        private set
    var error by mutableStateOf<String?>(null)
        private set
    var completed by mutableStateOf<String?>(null)
        private set
    private var initialized = false
    private var staged = false
    private val id = saved.get<String>("messageId") ?: UUID.randomUUID().toString().also { saved["messageId"] = it }

    fun updateNote(value: String) { note = value; saved["note"] = value }
    fun updateContext(value: Boolean) { asContext = value; saved["context"] = value }
    fun select(value: SessionKey?) {
        target = value
        saved["targetMachine"] = value?.machineId
        saved["targetSession"] = value?.sessionId
    }

    fun read(intent: Intent) {
        if (initialized) return
        initialized = true
        try {
            require(intent.action == Intent.ACTION_SEND || intent.action == Intent.ACTION_SEND_MULTIPLE)
            val streams = if (intent.action == Intent.ACTION_SEND_MULTIPLE) {
                IntentCompat.getParcelableArrayListExtra(intent, Intent.EXTRA_STREAM, Uri::class.java).orEmpty()
            } else listOfNotNull(IntentCompat.getParcelableExtra(intent, Intent.EXTRA_STREAM, Uri::class.java))
            val clips = intent.clipData?.let { clip -> (0 until clip.itemCount).map(clip::getItemAt) }.orEmpty()
            val uris = (streams + clips.mapNotNull { it.uri }).distinct()
            require(uris.all { it.scheme == "content" })
            payload = SharePayload(
                intent.getCharSequenceExtra(Intent.EXTRA_SUBJECT)?.toString()
                    ?: intent.getCharSequenceExtra(Intent.EXTRA_TITLE)?.toString().orEmpty(),
                intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.toString()
                    ?: clips.mapNotNull { it.text?.toString() }.distinct().joinToString("\n"), uris)
            require(payload.hasContent)
        } catch (_: Exception) {
            error = "无法读取分享内容，请从来源应用重新分享"
            return
        }
        busy = true
        progress = "正在保存分享内容…"
        viewModelScope.launch {
            try {
                kotlinx.coroutines.flow.combine(app.pairingReady, app.pairingRestoreError) { ready, failed -> ready || failed }.first { it }
                owner = app.hub.value
                check(owner != null)
                if (locked) { completed = "已加入发送队列"; return@launch }
                locked = queue.stage(id, payload.title, payload.text, payload.uris)
                staged = true
                if (locked) completed = "已加入发送队列"
            } catch (error: CancellationException) { throw error }
            catch (_: Exception) {
                this@ShareViewModel.error = if (owner == null) "请先打开 Jarvis 完成配对，再重新分享"
                    else "分享内容未能完整保存，请检查存储空间后从来源应用重新分享"
            } finally { busy = false }
            refresh()
        }
    }

    fun refresh() {
        if (loading || busy || locked || owner == null) return
        loading = true
        viewModelScope.launch {
            try {
                assistant.refresh()
                sessions.refresh()
            } catch (error: CancellationException) { throw error }
            catch (_: Exception) { if (staged) this@ShareViewModel.error = "对话列表暂未更新，仍可发到主对话；离线内容会先保存在本机。" }
            finally { loading = false }
        }
    }

    fun send(destination: SessionKey?) {
        if (busy || locked || !staged) return
        if (owner == null || app.hub.value !== owner) { error = "配对已变更，请重新分享"; return }
        busy = true
        error = null
        progress = "正在加入发送队列…"
        viewModelScope.launch {
            try {
                queue.submit(id, destination, note, asContext)
                sentTarget = destination
                saved["submitted"] = true
                locked = true
                completed = "已加入发送队列，联网后自动发送"
            } catch (error: CancellationException) { throw error }
            catch (_: Exception) { this@ShareViewModel.error = "暂时无法加入发送队列，内容已保留，请重试" }
            finally { busy = false }
        }
    }
}

internal suspend fun <T> sendSharedContent(
    sender: MessageSender, target: SessionKey, title: String, text: String, attachments: List<T>,
    note: String, asContext: Boolean, id: String, upload: suspend (T) -> String, beforeSend: () -> Unit = {},
) {
    val paths = attachments.map { upload(it) }
    beforeSend()
    sender.send(target, formatSharedContent(title, text, paths, note, asContext), id)
}

internal fun formatSharedContent(title: String, text: String, attachments: List<String>, note: String, asContext: Boolean): String {
    val paths = attachments.joinToString(" ") { "'${it.replace("'", "'\\''")}'" }
    val content = listOf(title, text, paths).filter { it.isNotBlank() }.joinToString("\n")
    val shared = if (asContext) {
        fun escape(value: String) = value.replace("&", "&amp;").replace("\"", "&quot;")
            .replace("<", "&lt;").replace(">", "&gt;")
        val url = Regex("https?://[^\\s<>]+", RegexOption.IGNORE_CASE).find(text)?.value.orEmpty()
        "<jarvis-context kind=\"context\" title=\"${escape(title.ifBlank { "分享内容" })}\" url=\"${escape(url)}\">\n" +
            content.replace("</jarvis-context>", "") + "\n</jarvis-context>"
    } else content
    return listOf(shared, note.trim()).filter { it.isNotBlank() }.joinToString("\n\n")
}
