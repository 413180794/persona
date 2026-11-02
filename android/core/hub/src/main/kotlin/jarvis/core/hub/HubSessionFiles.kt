package jarvis.core.hub

import jarvis.core.contracts.JsonCodec
import jarvis.core.contracts.api.*
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerializationException
import okhttp3.HttpUrl
import okhttp3.Response

/** Uses the active pairing's authenticated route, including its certificate policy. */
class HubSessionFiles internal constructor(
    private val request: suspend (String) -> Response,
    private val decoding: CoroutineDispatcher = Dispatchers.Default,
) : SessionFiles {
    constructor(connection: OkHttpHubConnection, decoding: CoroutineDispatcher = Dispatchers.Default) :
        this({ path -> connection.request(path, "GET", null) }, decoding)

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

    private suspend inline fun <reified T> get(
        machineId: String, operation: String, cwd: String, vararg query: Pair<String, String>,
    ): T {
        require(machineId.isNotBlank() && cwd.isNotBlank())
        // The placeholder origin is discarded; only encoded path and query reach the connection.
        val url = HttpUrl.Builder().scheme("https").host("example.com")
            .addPathSegments("api/v1/machines").addPathSegment(machineId).addPathSegments(operation)
            .addQueryParameter("cwd", cwd).apply { query.forEach { (key, value) -> addQueryParameter(key, value) } }.build()
        return request(url.encodedPath + "?" + url.encodedQuery).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            withContext(decoding) {
                try { JsonCodec.decodeFromString<T>(requireNotNull(response.body).string()) }
                catch (error: SerializationException) { throw HubFailure(HubError.Protocol, retryable = false, cause = error) }
            }
        }
    }
}
