package persona.core.data

import persona.core.contracts.HubScope
import persona.core.contracts.JsonCodec
import persona.feature.inbox.ReplyTarget
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import java.io.File
import java.io.FileOutputStream
import java.nio.file.Files
import java.nio.file.StandardCopyOption

/** Private, non-backed-up storage. Commit the request before any network side effect. */
class InboxReplyJournal(private val directory: File, private val pairingScope: String) {
    init {
        val parts = pairingScope.split('/')
        require(parts.size == 2) { "Reply journal requires a hub and pairing generation" }
        HubScope(parts[0], parts[1])
    }
    private val mutex = Mutex()
    private var closed = false
    private fun file(scope: String): File {
        check(!closed) { "Reply journal is closed" }
        require(scope == pairingScope) { "Reply journal belongs to another pairing" }
        return File(directory, "replies.json")
    }
    suspend fun close() = mutex.withLock { closed = true }
    /** Pairing cleanup shares the same lock as reads and atomic writes. */
    suspend fun clear(scope: String) = withContext(Dispatchers.IO) { mutex.withLock {
        val destination = file(scope)
        check(!destination.exists() || destination.delete()) { "Cannot clear reply journal" }
    } }
    internal suspend fun read(scope: String): InboxReplyDocument = withContext(Dispatchers.IO) {
        mutex.withLock { readLocked(scope) }
    }
    internal suspend fun update(scope: String, change: (InboxReplyDocument) -> InboxReplyDocument): InboxReplyDocument =
        withContext(Dispatchers.IO) { mutex.withLock {
            val updated = change(readLocked(scope))
            check(directory.isDirectory || directory.mkdirs())
            val destination = file(scope)
            val temp = File.createTempFile("reply-", ".tmp", directory)
            try {
                FileOutputStream(temp).use { stream ->
                    stream.write(JsonCodec.encodeToString(updated).toByteArray())
                    stream.fd.sync()
                }
                Files.move(temp.toPath(), destination.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
            } finally { temp.delete() }
            updated
        } }
    private fun readLocked(scope: String): InboxReplyDocument = file(scope).let {
        if (it.exists()) JsonCodec.decodeFromString(it.readText()) else InboxReplyDocument()
    }
}

internal fun ReplyTarget.storageKey(): String = when (this) {
    is ReplyTarget.Inbox -> "inbox:$itemId"
    is ReplyTarget.Lark -> "lark:$feedId"
}
