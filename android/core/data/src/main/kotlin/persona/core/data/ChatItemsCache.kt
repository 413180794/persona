package persona.core.data

import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.domain.lines.LineItem
import persona.core.domain.lines.Fold
import persona.core.domain.lines.TaskNote

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
