package persona.core.hub

import persona.core.contracts.AppLanguage
import persona.core.contracts.ClientLanguage
import persona.core.contracts.Endpoint
import java.net.InetAddress
import java.security.MessageDigest
import java.util.Base64
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.Assert.assertEquals
import org.junit.Test

class HubClientLanguageTest {
    /** Every request says which language the app shows, so the hub can answer in it (docs/protocol/i18n.md). */
    @Test fun requestsCarryTheShownLanguage() = runBlocking {
        val certificate = HeldCertificate.Builder().commonName("example.com").build()
        val tls = HandshakeCertificates.Builder().heldCertificate(certificate).build()
        MockWebServer().use { server ->
            server.useHttps(tls.sslSocketFactory(), false)
            server.start(InetAddress.getLoopbackAddress(), 0)
            server.enqueue(MockResponse().setBody("""{"ok":true,"hub_id":"test-hub"}"""))
            server.enqueue(MockResponse().setBody("""{"ok":true,"hub_id":"test-hub"}"""))
            val pin = Base64.getUrlEncoder().withoutPadding()
                .encodeToString(MessageDigest.getInstance("SHA-256").digest(certificate.certificate.publicKey.encoded))
            val endpoint = Endpoint(server.url("/").newBuilder().host(InetAddress.getLoopbackAddress().hostAddress!!).build().toString(), "lan")
            HubHttpClient("test-hub", pin, "test").use { client ->
                try {
                    ClientLanguage.current = AppLanguage.Ja
                    client.health(endpoint)
                    assertEquals("ja", (server.takeRequest().getHeader("Accept-Language")).zhAny())
                    ClientLanguage.current = AppLanguage.En
                    client.health(endpoint)
                    assertEquals("en", (server.takeRequest().getHeader("Accept-Language")).zhAny())
                } finally { ClientLanguage.current = AppLanguage.En }
            }
        }
    }
}
