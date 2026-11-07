package persona.core.hub.errors

import java.io.IOException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import persona.core.contracts.JsonCodec
import persona.core.hub.HubFailure
import persona.core.hub.OkHttpHubConnection

/** One POST /api/v1/error-reports. */
fun interface ErrorReportTransport {
    /** Sends [body] (`{"reports": [...]}`); the answer's status and body. Throws [IOException] when nothing came back. */
    suspend fun post(body: String): Pair<Int, String>
}

/** Through the connection's authenticated route: the same token, `X-Persona-Client` and TLS pinning as every request. */
class HttpErrorReportTransport(private val connection: OkHttpHubConnection) : ErrorReportTransport {
    override suspend fun post(body: String): Pair<Int, String> = try {
        connection.request("/api/v1/error-reports", "POST", body.toRequestBody("application/json".toMediaType())).use {
            it.code to it.body?.string().orEmpty()
        }
    } catch (failure: HubFailure) {
        // The connection throws for any non-2xx answer; the status tells keep from drop.
        failure.status?.let { it to failure.body.orEmpty() } ?: throw failure
    }
}

/**
 * Sends the queue while the hub says `error_reports` (docs/protocol/protocol.md "Error reports"), at most [MaxBatch] reports
 * and [MaxBatchBytes] a request. A 2xx answer takes the batch (the hub's `dropped` ones are not to be sent again
 * either); a 4xx refuses it for good, except 408 and 429, which are retried like a 5xx or no answer at all.
 */
class ErrorReportSender(private val queue: ErrorReportQueue) {
    private val gate = Mutex()

    /** What one [flush] did, for tests and logs. */
    data class Result(val sent: Int = 0, val dropped: Int = 0, val kept: Int = 0, val cleared: Boolean = false)

    suspend fun flush(features: Set<String>, transport: ErrorReportTransport): Result = gate.withLock {
        if (ErrorReportsFeature !in features) {
            queue.clear()
            return Result(cleared = true)
        }
        var sent = 0
        var dropped = 0
        var pending = queue.pending()
        while (pending.isNotEmpty()) {
            val batch = batchOf(pending)
            if (batch.isEmpty()) {
                // A single report over the request limit: the hub would answer 400 to it every time.
                queue.delete(listOf(pending.first()))
                dropped++
                pending = pending.drop(1)
                continue
            }
            val body = "{\"reports\":[" + batch.joinToString(",") { it.json } + "]}"
            val (status, answer) = try {
                transport.post(body)
            } catch (cancel: kotlinx.coroutines.CancellationException) {
                throw cancel
            } catch (_: IOException) {
                return Result(sent, dropped, kept = pending.size)
            }
            when {
                status in 200..299 -> {
                    queue.delete(batch)
                    sent += batch.size
                    // 200 {"enabled": false}: the extension went away between the hello and now; keep nothing.
                    if (disabled(answer)) {
                        queue.clear()
                        return Result(sent, dropped, cleared = true)
                    }
                }
                status in 400..499 && status != 408 && status != 429 -> {
                    queue.delete(batch)
                    dropped += batch.size
                }
                else -> return Result(sent, dropped, kept = pending.size)
            }
            pending = pending.drop(batch.size)
        }
        Result(sent, dropped)
    }

    private fun batchOf(pending: List<ErrorReportQueue.Entry>): List<ErrorReportQueue.Entry> {
        val batch = mutableListOf<ErrorReportQueue.Entry>()
        var bytes = Envelope
        for (entry in pending) {
            val size = entry.json.toByteArray().size + 1
            if (batch.size == MaxBatch || bytes + size > MaxBatchBytes) break
            batch += entry
            bytes += size
        }
        return batch
    }

    private fun disabled(answer: String): Boolean = runCatching {
        JsonCodec.parseToJsonElement(answer) as? JsonObject
    }.getOrNull()?.get("enabled")?.let { runCatching { it.jsonPrimitive.booleanOrNull }.getOrNull() } == false

    companion object {
        const val MaxBatch = 20
        /** Under the hub's 256 KB a request. */
        const val MaxBatchBytes = 240 * 1024
        private const val Envelope = 16
    }
}
