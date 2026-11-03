package persona.core.data

import persona.core.contracts.HubScope
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import java.io.File
import java.io.FileOutputStream
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.util.UUID

/** One owner per pairing. Closing shares the write lock so late callbacks cannot recreate data. */
class OneShotOperationJournal(private val directory: File, val scope: HubScope) : OneShotOperationRepository {
    private val lock = Mutex()
    private var closed = false
    private val file get() = File(directory, "operations.json")

    override suspend fun claim(kind: OneShotOperationKind, machineId: String, sessionId: String): OneShotClaim =
        withContext(Dispatchers.IO) { lock.withLock {
            check(!closed) { "Operation journal is closed" }
            val records = read()
            val existing = records.firstOrNull { it.kind == kind && it.machineId == machineId && it.sessionId == sessionId }
            if (existing != null) OneShotClaim(existing, false)
            else {
                val operation = OneShotOperation(UUID.randomUUID().toString(), kind, machineId, sessionId)
                write(records + operation)
                OneShotClaim(operation, true)
            }
        } }

    override suspend fun forget(operation: OneShotOperation) = update(operation) { records ->
        records.filterNot { it.id == operation.id }
    }

    private suspend fun update(operation: OneShotOperation, change: (List<OneShotOperation>) -> List<OneShotOperation>) =
        withContext(Dispatchers.IO) { lock.withLock {
            check(!closed) { "Operation journal is closed" }
            val records = read()
            check(records.any { it.id == operation.id && it.kind == operation.kind &&
                it.machineId == operation.machineId && it.sessionId == operation.sessionId }) { "Operation claim changed" }
            write(change(records))
        } }

    internal suspend fun close() = lock.withLock { closed = true }

    private fun read(): List<OneShotOperation> = if (file.exists()) JsonCodec.decodeFromString(file.readText()) else emptyList()

    private fun write(records: List<OneShotOperation>) {
        check(directory.isDirectory || directory.mkdirs()) { "Cannot create operation journal" }
        val temporary = File.createTempFile("operation-", ".tmp", directory)
        try {
            FileOutputStream(temporary).use {
                it.write(JsonCodec.encodeToString(records).toByteArray())
                it.fd.sync()
            }
            Files.move(temporary.toPath(), file.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
        } finally { temporary.delete() }
    }
}
