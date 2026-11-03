package persona.core.data

import androidx.room.withTransaction
import persona.core.contracts.HubScope
import persona.core.contracts.SessionKey
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.DraftAttachment
import persona.core.contracts.api.DraftSessionVersion
import persona.core.contracts.api.DraftKey
import persona.core.contracts.api.DraftRepository
import persona.core.contracts.api.DraftSnapshot
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString

class DraftStore(private val database: PersistentDatabase, private val scope: HubScope) : DraftRepository {
    override val structured = true
    override fun observe(key: DraftKey): Flow<DraftSnapshot> {
        val storage = key.storageKey()
        return database.records().observeDraft(scope.path, storage.machineId, storage.sessionId)
            .map { it.snapshot(key) }
    }

    override suspend fun read(key: DraftKey): DraftSnapshot {
        val storage = key.storageKey()
        val row = database.records().draft(scope.path, storage.machineId, storage.sessionId)
        return row.snapshot(key)
    }

    override suspend fun save(key: DraftKey, text: String, expectedVersion: Long): DraftSnapshot =
        save(key, text, expectedVersion, emptyList(), null)

    override suspend fun save(
        key: DraftKey, text: String, expectedVersion: Long,
        attachments: List<DraftAttachment>, sessionVersion: DraftSessionVersion?,
    ): DraftSnapshot = database.withTransaction {
        val storage = key.storageKey()
        val old = read(key)
        check(old.version == expectedVersion) { "Draft version changed" }
        val next = old.copy(text = text, version = Math.addExact(expectedVersion, 1),
            attachments = attachments, sessionVersion = sessionVersion)
        database.records().putDraft(DraftRow(scope.path, storage.machineId, storage.sessionId, text, next.version,
            JsonCodec.encodeToString(attachments), sessionVersion?.let { JsonCodec.encodeToString(it) }))
        next
    }

    private fun DraftRow?.snapshot(key: DraftKey) = DraftSnapshot(scope, key, this?.text.orEmpty(), this?.version ?: 0,
        this?.let { JsonCodec.decodeFromString<List<DraftAttachment>>(it.attachments) }.orEmpty(),
        this?.session_version?.let { JsonCodec.decodeFromString<DraftSessionVersion>(it) })

    // Preserve the original chat-only API and its on-disk keys.
    fun observe(session: SessionKey): Flow<DraftRow?> =
        database.records().observeDraft(scope.path, session.machineId, session.sessionId)

    suspend fun save(session: SessionKey, text: String, expectedVersion: Long): DraftRow {
        val draft = save(DraftKey.Chat(session), text, expectedVersion)
        return DraftRow(scope.path, session.machineId, session.sessionId, draft.text, draft.version)
    }
}

/** An empty machine is reserved for local composers; length prefixes prevent key collisions. */
internal fun DraftKey.storageKey(): SessionKey = when (this) {
    is DraftKey.Chat -> session.also { require(it.machineId.isNotBlank() && it.sessionId.isNotBlank()) }
    is DraftKey.Assistant -> SessionKey("", "assistant:$conversation")
    is DraftKey.Share -> SessionKey("", "share:$id")
    is DraftKey.LarkReply -> SessionKey("", "lark:${feedId.length}:$feedId$threadId")
    is DraftKey.InboxReply -> SessionKey("", "inbox:$itemId")
}
