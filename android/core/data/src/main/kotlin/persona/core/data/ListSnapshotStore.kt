package persona.core.data

import java.io.File
import java.io.FileOutputStream
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.security.MessageDigest
import persona.core.contracts.JsonCodec
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString

/** Atomic, pairing-scoped snapshots. Failed reads never delete or replace the original file. */
class ListSnapshotStore(private val directory: File) {
    private val lock = Mutex()
    suspend fun read(key: String): String? = withContext(Dispatchers.IO) { lock.withLock {
        val file = File(directory, snapshotHash(key))
        if (file.exists()) file.readText() else null
    } }
    suspend fun write(key: String, value: String) = withContext(Dispatchers.IO) { lock.withLock {
        check(directory.isDirectory || directory.mkdirs())
        val temp = File.createTempFile("snapshot-", ".tmp", directory)
        try {
            FileOutputStream(temp).use { it.write(value.toByteArray()); it.fd.sync() }
            Files.move(temp.toPath(), File(directory, snapshotHash(key)).toPath(),
                StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
            Unit
        } finally { temp.delete() }
    } }
}

internal fun snapshotHash(value: String): String = MessageDigest.getInstance("SHA-256")
    .digest(value.toByteArray()).joinToString("") { "%02x".format(it) }

internal suspend inline fun <reified T> ListSnapshotStore?.restore(key: String): T? {
    var result: T? = null
    quietly { this?.read(key)?.let { result = JsonCodec.decodeFromString<T>(it) } }
    return result
}

internal suspend inline fun <reified T> ListSnapshotStore?.save(key: String, value: T) {
    quietly { this?.write(key, JsonCodec.encodeToString(value)) }
}
