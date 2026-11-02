package jarvis.core.data

import androidx.room.withTransaction
import jarvis.core.contracts.HubScope
import jarvis.core.contracts.Item
import jarvis.core.contracts.JsonCodec
import jarvis.core.contracts.SessionKey
import jarvis.core.domain.ContinuityReducer
import jarvis.core.domain.lines.chatLines
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString

class SessionItemsCache(
    private val database: ItemsDatabase,
    private val scope: HubScope,
    private val budget: Long = CacheBudgets().items,
) : ChatItemsCache {
    init { require(budget >= 0) }
    private val active = mutableMapOf<SessionKey, Int>()
    internal var onRelease: () -> Unit = {}
    internal fun activeSessions(): Set<SessionKey> = synchronized(active) { active.keys.toSet() }

    override suspend fun retain(session: SessionKey) = database.withTransaction {
        synchronized(active) { active[session] = (active[session] ?: 0) + 1 }
        database.items().touch(scope.path, session.machineId, session.sessionId, System.currentTimeMillis())
    }

    override suspend fun release(session: SessionKey) {
        // A group's data can close while a page still watches one of its chats (the group merged into another or was
        // disconnected, docs/android.md「多 hub」): the cache is gone with it, there is nothing left to release.
        if (!database.isOpen) return
        try {
            database.withTransaction {
                synchronized(active) {
                    val count = (active[session] ?: 1) - 1
                    if (count == 0) active.remove(session) else active[session] = count
                }
                trim()
            }
        } catch (closed: IllegalStateException) {
            if (database.isOpen) throw closed
            return
        }
        onRelease()
    }

    private suspend fun trim() {
        val dao = database.items()
        val entries = dao.sizes(scope.path).map { BudgetEntry(SessionKey(it.machine, it.session), it.bytes, it.accessed) }
        val pinned = activeSessions()
        evictionCandidates(entries, budget) { it in pinned }.forEach {
            dao.clear(scope.path, it.machineId, it.sessionId)
            dao.clearCursor(scope.path, it.machineId, it.sessionId)
        }
    }

    fun observe(session: SessionKey) = database.items().observe(scope.path, session.machineId, session.sessionId).map { rows ->
        rows.map { JsonCodec.decodeFromString<Item>(it.json) }
    }

    override suspend fun load(session: SessionKey): CachedChat = database.withTransaction {
        database.items().touch(scope.path, session.machineId, session.sessionId, System.currentTimeMillis())
        trim()
        val cursor = database.items().cursor(scope.path, session.machineId, session.sessionId)
        CachedChat(observe(session).first(), cursor?.history, cursor?.covered ?: 0, cursor?.has_more)
    }

    /** Pass the reducer's authoritative merged view, not an isolated incremental page. */
    suspend fun save(session: SessionKey, reducer: ContinuityReducer) =
        save(session, CachedChat(reducer.items, reducer.history, reducer.covered, reducer.hasMore))

    suspend fun save(session: SessionKey, items: List<Item>) = save(session, CachedChat(items))

    override suspend fun save(session: SessionKey, snapshot: CachedChat) {
        val ordered = snapshot.items.distinctBy { it.id }.sortedWith(compareBy<Item> { it.seq }.thenBy { part(it) })
        val turns = chatLines(ordered.map { it.lineItem() }).turns
        val first = turns.takeLast(2).firstOrNull()?.users?.firstOrNull()?.item
        val start = first?.let { item -> ordered.indexOfFirst { it.id == item.id }.coerceAtLeast(0) } ?: 0
        val rows = ordered.drop(start).map { item ->
            // Inline originals belong to the display-sized image cache, not the items database.
            val cached = item.image?.let { item.copy(image = it.copy(data = "", lazy = true)) } ?: item
            ItemRow(scope.path, session.machineId, session.sessionId, item.seq, part(item), item.id, JsonCodec.encodeToString(cached))
        }
        database.withTransaction {
            val dao = database.items()
            dao.clear(scope.path, session.machineId, session.sessionId)
            dao.insert(rows)
            if (rows.isEmpty()) dao.clearCursor(scope.path, session.machineId, session.sessionId)
            else dao.putCursor(SessionCursorRow(scope.path, session.machineId, session.sessionId,
                snapshot.history, snapshot.covered, snapshot.hasMore == true || start > 0,
                System.currentTimeMillis()))
            trim()
        }
    }

    private fun part(item: Item): Int = item.id.substringAfterLast('#').toIntOrNull() ?: 0
}
