package persona.core.hub

import persona.core.contracts.Endpoint
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.ConnectionState
import java.io.IOException
import java.net.InetAddress
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestCoroutineScheduler
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.*
import okhttp3.*
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import okio.ByteString
import org.junit.Assert.*
import org.junit.Test

/** A hub restart and short drops stay invisible; a reconnect resumes the hub's broadcast stream (#1574). */
@OptIn(ExperimentalCoroutinesApi::class)
class HubRestartTest {
    @Test fun theSocketUrlCarriesTheResumePoint() {
        Fixture().use { fixture ->
            val base = "https://hub.example:8443/".toHttpUrl()
            assertEquals("https://hub.example:8443/api/v1/ws", fixture.connection.socketUrl(base, null).toString())
            assertEquals("https://hub.example:8443/api/v1/ws?event_stream=s%201&event_seq=42",
                fixture.connection.socketUrl(base, "s 1" to 42L).toString())
        }
    }

    @Test fun broadcastsAreCountedAndTheNextSocketResumes() = runBlocking {
        Fixture().use { fixture ->
            fixture.hello = { """{"type":"hello","hub_id":"test-hub","event_stream":"s1","event_seq":5,"resumed":false}""" }
            val first = fixture.start()
            assertNull(first.request.url.queryParameter("event_stream"))
            assertFalse(fixture.connection.resumed)
            assertEquals("s1" to 5L, fixture.connection.resumePoint)
            first.receive("""{"event_seq":6,"type":"todos"}""")
            first.receive("""{"event_seq":7,"type":"agents"}""")
            first.receive("""{"type":"items","machine_id":"m","session_id":"s","items":[]}""")
            fixture.scheduler.runCurrent()
            assertEquals("s1" to 7L, fixture.connection.resumePoint)

            fixture.hello = { """{"type":"hello","hub_id":"test-hub","event_stream":"s1","event_seq":9,"resumed":true}""" }
            first.fail()
            fixture.advance(1000)
            val second = fixture.awaitSocket(2)
            assertEquals("s1", second.request.url.queryParameter("event_stream"))
            assertEquals("7", second.request.url.queryParameter("event_seq"))
            assertTrue(fixture.connection.resumed)
            // Resumed: the replay, not the hello, moves the point, so a drop mid-replay loses nothing.
            assertEquals("s1" to 7L, fixture.connection.resumePoint)
            second.receive("""{"event_seq":8,"type":"todos"}""")
            second.receive("""{"event_seq":9,"type":"inbox"}""")
            fixture.scheduler.runCurrent()
            assertEquals("s1" to 9L, fixture.connection.resumePoint)

            // A hub that lost the stream starts a new one; the app starts over from its hello.
            fixture.hello = { """{"type":"hello","hub_id":"test-hub","event_stream":"s2","event_seq":3,"resumed":false}""" }
            second.fail()
            fixture.advance(1000)
            val third = fixture.awaitSocket(3)
            assertEquals("s1", third.request.url.queryParameter("event_stream"))
            assertFalse(fixture.connection.resumed)
            assertEquals("s2" to 3L, fixture.connection.resumePoint)
        }
    }

    @Test fun anOlderHubIsNeverAskedToResume() = runBlocking {
        Fixture().use { fixture ->
            val first = fixture.start()
            first.receive("""{"event_seq":6,"type":"todos"}""")
            fixture.scheduler.runCurrent()
            assertNull(fixture.connection.resumePoint)
            first.fail()
            fixture.advance(1000)
            val second = fixture.awaitSocket(2)
            assertNull(second.request.url.queryParameter("event_stream"))
            assertFalse(fixture.connection.resumed)
        }
    }

    @Test fun aRestartingHubIsRetriedQuicklyWhileTheStateStaysOnline() = runBlocking {
        Fixture().use { fixture ->
            val first = fixture.start()
            assertEquals(ConnectionState.Online, fixture.connection.state.value)
            fixture.reachable = false
            first.receive("""{"type":"hub_restarting","back_in_ms":3000}""")
            first.close(1001, "hub restarting")
            fixture.scheduler.runCurrent()
            assertEquals(ConnectionState.Online, fixture.connection.state.value)
            fixture.pump(149)
            assertEquals(1, fixture.sockets.size)
            fixture.pump(251)
            fixture.awaitSocket(2)
            // Steady retries, no exponential backoff (which would make at most four more in ten seconds).
            fixture.pump(10_000)
            assertTrue("tries: ${fixture.sockets.size}", fixture.sockets.size >= 8)
            fixture.pump(29_600 - 10_400)
            assertEquals(ConnectionState.Online, fixture.connection.state.value)
            fixture.pump(400)
            assertEquals(ConnectionState.Connecting, fixture.connection.state.value)

            // Back: Online again, and the next drop is an ordinary one again.
            fixture.reachable = true
            fixture.pump(20_000)
            assertEquals(ConnectionState.Online, fixture.connection.state.value)
        }
    }

    @Test fun theNextHubAnsweringWithinTheGraceIsNeverShown() = runBlocking {
        Fixture().use { fixture ->
            fixture.hello = { """{"type":"hello","hub_id":"test-hub","event_stream":"s1","event_seq":1,"resumed":true}""" }
            val first = fixture.start()
            val states = fixture.record()
            first.receive("""{"type":"hub_restarting","back_in_ms":3000}""")
            first.close(1001, "hub restarting")
            fixture.pump(400)
            fixture.awaitSocket(2)
            fixture.pump(4_000)
            assertEquals(listOf(ConnectionState.Online), states)
            assertTrue(fixture.connection.resumed)
        }
    }

    @Test fun anOrdinaryDropShowsAfterEightSeconds() = runBlocking {
        Fixture().use { fixture ->
            val first = fixture.start()
            fixture.reachable = false
            first.fail()
            fixture.scheduler.runCurrent()
            fixture.pump(7_900)
            assertEquals(ConnectionState.Online, fixture.connection.state.value)
            fixture.pump(100)
            assertEquals(ConnectionState.Connecting, fixture.connection.state.value)
        }
    }

    @Test fun aFreshStartAfterAVisibleLengthDropSurfacesSoScreensRefetch() = runBlocking {
        Fixture().use { fixture ->
            val first = fixture.start()
            val seen = fixture.record()
            fixture.reachable = false
            first.fail()
            fixture.scheduler.runCurrent()
            fixture.pump(2_000)
            assertEquals(ConnectionState.Online, fixture.connection.state.value)
            fixture.reachable = true
            fixture.pump(6_000)
            // Not resumed (an older hub): Updating then Online, as before the grace window, so refetches run.
            assertEquals(listOf(ConnectionState.Online, ConnectionState.Updating, ConnectionState.Online), seen)
            assertEquals(ConnectionState.Online, fixture.connection.state.value)
        }
    }

    private class Fixture : AutoCloseable {
        val scheduler = TestCoroutineScheduler()
        val sockets = mutableListOf<FakeSocket>()
        @Volatile var hello: () -> String = { """{"type":"hello","hub_id":"test-hub","features":[]}""" }
        /** False: each socket fails as soon as it is opened (a hub not yet answering). */
        @Volatile var reachable = true
        private val server = MockWebServer().apply {
            dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest) = MockResponse()
                    .setBody("""{"ok":true,"hub_id":"test-hub"}""")
            }
            start(InetAddress.getLoopbackAddress(), 0)
        }
        val connection = OkHttpHubConnection(
            HubConnectionConfig("test-hub", null, "test-token", listOf(Endpoint(server.url("/").toString(), "lan")), "test"),
            HubNetwork("test-network", transport = HubNetworkTransport.Wifi), dispatcher = StandardTestDispatcher(scheduler),
            clockMillis = { scheduler.currentTime },
            webSocketFactory = object : WebSocket.Factory {
                override fun newWebSocket(request: Request, listener: WebSocketListener): WebSocket {
                    val socket = FakeSocket(request, listener)
                    synchronized(sockets) { sockets += socket }
                    if (!reachable) {
                        socket.fail()
                        return socket
                    }
                    listener.onOpen(socket, Response.Builder().request(request).protocol(Protocol.HTTP_1_1)
                        .code(101).message("Switching Protocols").build())
                    socket.receive(hello())
                    return socket
                }
            },
        )

        suspend fun start() = awaitSocket(1)

        /** Every state published from now on, in order. */
        fun record(): MutableList<ConnectionState> {
            val states = mutableListOf<ConnectionState>()
            CoroutineScope(StandardTestDispatcher(scheduler) + watchers).launch { connection.state.collect { states += it } }
            scheduler.runCurrent()
            return states
        }
        private val watchers = Job()

        suspend fun awaitSocket(count: Int): FakeSocket {
            withTimeout(5000) {
                while (sockets.size < count) {
                    scheduler.runCurrent()
                    delay(10)
                }
            }
            repeat(5) { scheduler.runCurrent(); delay(5) }
            return sockets.last()
        }

        fun advance(millis: Long) { scheduler.advanceTimeBy(millis); scheduler.runCurrent() }

        /** Moves virtual time in small steps, letting the real health probes in between finish. */
        suspend fun pump(millis: Long) {
            var left = millis
            while (left > 0) {
                val step = minOf(50L, left)
                scheduler.advanceTimeBy(step)
                repeat(4) { scheduler.runCurrent(); delay(3) }
                left -= step
            }
            scheduler.runCurrent()
        }

        override fun close() { watchers.cancel(); connection.close(); scheduler.runCurrent(); server.close() }
    }

    private class FakeSocket(val request: Request, private val listener: WebSocketListener) : WebSocket {
        val sent = mutableListOf<JsonObject>()
        var cancelled = false
        fun receive(text: String) = listener.onMessage(this, text)
        fun fail() = listener.onFailure(this, IOException("connection reset"), null)
        override fun request() = request
        override fun queueSize() = 0L
        override fun send(text: String): Boolean { sent += JsonCodec.parseToJsonElement(text).jsonObject; return true }
        override fun send(bytes: ByteString) = true
        override fun close(code: Int, reason: String?): Boolean {
            if (reason != null) listener.onClosing(this, code, reason)
            return true
        }
        override fun cancel() { cancelled = true }
    }
}
