package persona.android.platform

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.PairingException
import persona.core.contracts.api.PairingFailure
import persona.core.contracts.api.PairingRequest

class PairingLinksTest {
    @Test fun embeddedLinksUseTheSameParsedRequestBeforePermissionAndTlsChecks() {
        val suffix = "pair?url=https%3A%2F%2Fhub.example.com%3A8443&token=fixture&hub=example-hub&fp=${"A".repeat(43)}&tls=443" +
            "&alt=https%3A%2F%2F%5B2001%3Adb8%3A%3A1%5D%3A8443&alt=https%3A%2F%2Fbackup.example.com"
        val current = PairingRequest("  persona://$suffix  ", "outer-token").parsedPairingLink()!!
        assertEquals("https://hub.example.com:8443", current.url)
        assertEquals("fixture", current.token)
        assertEquals("example-hub", current.hub)
        assertEquals("A".repeat(43), current.fp)
        assertEquals(443, current.tls)
        assertEquals(listOf("https://[2001:db8::1]:8443", "https://backup.example.com"), current.alt)
    }

    @Test fun normalAddressRequestsKeepTheCallersExistingFallback() {
        val request = PairingRequest(" https://hub.example.com ", " outer-token ", alt = listOf("https://backup.example.com"))
        assertNull(request.parsedPairingLink())
        assertSame(request, request.parsedPairingLink() ?: request)
        val wired = request.parsedPairingLink() ?: request.copy(url = request.url.trim(), token = request.token.trim())
        assertEquals("https://hub.example.com", wired.url)
        assertEquals("outer-token", wired.token)
        assertEquals(request.alt, wired.alt)
    }

    @Test fun personaSchemeRetainsCredentialAndTlsValidation() {
        for (scheme in listOf("persona")) {
            for (suffix in listOf("other?url=https://hub.example.com&token=fixture",
                "pair?url=https://hub.example.com&token=", "pair?url=https://hub.example.com&token=fixture&tls=no",
                "pair?url=https://hub.example.com&token=fixture&token=duplicate")) {
                val error = assertThrows(PairingException::class.java) {
                    PairingRequest("$scheme://$suffix", "outer-token").parsedPairingLink()
                }
                assertEquals(PairingFailure.InvalidCode, error.reason)
            }
            val error = assertThrows(PairingException::class.java) {
                PairingRequest("$scheme://pair?url=http://hub.example.com&token=fixture", "outer-token").parsedPairingLink()
            }
            assertEquals(PairingFailure.TlsRequired, error.reason)
            assertEquals("http://localhost:18660", PairingRequest(
                "$scheme://pair?url=http://localhost:18660&token=fixture", "outer-token").parsedPairingLink()!!.url)
        }
    }
}
