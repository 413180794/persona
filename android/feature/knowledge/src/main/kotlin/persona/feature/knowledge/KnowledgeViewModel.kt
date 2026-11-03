@file:OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)

package persona.feature.knowledge

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import persona.core.contracts.HubScope
import persona.core.contracts.api.*
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
)

internal data class KnowledgePage(
    val scope: HubScope? = null,
    val filter: KnowledgeFilter = KnowledgeFilter(),
    val loading: Boolean = false,
    val paired: Boolean = true,
    val entries: List<KnowledgeEntry> = emptyList(),
    val tags: List<String> = emptyList(),
    val groups: List<KnowledgeSearchGroup> = emptyList(),
    val message: String? = null,
)

internal class KnowledgeViewModel(
    connection: KnowledgeConnection,
    private val dispatcher: CoroutineDispatcher = Dispatchers.Default,
) : ViewModel() {
    private val filter = MutableStateFlow(KnowledgeFilter())
    val page = combine(connection.current, filter) { repository, selection -> repository to selection }
        .flatMapLatest { (repository, selection) -> flow {
            val initial = KnowledgePage(scope = repository?.scope, filter = selection, paired = repository != null)
            if (repository == null) { emit(initial); return@flow }
            emit(initial.copy(loading = true))
            try {
                if (selection.query.isBlank()) {
                    val result = repository.list(selection.kind, selection.tag)
                    val entries = withContext(dispatcher) { result.items.filter { it.inScope() }.distinctBy { it.key } }
                    emit(initial.copy(entries = entries, tags = result.tags.orEmpty().distinct()))
                } else {
                    delay(300)
                    val hits = repository.search(selection.query)
                    val kinds = hits.mapNotNull { it.reference()?.first }.distinct()
                    val names = coroutineScope {
                        kinds.map { kind -> async { repository.list(kind).items } }.awaitAll().flatten().associateBy { it.key }
                    }
                    val groups = withContext(dispatcher) { searchGroups(hits, names) }
                    emit(initial.copy(groups = groups))
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { emit(initial.copy(message = loadMessage(error))) }
        } }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(0, 0), KnowledgePage(loading = true))

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
    val message: String? = null,
)
internal data class KnowledgeSending(val busy: Boolean = false, val message: String? = null)

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
    private val opened = Channel<Unit>(Channel.BUFFERED)
    val handoffs = opened.receiveAsFlow()
    val page = combine(connection.current, revision) { repository, _ -> repository }
        .flatMapLatest { repository -> flow {
            val initial = KnowledgeDetailPage(scope = repository?.scope, paired = repository != null, loading = repository != null)
            emit(initial)
            if (repository != null) try {
                emit(initial.copy(loading = false, detail = repository.detail(kind, id)))
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { emit(initial.copy(loading = false, message = loadMessage(error))) }
        } }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(0, 0), KnowledgeDetailPage())

    init {
        viewModelScope.launch {
            connection.current.collect {
                sendJob?.cancel()
                attempt = null
                _sending.value = KnowledgeSending()
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

    private fun checkCurrent(repository: KnowledgeRepository) {
        if (connection.current.value !== repository) throw CancellationException("Pairing changed")
    }
}

private fun loadMessage(error: Exception): String = if (error is IOException) "暂时连不上知识库，请稍后重试"
    else "知识库暂时无法读取，请重试"
