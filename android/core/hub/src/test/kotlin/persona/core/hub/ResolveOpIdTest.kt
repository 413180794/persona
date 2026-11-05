package persona.core.hub

import persona.core.contracts.Resolution
import persona.core.contracts.api.InteractionGoneException
import java.io.IOException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.runTest
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.*
import org.junit.Test

/** An answer cut off by a restarting hub goes again under the same op id, for a bounded time (#1574). */
@OptIn(ExperimentalCoroutinesApi::class)
class ResolveOpIdTest {
    private class Sent(val path: String, val opId: String?, val body: String)

    private fun ok(path: String) = Response.Builder().request(Request.Builder().url("https://hub.example$path").build())
        .protocol(Protocol.HTTP_1_1).code(200).message("OK").body("{}".toResponseBody()).build()

    @Test fun aCutOffAnswerIsRetriedWithTheSameOpId() = runTest {
        val sent = mutableListOf<Sent>()
        val failures = ArrayDeque(listOf<Exception>(
            HubFailure.from(java.net.ConnectException("refused")),
            HubFailure(HubError.Server, "HTTP 503", status = 503),
            HubFailure.from(IOException("connection reset")),
        ))
        var waits = 0
        var ids = 0
        val transport = HubChatTransport({ path, _, body, headers ->
            val buffer = okio.Buffer().also { body!!.writeTo(it) }
            sent += Sent(path, headers[HubChatTransport.OP_ID_HEADER], buffer.readUtf8())
            failures.removeFirstOrNull()?.let { throw it }
            ok(path)
        }, awaitOnline = { waits++; true }, clockMillis = { testScheduler.currentTime }, newOpId = { "op-${++ids}" })

        transport.resolve("ix", Resolution(allow = true))
        assertEquals(4, sent.size)
        assertEquals(setOf("op-1"), sent.map { it.opId }.toSet())
        assertEquals(1, sent.map { it.body }.toSet().size)
        assertEquals("/api/v1/interactions/ix/resolve", (sent.first().path).zhAny())
        assertEquals(3, waits)

        // The next answer is another operation.
        transport.resolve("ix2", Resolution(allow = false))
        assertEquals("op-2", (sent.last().opId).zhAny())
    }

    @Test fun retriesStopAfterTheBoundAndOtherFailuresAreNotRetried() = runTest {
        var attempts = 0
        val bounded = HubChatTransport({ _, _, _, _ -> attempts++; throw HubFailure.from(IOException("reset")) },
            awaitOnline = { true }, clockMillis = { testScheduler.currentTime })
        val error = runCatching { bounded.resolve("ix", Resolution(allow = true)) }.exceptionOrNull()
        assertEquals(HubError.Transport, (error as HubFailure).kind)
        assertTrue(testScheduler.currentTime in HubChatTransport.RESOLVE_RETRY_MILLIS..(HubChatTransport.RESOLVE_RETRY_MILLIS + 1_000))
        assertTrue(attempts in 100..122)

        // The connection never came back within the bound: the answer is left to be checked.
        attempts = 0
        val offline = HubChatTransport({ _, _, _, _ -> attempts++; throw HubFailure.from(java.net.ConnectException("refused")) },
            awaitOnline = { false }, clockMillis = { testScheduler.currentTime })
        assertTrue(runCatching { offline.resolve("ix", Resolution(allow = true)) }.exceptionOrNull() is HubFailure)
        assertEquals(1, attempts)

        // A refusal from the hub itself is final; a gone interaction stays what it was.
        attempts = 0
        val refused = HubChatTransport({ _, _, _, _ -> attempts++; throw HubFailure(HubError.Server, "HTTP 500", status = 500) },
            awaitOnline = { true }, clockMillis = { testScheduler.currentTime })
        assertEquals(500, (runCatching { refused.resolve("ix", Resolution(allow = true)) }.exceptionOrNull() as HubFailure).status)
        assertEquals(1, attempts)
        val gone = HubChatTransport({ _, _, _, _ -> throw HubFailure(HubError.Server, "HTTP 410", status = 410) },
            awaitOnline = { true }, clockMillis = { testScheduler.currentTime })
        assertTrue(runCatching { gone.resolve("ix", Resolution(allow = true)) }.exceptionOrNull() is InteractionGoneException)
    }
}
