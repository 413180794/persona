package jarvis.core.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PairingTest {
    @Test fun legacyLinkKeepsItsUrlAndToken() {
        assertEquals(Pairing("http://192.0.2.1:18660/", "mock"), Pairing.parse(
            "  jarvis://pair?url=http%3A%2F%2F192.0.2.1%3A18660%2F&token=mock  ",
        ))
    }

    @Test fun queryValuesAreDecodedOnceAndTrimmed() {
        assertEquals(Pairing("https://hub.example.com/a%20b", "a+b c"), Pairing.parse(
            "jarvis://pair?url=+https%3A%2F%2Fhub.example.com%2Fa%2520b+&token=+a%2Bb+c+",
        ))
    }

    @Test fun metadataAndRepeatedAlternativesAreRetainedInOrder() {
        val pairing = Pairing.parse("jarvis://pair?url=http%3A%2F%2F192.0.2.1&token=mock" +
            "&hub=example-hub&fp=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&tls=18661" +
            "&alt=https%3A%2F%2F%5B2001%3Adb8%3A%3A1%5D%3A18661" +
            "&alt=https%3A%2F%2Fhub.example.com%3A18661")!!
        assertEquals("example-hub", pairing.hub)
        assertEquals("A".repeat(43), pairing.fp)
        assertEquals(18661, pairing.tls)
        assertEquals(listOf("https://[2001:db8::1]:18661", "https://hub.example.com:18661"), pairing.alt)
        assertEquals("http://192.0.2.1", pairing.url)
        assertEquals("mock", pairing.token)
    }

    @Test fun unknownOrMalformedOptionalFieldsDoNotBreakLegacyPairing() {
        val p = Pairing.parse("jarvis://pair?url=http://192.0.2.1&token=mock&tls=unknown&future=1")!!
        assertEquals(Pairing("http://192.0.2.1", "mock"), p)
    }

    @Test fun duplicateLegacyFieldsUseTheFirstValue() {
        assertEquals(Pairing("http://192.0.2.1", "first"), Pairing.parse(
            "jarvis://pair?url=http://192.0.2.1&url=https://hub.example.com&token=first&token=second",
        ))
    }

    @Test fun legacyPathAndFragmentDoNotChangeParameters() {
        assertEquals(Pairing("http://192.0.2.1", "mock"), Pairing.parse(
            "jarvis://pair/qr?url=http://192.0.2.1&token=mock#ignored",
        ))
        assertNull(Pairing.parse("jarvis://pair#ignored?url=http://192.0.2.1&token=mock"))
    }

    @Test fun legacyValidationIsPreserved() {
        for (link in listOf(
            "https://pair?url=http://192.0.2.1&token=mock",
            "jarvis://other?url=http://192.0.2.1&token=mock",
            "JARVIS://pair?url=http://192.0.2.1&token=mock",
            "jarvis://pair?token=mock",
            "jarvis://pair?url=http://192.0.2.1",
            "jarvis://pair?url=ftp://192.0.2.1&token=mock",
            "jarvis://pair?url=http://192.0.2.1&token=+",
        )) assertNull(link, Pairing.parse(link))
    }
}
