package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import kotlin.coroutines.coroutineContext
import kotlinx.serialization.SerializationException
import okhttp3.HttpUrl
import okhttp3.Response
import java.io.File

/** Uses the active pairing's authenticated route, including its certificate policy. */
class HubSessionFiles internal constructor(
    private val request: suspend (String) -> Response,
    private val decoding: CoroutineDispatcher = Dispatchers.Default,
    /** An unbuffered request, for bodies read as a stream (file/download). */
    private val stream: suspend (String) -> Response = request,
) : SessionFiles {
    constructor(connection: OkHttpHubConnection, decoding: CoroutineDispatcher = Dispatchers.Default) :
        this({ path -> connection.request(path, "GET", null) }, decoding, { path -> connection.request(path, "GET", null, buffered = false) })

    override suspend fun directory(machineId: String, cwd: String, path: String) =
        get<SessionDirectory>(machineId, "fs", cwd, "path" to path)
    override suspend fun file(machineId: String, cwd: String, path: String) =
        get<SessionFileContent>(machineId, "file", cwd, "path" to path)
    override suspend fun status(machineId: String, cwd: String) =
        get<SessionGitStatus>(machineId, "git/status", cwd)
    override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) =
        get<SessionGitDiff>(machineId, "git/diff", cwd, "path" to path, "staged" to if (staged) "1" else "0")
    override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int): SessionGitLog {
        require(skip >= 0 && limit in 1..100)
        return get(machineId, "git/log", cwd, "skip" to skip.toString(), "limit" to limit.toString())
    }
    override suspend fun show(machineId: String, cwd: String, hash: String) =
        get<SessionGitShow>(machineId, "git/show", cwd, "hash" to hash)
    override suspend fun stat(machineId: String, cwd: String, paths: List<String>) =
        get<SessionPathStats>(machineId, "stat", cwd, "paths" to paths.joinToString("\n"))

    override suspend fun download(machineId: String, cwd: String, path: String, into: File): Long {
        require(machineId.isNotBlank() && cwd.isNotBlank())
        val url = apiUrl(machineId, "file/download", cwd, "path" to path)
        return stream(url).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            withContext(Dispatchers.IO) {
                into.parentFile?.mkdirs()
                // A part file renamed at the end: a download cut short never passes for the whole file.
                // Its own part name: two pages fetching one file don't write into each other.
                val part = File(into.path + ".part-" + System.nanoTime())
                try {
                    var n = 0L
                    part.outputStream().use { out ->
                        requireNotNull(response.body).byteStream().use { input ->
                            val buffer = ByteArray(64 * 1024)
                            while (true) {
                                // Leaving the page stops the download between chunks, not at the end of the file.
                                coroutineContext.ensureActive()
                                val read = input.read(buffer)
                                if (read < 0) break
                                out.write(buffer, 0, read)
                                n += read
                            }
                        }
                    }
                    if (!part.renameTo(into)) throw java.io.IOException("rename")
                    n
                } finally {
                    part.delete()
                }
            }
        }
    }

    private fun apiUrl(machineId: String, operation: String, cwd: String, vararg query: Pair<String, String>): String {
        // The placeholder origin is discarded; only encoded path and query reach the connection.
        val url = HttpUrl.Builder().scheme("https").host("example.com")
            .addPathSegments("api/v1/machines").addPathSegment(machineId).addPathSegments(operation)
            .addQueryParameter("cwd", cwd).apply { query.forEach { (key, value) -> addQueryParameter(key, value) } }.build()
        return url.encodedPath + "?" + url.encodedQuery
    }

    private suspend inline fun <reified T> get(
        machineId: String, operation: String, cwd: String, vararg query: Pair<String, String>,
    ): T {
        require(machineId.isNotBlank() && cwd.isNotBlank())
        return request(apiUrl(machineId, operation, cwd, *query)).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            withContext(decoding) {
                try { JsonCodec.decodeFromString<T>(requireNotNull(response.body).string()) }
                catch (error: SerializationException) { throw HubFailure(HubError.Protocol, retryable = false, cause = error) }
            }
        }
    }
}
