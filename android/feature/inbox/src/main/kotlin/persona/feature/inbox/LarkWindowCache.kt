package persona.feature.inbox

import android.util.AtomicFile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import java.io.File
import java.security.MessageDigest

interface LarkWindowCache {
    suspend fun read(scope: String, feed: String, around: Long?): JsonObject?
    suspend fun write(scope: String, feed: String, around: Long?, page: JsonObject)
}

/** Store under the app's private, backup-excluded directory. Scope includes hub and account identity. */
class FileLarkWindowCache(private val directory: File,
    private val writeFiles: suspend (() -> Unit) -> Unit = { it() },
) : LarkWindowCache {
    private fun file(scope: String, feed: String, around: Long?): AtomicFile {
        val key = listOf(scope, feed, around?.toString().orEmpty()).joinToString(":") { "${it.length}:$it" }
        val name = MessageDigest.getInstance("SHA-256").digest(key.toByteArray()).joinToString("") { "%02x".format(it) }
        return AtomicFile(File(directory, "$name.json"))
    }
    override suspend fun read(scope: String, feed: String, around: Long?) = withContext(Dispatchers.IO) {
        var page: JsonObject? = null
        writeFiles {
            page = runCatching { Json.parseToJsonElement(file(scope, feed, around).readFully().decodeToString()).jsonObject }.getOrNull()
        }
        page
    }
    override suspend fun write(scope: String, feed: String, around: Long?, page: JsonObject) = withContext(Dispatchers.IO) { writeFiles {
        directory.mkdirs()
        val file = file(scope, feed, around)
        val stream = file.startWrite()
        try { stream.write(page.toString().toByteArray()); file.finishWrite(stream) }
        catch (error: Exception) { file.failWrite(stream); throw error }
    } }
}
