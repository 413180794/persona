package persona.android.capsule

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
import kotlinx.coroutines.ensureActive
import persona.android.ui.ShellSession
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.sort.canSend
import persona.core.domain.sort.AgentPreferences
import persona.core.domain.sort.AgentSession
import persona.feature.chat.ChatWiringSource
import persona.platform.capsule.*
import java.util.UUID

internal fun capsuleId(key: SessionKey) = "${key.machineId.length}:${key.machineId}${key.sessionId}"
internal const val MainEntry = "persona:main"

internal fun capsuleEntries(sessions: List<Session>, main: SessionKey?, requests: List<Interaction>, mutes: AgentPreferences = AgentPreferences()): List<CapsuleEntry> {
    val attention = requests.groupBy { it.sessionKey }
    val listed = sessions.filter { !it.quiet && (!mutes.mutes(AgentSession(it)) || it.key == main) }.distinctBy { it.key }
    val entries = listed.filter { it.key == main || attention.containsKey(it.key) || it.agentState in setOf(AgentState.NeedsYou, AgentState.Working, AgentState.Unread) }
        .map { session ->
            val pending = attention[session.key].orEmpty()
            val state = when {
                pending.isNotEmpty() || session.agentState == AgentState.NeedsYou -> CapsuleState.NeedsYou
                session.agentState == AgentState.Working -> CapsuleState.Working
                session.agentState == AgentState.Unread -> CapsuleState.Done
                else -> CapsuleState.Idle
            }
            CapsuleEntry(capsuleId(session.key), if (session.key == main) "Persona · ${session.displayTitle}" else session.displayTitle,
                pending.firstOrNull()?.summary?.takeIf { it.isNotBlank() }
                    ?: session.activity?.takeIf { it.isNotBlank() } ?: session.lastText.orEmpty(), state,
                if (pending.isNotEmpty()) listOf(CapsuleAction("open", "查看并处理")) else emptyList())
        }.sortedBy { it.state.ordinal }
    return if (entries.none { it.id == main?.let(::capsuleId) }) entries + CapsuleEntry(MainEntry, "Persona", "想让 Persona 做什么？", CapsuleState.Idle)
        else entries
}

/** A new surface of the existing merged repositories and durable outbox. */
@OptIn(ExperimentalCoroutinesApi::class)
internal class CapsuleSessions(
    private val session: ShellSession,
    private val wiring: ChatWiringSource,
    private val scope: CoroutineScope,
    private val openConversation: (SessionKey?, Boolean) -> Unit,
) : CapsuleContentSource {
    private val selected = MutableStateFlow<String?>(null)
    private val expanded = MutableStateFlow(false)
    private val busy = MutableStateFlow(false)
    private val failure = MutableStateFlow<String?>(null)
    private val entries = combine(session.sessions.sessions, session.assistant.assistant, session.interactions.pending,
        session.preferences.snapshots) { list, assistant, pending, preferences ->
        capsuleEntries(list + assistant.conversations, assistant.mainSession, pending, preferences.mutes)
    }.stateIn(scope, SharingStarted.Eagerly, emptyList())
    private val target = combine(entries, selected, session.assistant.assistant) { list, chosen, assistant ->
        val resolved = if (chosen == MainEntry) assistant.mainSession?.let(::capsuleId) ?: MainEntry else chosen
        list.firstOrNull { it.id == resolved }
            ?: list.firstOrNull { chosen == MainEntry && it.id == MainEntry } ?: list.firstOrNull()
    }
        .stateIn(scope, SharingStarted.Eagerly, null)
    private val messages = combine(target, expanded) { entry, shown -> if (shown) key(entry?.id) else null }
        .distinctUntilChanged().flatMapLatest { key ->
            if (key == null) flowOf(null to ChatSnapshot(loading = false))
            else session.chats.observe(key).map { key to it }
        }.stateIn(scope, SharingStarted.Eagerly, null to ChatSnapshot(loading = false))
    override val content = combine(entries, target, messages, session.sender.outbox, wiring.current) { list, entry, observed, outbox, binding ->
        val key = key(entry?.id)
        val chat = observed.second.takeIf { observed.first == key } ?: ChatSnapshot()
        val seen = chat.items.mapNotNull { it.clientMsgId }.toSet()
        val history = chat.items.filter { it.kind == "text" && it.role in setOf("assistant", "user") && !it.text.isNullOrBlank() }
            .takeLast(8).map { CapsuleMessage(it.id, it.text.orEmpty(), it.role == "user") }
        val pending = outbox.filter { it.session == key && it.clientMessageId !in seen && it.state != DeliveryState.Sent }
            .map { CapsuleMessage(it.clientMessageId, it.text + when (it.state) {
                DeliveryState.WaitingForNetwork -> "\n等待网络"
                DeliveryState.Queued -> "\n已排队"
                DeliveryState.Unknown, DeliveryState.NeedsConfirmation, DeliveryState.Verifying -> "\n结果待核对，请打开会话"
                DeliveryState.Refused, DeliveryState.Rejected -> "\n${it.error ?: "发送未成功，请打开会话"}"
                else -> "\n发送中…"
            }, true) }
        CapsuleContent(entries = list, selectedId = entry?.id, messages = history + pending,
            canSend = if (entry?.id == MainEntry) binding?.scope?.path == session.identity
                else key != null && find(key)?.canSend == true, error = chat.error, available = true)
    }.combine(busy) { value, sending -> value.copy(busy = sending) }
        .combine(failure) { value, error -> value.copy(error = error ?: value.error) }
        .stateIn(scope, SharingStarted.Eagerly, CapsuleContent())

    private fun find(key: SessionKey): Session? =
        session.sessions.sessions.value.firstOrNull { it.key == key }
            ?: session.assistant.assistant.value.conversations.firstOrNull { it.key == key }
    private fun key(id: String?): SessionKey? =
        (session.sessions.sessions.value + session.assistant.assistant.value.conversations).firstOrNull { capsuleId(it.key) == id }?.key
    override fun shown(expanded: Boolean) { this.expanded.value = expanded }
    override fun select(id: String?) {
        val mainId = session.assistant.assistant.value.mainSession?.let(::capsuleId)
        selected.value = if (id == null || id == mainId) MainEntry else id
        failure.value = null
    }
    override suspend fun send(id: String?, text: String) {
        scope.coroutineContext.ensureActive()
        check(!busy.value) { "正在发送" }
        require(text.isNotBlank())
        val chosen = entries.value.firstOrNull { it.id == id } ?: error("会话已变化，请重新选择")
        busy.value = true
        failure.value = null
        try {
            if (chosen.id == MainEntry) {
                val binding = wiring.current.value?.takeIf { it.scope.path == session.identity } ?: error("连接已切换")
                binding.mainSender.sendToMain(text, UUID.randomUUID().toString())
            } else {
                val key = key(chosen.id) ?: error("会话已关闭")
                check(find(key)?.canSend == true) { "此会话仅可查看" }
                session.sender.send(key, text, UUID.randomUUID().toString())
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { failure.value = error.message ?: "发送失败"; throw error }
        finally { busy.value = false }
    }
    override suspend fun perform(id: String?, actionId: String) {
        scope.coroutineContext.ensureActive()
        require(actionId == "open")
        open(id)
    }
    override fun open(id: String?) {
        scope.coroutineContext.ensureActive()
        val key = key(id)
        check(id == null || id == MainEntry || key != null) { "会话已关闭" }
        openConversation(key, key == null || find(key)?.assistant == true)
    }
}
