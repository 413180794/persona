package persona.feature.settings

import persona.core.contracts.api.PairingFailure
import org.junit.Assert.*
import org.junit.Test
import java.net.InetAddress

class OnboardingTest {
    @Test fun localPermissionRequiresBothSdkGatesAndRecognizesOnLinkPublicIpv6() {
        assertFalse(PairingLocalNetwork.enforced(36, 37))
        assertFalse(PairingLocalNetwork.enforced(37, 36))
        assertTrue(PairingLocalNetwork.enforced(37, 37))
        val phone = InetAddress.getByName("2001:db8:1::1")
        val prefixes = listOf(phone to 64)
        assertTrue(PairingLocalNetwork.isLocal(InetAddress.getByName("2001:db8:1::2"), prefixes))
        assertFalse(PairingLocalNetwork.isLocal(InetAddress.getByName("2001:db8:2::2"), prefixes))
        assertFalse(PairingLocalNetwork.isLocal(InetAddress.getByName("192.0.2.1"), prefixes))
        assertTrue(PairingLocalNetwork.isLocal(InetAddress.getByAddress(byteArrayOf(10, 0, 0, 1)), emptyList()))
        assertFalse(PairingLocalNetwork.isLocal(InetAddress.getLoopbackAddress(), emptyList()))
    }

    @Test fun failuresAlwaysOfferRecoveryAndIdentityFailuresNeverRetryCredentials() {
        PairingFailure.entries.forEach { assertTrue(it.recovery().label != 0) }
        listOf(PairingFailure.InvalidCode, PairingFailure.Certificate, PairingFailure.Expired,
            PairingFailure.TlsRequired).forEach { assertEquals(PairingRecovery.Scan, it.recovery()) }
        listOf(PairingFailure.Dns, PairingFailure.Refused, PairingFailure.Unreachable,
            PairingFailure.Timeout, PairingFailure.Unknown).forEach { assertEquals(PairingRecovery.Retry, it.recovery()) }
        assertEquals(PairingRecovery.Permission, PairingFailure.LocalPermission.recovery())
        assertEquals(PairingRecovery.Update, PairingFailure.Incompatible.recovery())
    }
}
