package jarvis.core.data

import jarvis.core.contracts.Item
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.parseTime
import jarvis.core.domain.lines.LineItem
import jarvis.core.domain.lines.Fold
import jarvis.core.domain.lines.TaskNote

data class CachedChat(
    val items: List<Item> = emptyList(),
    val history: String? = null,
    val covered: Int = 0,
    val hasMore: Boolean? = null,
)

interface ChatItemsCache {
    suspend fun retain(session: SessionKey) {}
    suspend fun release(session: SessionKey) {}
    suspend fun load(session: SessionKey): CachedChat
    suspend fun save(session: SessionKey, snapshot: CachedChat)
}

internal fun Item.lineItem() = LineItem(this,
    fold?.let { Fold(it.items, it.tools, it.thoughts, it.throughSeq, it.throughId, parseTime(it.until)) },
    task?.let { TaskNote(it.kind, it.title, it.status, it.toolUseId, it.taskId, it.detail, it.exitCode) })
