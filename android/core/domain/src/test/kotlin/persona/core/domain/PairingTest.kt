package persona.core.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PairingTest {
    @Test fun personaLinksUseCanonicalPairingRules() {
        val link = "persona://pair?url=https%3A%2F%2Fhub.example.com&token=fixture&hub=example-hub&tls=18661" +
            "&alt=https%3A%2F%2F%5B2001%3Adb8%3A%3A1%5D%3A18661&token=ignored"
        assertEquals(Pairing("https://hub.example.com", "fixture", hub = "example-hub", tls = 18661,
            alt = listOf("https://[2001:db8::1]:18661")), Pairing.parse(link))
        for (suffix in listOf("pair?url=https://hub.example.com", "other?url=https://hub.example.com&token=fixture",
            "pair?url=ftp://hub.example.com&token=fixture", "pair?url=https://hub.example.com&token=+")) {
            assertNull(Pairing.parse("persona://$suffix"))
        }
    }
    @Test fun isLinkAcceptsPersonaButNotHubAddresses() {
        for (link in listOf("persona://pair?url=x", " persona://pair?url=x ", "persona://open?target=inbox", "persona://")) {
            assertTrue(link, Pairing.isLink(link))
        }
        // A bare host named after a scheme, with a port, is an address and not a link.
        for (address in listOf("https://hub.example.com", "persona.example.com", "persona.local:18660",
            "persona:18660", "persona:", "")) {
            assertFalse(address, Pairing.isLink(address))
        }
    }
    @Test fun legacyLinkKeepsItsUrlAndToken() {
        assertEquals(Pairing("http://192.0.2.1:18660/", "mock"), Pairing.parse(
            "  persona://pair?url=http%3A%2F%2F192.0.2.1%3A18660%2F&token=mock  ",
        ))
    }

    @Test fun queryValuesAreDecodedOnceAndTrimmed() {
        assertEquals(Pairing("https://hub.example.com/a%20b", "a+b c"), Pairing.parse(
            "persona://pair?url=+https%3A%2F%2Fhub.example.com%2Fa%2520b+&token=+a%2Bb+c+",
        ))
    }

    @Test fun metadataAndRepeatedAlternativesAreRetainedInOrder() {
        val pairing = Pairing.parse("persona://pair?url=http%3A%2F%2F192.0.2.1&token=mock" +
            "&hub=example-hub&fp=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&tls=18661" +
            "&alt=https%3A%2F%2F%5B2001%3Adb8%3A%3A1%5D%3A18661" +
            "&alt=https%3A%2F%2Fhub.example.com%3A18661")!!
        assertEquals("example-hub", (pairing.hub).zhAny())
        assertEquals("A".repeat(43), (pairing.fp).zhAny())
        assertEquals(18661, pairing.tls)
        assertEquals(listOf("https://[2001:db8::1]:18661", "https://hub.example.com:18661"), (pairing.alt).zhAny())
        assertEquals("http://192.0.2.1", (pairing.url).zhAny())
        assertEquals("mock", (pairing.token).zhAny())
    }

    @Test fun unknownOrMalformedOptionalFieldsDoNotBreakLegacyPairing() {
        val p = Pairing.parse("persona://pair?url=http://192.0.2.1&token=mock&tls=unknown&future=1")!!
        assertEquals(Pairing("http://192.0.2.1", "mock"), p)
    }

    @Test fun duplicateLegacyFieldsUseTheFirstValue() {
        assertEquals(Pairing("http://192.0.2.1", "first"), Pairing.parse(
            "persona://pair?url=http://192.0.2.1&url=https://hub.example.com&token=first&token=second",
        ))
    }

    @Test fun legacyPathAndFragmentDoNotChangeParameters() {
        assertEquals(Pairing("http://192.0.2.1", "mock"), Pairing.parse(
            "persona://pair/qr?url=http://192.0.2.1&token=mock#ignored",
        ))
        assertNull(Pairing.parse("persona://pair#ignored?url=http://192.0.2.1&token=mock"))
    }

    @Test fun legacyValidationIsPreserved() {
        for (link in listOf(
            "https://pair?url=http://192.0.2.1&token=mock",
            "persona://other?url=http://192.0.2.1&token=mock",
            "PERSONA://pair?url=http://192.0.2.1&token=mock",
            "persona://pair?token=mock",
            "persona://pair?url=http://192.0.2.1",
            "persona://pair?url=ftp://192.0.2.1&token=mock",
            "persona://pair?url=http://192.0.2.1&token=+",
        )) assertNull(link, Pairing.parse(link))
    }
}
