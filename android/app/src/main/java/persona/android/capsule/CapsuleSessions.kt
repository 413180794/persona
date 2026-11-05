package persona.android.capsule

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import persona.android.ui.ShellSession
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.inbox.InboxRowType
import persona.core.domain.inbox.buildInboxList
import persona.core.domain.sort.AgentPreferences
import persona.core.domain.sort.AgentSession
import persona.core.domain.sort.canSend
import persona.feature.agents.AgentsPreferencesSnapshot
import persona.feature.agents.listSession
import persona.feature.chat.ChatWiringSource
import persona.feature.chat.ChatAppBindings
import persona.feature.inbox.InboxSnapshot
import persona.platform.capsule.*
import java.util.UUID

internal fun capsuleId(key: SessionKey) = "${key.machineId.length}:${key.machineId}${key.sessionId}"
internal const val MainEntry = "persona:main"
internal const val UnreadSection = "unread"
internal const val MainSection = "main"
internal const val InboxSection = "inbox"

internal fun capsuleUnreadSessions(sessions: List<Session>, preferences: AgentsPreferencesSnapshot): List<Session> =
    sessions.distinctBy { it.key }.filter { session ->
        !session.assistant && !session.quiet && session.listSession(preferences).let {
            it.state == AgentState.Unread && !preferences.mutes.mutes(it)
        }
    }.sortedByDescending { it.updatedAt.orEmpty() }

private fun capsuleEntry(session: Session, requests: List<Interaction> = emptyList(), id: String = capsuleId(session.key)): CapsuleEntry {
    val pending = requests.filter { it.sessionKey == session.key }
    val state = when {
        pending.isNotEmpty() || session.agentState == AgentState.NeedsYou -> CapsuleState.NeedsYou
        session.agentState == AgentState.Working -> CapsuleState.Working
        session.agentState == AgentState.Unread -> CapsuleState.Done
        else -> CapsuleState.Idle
    }
    return CapsuleEntry(id, session.displayTitle,
        pending.firstOrNull()?.summary?.takeIf { it.isNotBlank() }
            ?: session.activity?.takeIf { it.isNotBlank() } ?: session.lastText.orEmpty(), state,
        if (pending.isNotEmpty()) listOf(CapsuleAction("open", "查看并处理")) else emptyList())
}

internal sealed interface CapsuleInboxTarget {
    data class Item(val id: String) : CapsuleInboxTarget
    data class Request(val key: SessionKey) : CapsuleInboxTarget
}
internal data class CapsuleInbox(val entries: List<CapsuleEntry>, val targets: Map<String, CapsuleInboxTarget>)

/** The same action rows as the app: selected-group records and already-qualified, merged request rows. */
internal fun capsuleInbox(snapshot: InboxSnapshot): CapsuleInbox {
    val requests = snapshot.interactions
    val rows = buildInboxList(snapshot.records.map { it.item }, requests, snapshot.sessions, snapshot.mutes)
        .groups.firstOrNull { it.id == "action" }?.rows.orEmpty()
    val records = snapshot.records.associateBy { it.item.id }
    val interactions = requests.associateBy { it.id }
    val targets = mutableMapOf<String, CapsuleInboxTarget>()
    val entries = rows.map { row -> when (row.type) {
        InboxRowType.Item -> {
            val record = records.getValue(row.id)
            val item = record.item
            val id = "inbox:item:${item.id}"
            targets[id] = CapsuleInboxTarget.Item(item.id)
            CapsuleEntry(id, item.title, item.summary?.takeIf { it.isNotBlank() } ?: item.preview, CapsuleState.NeedsYou)
        }
        InboxRowType.Interaction -> {
            val request = interactions.getValue(row.id)
            val id = "inbox:request:${request.id}"
            targets[id] = CapsuleInboxTarget.Request(SessionKey(request.machineId, request.sessionId))
            val preview = listOfNotNull(request.title.takeIf { it.isNotBlank() }, request.summary?.takeIf { it.isNotBlank() })
                .distinct().joinToString(" · ")
            CapsuleEntry(id, request.sessionTitle.ifBlank { request.title }, preview, CapsuleState.NeedsYou)
        }
    } }
    return CapsuleInbox(entries, targets)
}

private data class CapsuleAgents(
    val sessions: List<Session>, val main: SessionKey?, val requests: List<Interaction>, val preferences: AgentsPreferencesSnapshot,
)
private data class CapsuleCatalog(
    val sessions: List<Session> = emptyList(), val main: SessionKey? = null,
    val unread: List<CapsuleEntry> = emptyList(), val mainEntry: CapsuleEntry = CapsuleEntry(MainEntry, "主对话", "想让 Persona 做什么？", CapsuleState.Idle),
    val inbox: CapsuleInbox = CapsuleInbox(emptyList(), emptyMap()), val activity: List<CapsuleEntry> = emptyList(),
    val inboxAlerts: List<CapsuleEntry> = emptyList(),
) {
    val sections get() = listOf(CapsuleSection(UnreadSection, "未读", unread.size), CapsuleSection(MainSection, "主对话", list = false),
        CapsuleSection(InboxSection, "收件箱", inbox.entries.size))
}
private data class CapsuleSelection(val section: String, val entries: List<CapsuleEntry>, val id: String?, val key: SessionKey?)

/** Three entrances to the existing merged repositories; the capsule never acknowledges a preview as read. */
@OptIn(ExperimentalCoroutinesApi::class)
internal class CapsuleSessions(
    private val session: ShellSession,
    private val wiring: ChatWiringSource,
    private val scope: CoroutineScope,
    private val openInbox: (String?) -> Unit,
    private val openConversation: (SessionKey?, Boolean) -> Unit,
) : CapsuleContentSource {
    private val selectedSection = MutableStateFlow<String?>(null)
    private val selected = MutableStateFlow<Session?>(null)
    private val expanded = MutableStateFlow(false)
    private val busy = MutableStateFlow(false)
    private val failure = MutableStateFlow<String?>(null)
    private val completionEvents = MutableSharedFlow<CapsuleCompletion>()
    override val completions = completionEvents.asSharedFlow()
    private val completionTargets = mutableMapOf<String, Pair<SessionKey, Boolean>>()
    private val agents = combine(session.sessions.sessions, session.assistant.assistant, session.interactions.pending,
        session.preferences.snapshots) { list, assistant, pending, preferences ->
        CapsuleAgents((list + assistant.conversations).distinctBy { it.key }, assistant.mainSession, pending, preferences)
    }
    private val catalog = combine(agents, session.inbox.snapshot) { data, snapshot ->
        val inbox = capsuleInbox(snapshot)
        val activity = data.sessions.filter { current ->
            !current.quiet && (!data.preferences.mutes.mutes(AgentSession(current)) || current.key == data.main)
        }.map { current ->
            val projected = current.listSession(data.preferences)
            capsuleEntry(projected.session, data.requests)
        }.filter { it.state != CapsuleState.Idle }.sortedBy { it.state.ordinal }
        CapsuleCatalog(data.sessions, data.main, capsuleUnreadSessions(data.sessions, data.preferences).map { capsuleEntry(it) },
            data.sessions.firstOrNull { it.key == data.main }?.let { capsuleEntry(it, data.requests, MainEntry).copy(title = "主对话") }
                ?: CapsuleEntry(MainEntry, "主对话", "想让 Persona 做什么？", CapsuleState.Idle), inbox, activity,
            inbox.entries.filter { entry ->
                when (val target = inbox.targets.getValue(entry.id)) {
                    is CapsuleInboxTarget.Item -> true
                    is CapsuleInboxTarget.Request -> data.sessions.firstOrNull { it.key == target.key }?.let {
                        !it.quiet && !data.preferences.mutes.mutes(AgentSession(it))
                    } ?: !data.preferences.mutes.mutes(target.key.path, null)
                }
            })
    }.stateIn(scope, SharingStarted.Eagerly, CapsuleCatalog())
    private val selection = combine(catalog, selectedSection, selected) { data, section, chosen ->
        val entrance = section ?: if (data.unread.isNotEmpty()) UnreadSection else MainSection
        when (entrance) {
            MainSection -> CapsuleSelection(entrance, listOf(data.mainEntry), MainEntry, data.main)
            InboxSection -> CapsuleSelection(entrance, data.inbox.entries, null, null)
            else -> {
                val current = chosen?.let { old -> data.sessions.firstOrNull { it.key == old.key } ?: old }
                val retained = current?.let { capsuleEntry(it) }
                val entries = if (retained == null || data.unread.any { it.id == retained.id }) data.unread else data.unread + retained
                CapsuleSelection(UnreadSection, entries, retained?.id, current?.key)
            }
        }
    }.stateIn(scope, SharingStarted.Eagerly, CapsuleSelection(MainSection, emptyList(), MainEntry, null))
    private val messages = combine(selection, expanded) { target, shown -> if (shown) target.key else null }
        .distinctUntilChanged().flatMapLatest { key ->
            if (key == null) flowOf(null to ChatSnapshot(loading = false))
            else session.chats.observe(key).map { key to it }
        }.stateIn(scope, SharingStarted.Eagerly, null to ChatSnapshot(loading = false))
    override val content = combine(catalog, selection, messages, session.sender.outbox, wiring.current) { data, target, observed, outbox, binding ->
        val key = target.key
        val chat = observed.second.takeIf { observed.first == key } ?: ChatSnapshot()
        val seen = chat.items.mapNotNull { it.clientMsgId }.toSet()
        val history = chat.items.filter { it.kind == "text" && it.role in setOf("assistant", "user") && !it.text.isNullOrBlank() }
            .takeLast(8).map { CapsuleMessage(it.id, it.text.orEmpty(), it.role == "user") }
        val pending = outbox.filter { key != null && it.session == key && it.clientMessageId !in seen && it.state != DeliveryState.Sent }
            .map { CapsuleMessage(it.clientMessageId, it.text + when (it.state) {
                DeliveryState.WaitingForNetwork -> "\n等待网络"
                DeliveryState.Queued -> "\n已排队"
                DeliveryState.Unknown, DeliveryState.NeedsConfirmation, DeliveryState.Verifying -> "\n结果待核对，请打开会话"
                DeliveryState.Refused, DeliveryState.Rejected -> "\n${it.error ?: "发送未成功，请打开会话"}"
                else -> "\n发送中…"
            }, true) }
        CapsuleContent(entries = target.entries, selectedId = target.id, messages = history + pending,
            canSend = target.id != null && if (target.id == MainEntry && key == null) binding?.scope?.path == session.identity
                else key != null && find(key)?.canSend == true,
            error = chat.error, available = true, sections = data.sections, sectionId = target.section,
            badgeCount = data.unread.size + data.inboxAlerts.size, hasActivity = data.activity.isNotEmpty() || data.inboxAlerts.isNotEmpty(),
            headline = data.inboxAlerts.firstOrNull() ?: data.activity.firstOrNull())
    }.combine(busy) { value, sending -> value.copy(busy = sending) }
        .combine(failure) { value, error -> value.copy(error = error ?: value.error) }
        .stateIn(scope, SharingStarted.Eagerly, CapsuleContent())

    init {
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            val tracker = CapsuleCompletionTracker()
            val lock = Mutex()
            val observed = mutableSetOf<String>()
            val bindings = mutableMapOf<String, ChatAppBindings?>()
            val preferences = session.preferences.snapshots.map<AgentsPreferencesSnapshot, AgentsPreferencesSnapshot?> { it }
                .stateIn(this, SharingStarted.Eagerly, null)
            fun observe(machineId: String): StateFlow<ChatAppBindings?> {
                val source = wiring.bindingsFor(SessionKey(machineId, ""))
                if (observed.add(machineId)) {
                    launch(start = CoroutineStart.UNDISPATCHED) {
                        source.flatMapLatest { binding ->
                            binding?.connection?.state?.map { binding to it } ?: flowOf(null to null)
                        }.collect { (binding, state) -> lock.withLock {
                            if (source.value !== binding) return@withLock
                            if (bindings[machineId] !== binding || state != ConnectionState.Online) tracker.reset(machineId)
                            bindings[machineId] = binding
                        } }
                    }
                }
                return source
            }
            session.connection.events.onSubscription {
                // Subscribe before reading authority so live edges wait behind initialization, rather than disappearing.
                preferences.filterNotNull().first()
                (session.sessions as? VerifiedSessionsRepository)?.verifiedSessions().orEmpty().forEach { (machine, sessions) ->
                    val source = observe(machine)
                    lock.withLock {
                        bindings[machine] = source.value
                        if (source.value?.connection?.state?.value == ConnectionState.Online) {
                            tracker.update(SessionsEvent(machine, sessions)) { true }
                        }
                    }
                }
            }.filterIsInstance<SessionsEvent>().collect { event ->
                // After a disconnect only a newly observed online snapshot reestablishes the baseline.
                val source = observe(event.machineId)
                val finished = lock.withLock {
                    val binding = source.value
                    if (bindings[event.machineId] !== binding || binding?.connection?.state?.value != ConnectionState.Online) {
                        tracker.reset(event.machineId)
                        bindings[event.machineId] = binding
                    }
                    if (binding?.connection?.state?.value != ConnectionState.Online) emptyList()
                    else tracker.update(event) { requireNotNull(preferences.value).mutes.mutes(AgentSession(it)) }
                }
                finished.forEach { target ->
                    val completion = CapsuleCompletion(completionId(target), capsuleId(target.key), target.displayTitle)
                    completionTargets[completion.id] = target.key to target.assistant
                    completionEvents.emit(completion)
                }
            }
        }
    }

    override fun openCompletion(completion: CapsuleCompletion) {
        scope.coroutineContext.ensureActive()
        val (key, assistant) = completionTargets[completion.id] ?: error("完成提示已失效")
        check(capsuleId(key) == completion.targetId && find(key) != null) { "会话已关闭" }
        openConversation(key, assistant)
    }

    private fun find(key: SessionKey): Session? =
        session.sessions.sessions.value.firstOrNull { it.key == key }
            ?: session.assistant.assistant.value.conversations.firstOrNull { it.key == key }
    override fun shown(expanded: Boolean) {
        if (expanded && selectedSection.value == null) selectedSection.value = content.value.sectionId ?: MainSection
        this.expanded.value = expanded
    }
    override fun selectSection(id: String) {
        require(id in setOf(UnreadSection, MainSection, InboxSection))
        selectedSection.value = id
        selected.value = null
        failure.value = null
    }
    override fun select(id: String?) {
        scope.coroutineContext.ensureActive()
        if (id == MainEntry) { selectSection(MainSection); return }
        if (selection.value.section == InboxSection && id != null) { open(id); return }
        selected.value = id?.let { chosen -> catalog.value.sessions.firstOrNull { capsuleId(it.key) == chosen }
            ?: error("会话已关闭") }
        failure.value = null
    }
    override suspend fun send(id: String?, text: String) {
        scope.coroutineContext.ensureActive()
        check(!busy.value) { "正在发送" }
        require(text.isNotBlank())
        val target = selection.value
        check(id != null && target.id == id) { "会话已变化，请重新选择" }
        busy.value = true
        failure.value = null
        try {
            if (id == MainEntry && target.key == null) {
                val binding = wiring.current.value?.takeIf { it.scope.path == session.identity } ?: error("连接已切换")
                binding.mainSender.sendToMain(text, UUID.randomUUID().toString())
            } else {
                val key = target.key ?: error("会话已关闭")
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
        if (selection.value.section == InboxSection) {
            check(wiring.current.value?.scope?.path == session.identity) { "连接已切换" }
            if (id == null) { openInbox(null); return }
            when (val item = catalog.value.inbox.targets[id] ?: error("事项已变化，请重新选择")) {
                is CapsuleInboxTarget.Item -> openInbox(item.id)
                is CapsuleInboxTarget.Request -> openConversation(item.key, find(item.key)?.assistant == true)
            }
            return
        }
        val target = selection.value
        check(id == target.id || id == null) { "会话已变化，请重新选择" }
        if (id == null && target.section == UnreadSection) { openConversation(null, false); return }
        val key = target.key
        check(id == MainEntry || key != null && find(key) != null) { "会话已关闭" }
        openConversation(key, id == MainEntry || find(requireNotNull(key))?.assistant == true)
    }
}
