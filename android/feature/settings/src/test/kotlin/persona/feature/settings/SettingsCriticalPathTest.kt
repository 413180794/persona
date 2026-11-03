package persona.feature.settings

import persona.core.contracts.api.PairingException
import org.junit.Assert.*
import org.junit.Test

class SettingsCriticalPathTest {
    @Test fun pairingMetadataAndInvalidInput() {
        val request = PairingInput.parse("jarvis://pair?url=https%3A%2F%2Fexample.com&token=example-token&hub=example-hub&fp=example-fingerprint&tls=443&alt=https%3A%2F%2Fexample.com%3A8443&alt=https%3A%2F%2F%5B2001%3Adb8%3A%3A1%5D")
        assertEquals("https://example.com", request.url)
        assertEquals("example-token", request.token)
        assertEquals("example-hub", request.hub)
        assertEquals("example-fingerprint", request.fp)
        assertEquals(443, request.tls)
        assertEquals(2, request.alt.size)
        assertFalse(request.toString().contains(request.token))
        listOf(
            "jarvis://pair?url=http-broken&token=example",
            "jarvis://pair?url=https%3A%2F%2Fexample.com&token=example&tls=no",
            "jarvis://pair?url=https%3A%2F%2Fexample.com&token=example&token=duplicate",
            "jarvis://other?url=https%3A%2F%2Fexample.com&token=example",
        ).forEach { link -> assertThrows(PairingException::class.java) { PairingInput.parse(link) } }
    }

    @Test fun personaPairingStillUsesTransportAndCredentialValidation() {
        val link = "persona://pair?url=https%3A%2F%2Fhub.example.com&token=fixture&tls=443&alt=https%3A%2F%2Fhub.example.com%3A8443"
        val parsed = PairingInput.parse(link)
        val legacy = PairingInput.parse(link.replaceFirst("persona://", "jarvis://"))
        assertEquals(legacy.url, parsed.url)
        assertEquals(legacy.token, parsed.token)
        assertEquals(legacy.tls, parsed.tls)
        assertEquals(legacy.alt, parsed.alt)
        assertEquals(parsed.url, PairingInput.manual(link, "").url)
        for (suffix in listOf("pair?url=http-broken&token=fixture", "pair?url=https://hub.example.com&token=fixture&tls=no",
            "pair?url=https://hub.example.com&token=fixture&token=duplicate", "other?url=https://hub.example.com&token=fixture")) {
            assertThrows(PairingException::class.java) { PairingInput.parse("persona://$suffix") }
        }
    }

    @Test fun updateDigestRejectsCorruptionAndMissingChecksum() {
        val expected = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        assertTrue(verifySha256("abc".byteInputStream(), expected))
        assertFalse(verifySha256("abd".byteInputStream(), expected))
        assertFalse(verifySha256("abc".byteInputStream(), ""))
    }
}
