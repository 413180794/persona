package persona.core.hub

import persona.core.contracts.Endpoint
import java.io.IOException
import java.io.OutputStream
import java.net.InetAddress
import java.net.Socket
import java.security.MessageDigest
import java.util.Base64
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import javax.net.SocketFactory
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.Assert.*
import org.junit.Test

class HubHttpClientCloseTest {
    /**
     * Pairing closes its client in a finally on the main thread. Closing a pooled TLS connection writes close_notify,
     * which Android refuses there (NetworkOnMainThreadException): the pairing was saved, the page said it failed.
     */
    @Test fun closeNeverWritesToTheNetworkOnTheCallersThread() = runBlocking {
        val certificate = HeldCertificate.Builder().commonName("example.com").build()
        val tls = HandshakeCertificates.Builder().heldCertificate(certificate).build()
        MockWebServer().use { server ->
            server.useHttps(tls.sslSocketFactory(), false)
            server.start(InetAddress.getLoopbackAddress(), 0)
            server.enqueue(MockResponse().setBody("""{"ok":true,"hub_id":"test-hub"}"""))
            val sockets = GuardedSockets()
            val client = HubHttpClient("test-hub", fingerprint(certificate), "test", socketFactory = sockets)
            val endpoint = Endpoint(server.url("/").newBuilder().host(InetAddress.getLoopbackAddress().hostAddress!!).build().toString(), "lan")
            assertEquals("test-hub", (client.health(endpoint).hubId).zhAny()) // leaves the TLS connection in the pool

            sockets.forbidden = Thread.currentThread()
            client.close() // must not throw, as on Android's main thread
            client.close() // a second close is harmless
            assertTrue("the pooled TLS connection is still closed", sockets.closed.await(5, TimeUnit.SECONDS))
            assertNull(sockets.violation)
        }
    }

    /** Sockets whose writes fail on [forbidden], like StrictMode's network-on-main-thread check. */
    private class GuardedSockets : SocketFactory() {
        @Volatile var forbidden: Thread? = null
        @Volatile var violation: Throwable? = null
        val closed = CountDownLatch(1)

        private fun guarded() = object : Socket() {
            override fun getOutputStream(): OutputStream {
                val out = super.getOutputStream()
                return object : OutputStream() {
                    private fun check() {
                        if (Thread.currentThread() === forbidden) {
                            throw IOException("network write on the caller's thread").also { violation = it }
                        }
                    }
                    override fun write(b: Int) { check(); out.write(b) }
                    override fun write(b: ByteArray, off: Int, len: Int) { check(); out.write(b, off, len) }
                    override fun flush() { check(); out.flush() }
                }
            }
            override fun close() { super.close(); closed.countDown() }
        }

        override fun createSocket() = guarded()
        override fun createSocket(host: String, port: Int) = throw UnsupportedOperationException()
        override fun createSocket(host: String, port: Int, local: InetAddress, localPort: Int) = throw UnsupportedOperationException()
        override fun createSocket(host: InetAddress, port: Int) = throw UnsupportedOperationException()
        override fun createSocket(host: InetAddress, port: Int, local: InetAddress, localPort: Int) = throw UnsupportedOperationException()
    }

    private fun fingerprint(certificate: HeldCertificate) = Base64.getUrlEncoder().withoutPadding().encodeToString(
        MessageDigest.getInstance("SHA-256").digest(certificate.certificate.publicKey.encoded),
    )
}
