@file:OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)

package persona.feature.knowledge

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import persona.core.contracts.HubScope
import persona.core.contracts.Session
import persona.core.contracts.api.*
import persona.core.designsystem.operationFeedback
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.*
import java.io.IOException
import java.util.UUID

internal data class KnowledgeFilter(
    val kind: KnowledgeKind = KnowledgeKind.Projects,
    val tag: String? = null,
    val query: String = "",
    val revision: Int = 0,
) {
    fun sameView(other: KnowledgeFilter) = kind == other.kind && tag == other.tag && query == other.query
}

internal data class KnowledgePage(
    val scope: HubScope? = null,
    val filter: KnowledgeFilter = KnowledgeFilter(),
    val loading: Boolean = false,
    val paired: Boolean = true,
    val entries: List<KnowledgeEntry> = emptyList(),
    val tags: List<String> = emptyList(),
    val groups: List<KnowledgeSearchGroup> = emptyList(),
    val message: String? = null,
    /** The list or search shown is the answer to [filter] (not what an earlier one left while this one loads). */
    val loaded: Boolean = false,
)

/** Tagging saved on an entry's page; the list (another view model) reloads to show it. */
internal object KnowledgeEdits {
    val revision = MutableStateFlow(0)
    fun changed() = revision.update { it + 1 }
}

internal class KnowledgeViewModel(
    connection: KnowledgeConnection,
    private val dispatcher: CoroutineDispatcher = Dispatchers.Default,
    edits: StateFlow<Int> = KnowledgeEdits.revision,
) : ViewModel() {
    private val filter = MutableStateFlow(KnowledgeFilter())
    /** Names of the entries lists have shown, per pairing: search hits carry little more than an id. */
    private val names = mutableMapOf<String, KnowledgeEntry>()
    private var namesScope: HubScope? = null
    private var last: KnowledgePage? = null

    val page = combine(connection.current, filter, edits) { repository, selection, _ -> repository to selection }
        .flatMapLatest { (repository, selection) -> flow {
            val initial = KnowledgePage(scope = repository?.scope, filter = selection, paired = repository != null)
            if (repository == null) { emit(initial); return@flow }
            // Reloading the same view keeps what it showed until the answer comes (a refresh, a save on an entry's page).
            val shown = last?.takeIf { it.scope == repository.scope && it.filter.sameView(selection) && it.message == null }
            emit(initial.copy(loading = true, entries = shown?.entries.orEmpty(), tags = shown?.tags.orEmpty(), groups = shown?.groups.orEmpty()))
            try {
                if (selection.query.isBlank()) {
                    val result = repository.list(selection.kind, selection.tag)
                    val entries = withContext(dispatcher) { result.items.filter { it.inScope() }.distinctBy { it.key } }
                    remember(repository.scope, entries)
                    emit(initial.copy(entries = entries, tags = result.tags.orEmpty().distinct(), loaded = true))
                } else {
                    delay(300)
                    val hits = repository.search(selection.query.trim())
                    val missing = hits.mapNotNull { it.reference()?.first }.distinct().filter { kind -> !known(repository.scope, kind) }
                    val listed = coroutineScope { missing.map { kind -> async { runCatching { repository.list(kind).items }.getOrNull() } }.awaitAll() }
                    listed.filterNotNull().forEach { remember(repository.scope, it) }
                    val index = synchronized(names) { names.toMap() }
                    val groups = withContext(dispatcher) { searchGroups(hits, index) }
                    emit(initial.copy(groups = groups, loaded = true))
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { emit(initial.copy(message = loadMessage(error))) }
        } }
        .onEach { if (!it.loading) last = it }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(0, 0), KnowledgePage(loading = true))

    private fun remember(scope: HubScope, entries: List<KnowledgeEntry>) = synchronized(names) {
        if (namesScope != scope) { names.clear(); namesScope = scope }
        entries.forEach { names[it.key] = it }
    }

    private fun known(scope: HubScope, kind: KnowledgeKind) = synchronized(names) {
        namesScope == scope && names.keys.any { it.startsWith("${kind.wire}/") }
    }

    fun kind(kind: KnowledgeKind) { filter.update { it.copy(kind = kind, tag = null) } }
    fun tag(tag: String?) { filter.update { it.copy(tag = tag) } }
    fun query(query: String) { filter.update { it.copy(query = query) } }
    fun refresh() { filter.update { it.copy(revision = it.revision + 1) } }
}

internal data class KnowledgeDetailPage(
    val scope: HubScope? = null,
    val loading: Boolean = true,
    val paired: Boolean = true,
    val detail: KnowledgeDetail? = null,
    val agents: List<Session> = emptyList(),
    val message: String? = null,
)
internal data class KnowledgeSending(val busy: Boolean = false, val message: String? = null)

/** The tag editor's save: [saved] counts the saves that went through, so the sheet closes on each. */
internal data class KnowledgeTagging(val busy: Boolean = false, val error: String? = null, val saved: Int = 0)

internal class KnowledgeDetailViewModel(
    private val kind: KnowledgeKind,
    private val id: String,
    private val connection: KnowledgeConnection,
    private val sender: MainConversationSender,
) : ViewModel() {
    private val revision = MutableStateFlow(0)
    private var sendJob: Job? = null
    private var attempt: Pair<String, String>? = null
    private val _sending = MutableStateFlow(KnowledgeSending())
    val sending = _sending.asStateFlow()
    private val _tagging = MutableStateFlow(KnowledgeTagging())
    val tagging = _tagging.asStateFlow()
    private val opened = Channel<Unit>(Channel.BUFFERED)
    val handoffs = opened.receiveAsFlow()
    private var last: KnowledgeDetailPage? = null
    val page = combine(connection.current, revision) { repository, _ -> repository }
        .flatMapLatest { repository -> flow {
            val initial = KnowledgeDetailPage(scope = repository?.scope, paired = repository != null, loading = repository != null)
            // A reload (pull, a saved tag) keeps the page on screen until the new one comes.
            val shown = last?.takeIf { repository != null && it.scope == repository.scope && it.detail != null }
            emit(initial.copy(detail = shown?.detail, agents = shown?.agents.orEmpty()))
            if (repository != null) try {
                val response = repository.entry(kind, id)
                emit(initial.copy(loading = false, detail = response.detail, agents = projectAgents(response.agents)))
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { emit(initial.copy(loading = false, message = loadMessage(error))) }
        } }
        .onEach { if (!it.loading) last = it }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(0, 0), KnowledgeDetailPage())

    init {
        viewModelScope.launch {
            connection.current.collect {
                sendJob?.cancel()
                attempt = null
                _sending.value = KnowledgeSending()
                _tagging.value = KnowledgeTagging()
                while (opened.tryReceive().isSuccess) { }
            }
        }
    }

    fun refresh() { revision.update { it + 1 } }

    fun ask(question: String) {
        if (_sending.value.busy || question.isBlank()) return
        val repository = connection.current.value ?: return
        val currentPage = page.value
        val detail = currentPage.detail ?: return
        if (currentPage.scope != repository.scope) return
        val text = knowledgeContext(detail) + "\n\n" + question.trim()
        val messageId = attempt?.takeIf { it.first == text }?.second ?: UUID.randomUUID().toString()
        attempt = text to messageId
        _sending.value = KnowledgeSending(busy = true)
        sendJob = viewModelScope.launch {
            try {
                checkCurrent(repository)
                // The data layer owns durable delivery, offline queueing and deduplication.
                sender.sendToMain(text, messageId)
                checkCurrent(repository)
                attempt = null
                _sending.value = KnowledgeSending(message = "已交给 Persona，发送状态请在对话中查看")
                opened.send(Unit)
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) {
                _sending.value = KnowledgeSending(message = "暂时没能交给 Persona，请重试；已提交的消息会沿用同一发送标识")
            }
        }
    }

    /** Saves hand-made tagging, then reads the entry again and has the list reload. */
    fun saveTags(update: KnowledgeUpdate) {
        if (_tagging.value.busy) return
        val repository = connection.current.value ?: return
        val saved = _tagging.value.saved
        _tagging.value = KnowledgeTagging(busy = true, saved = saved)
        viewModelScope.launch {
            try {
                checkCurrent(repository)
                repository.update(kind, id, update)
                checkCurrent(repository)
                _tagging.value = KnowledgeTagging(saved = saved + 1)
                refresh()
                KnowledgeEdits.changed()
            } catch (cancelled: CancellationException) {
                _tagging.value = KnowledgeTagging(saved = saved)
                throw cancelled
            } catch (error: Exception) {
                _tagging.value = KnowledgeTagging(error = "没有保存：${operationFeedback(error).message}", saved = saved)
            }
        }
    }

    fun dismissTagError() { _tagging.update { it.copy(error = null) } }

    private fun checkCurrent(repository: KnowledgeRepository) {
        if (connection.current.value !== repository) throw CancellationException("Pairing changed")
    }
}

private fun loadMessage(error: Exception): String = if (error is IOException) "暂时连不上知识库，请稍后重试"
    else "知识库暂时无法读取，请重试"
