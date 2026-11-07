package persona.feature.settings

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.ExtensionStreamBinding
import persona.core.contracts.api.HubConnection
import java.lang.reflect.Proxy

class NativePageManagerTest {
    private val connection = Proxy.newProxyInstance(HubConnection::class.java.classLoader, arrayOf(HubConnection::class.java)) { _, _, _ ->
        error("Binding selection must not invoke the connection")
    } as HubConnection

    @Test fun aPinnedPageRoutesToItsManagerWithoutReplacingCredentialOwnership() {
        val owner = ExtensionStreamBinding(connection, "synthetic-owner", "synthetic-group", "default-manager")
        val pinned = nativePageBinding(owner, "selected-manager")!!
        assertEquals("selected-manager", pinned.manager)
        assertSame(connection, pinned.connection)
        assertEquals(owner.owner, pinned.owner)
        assertEquals(owner.group, pinned.group)
        assertSame(owner.approvals, pinned.approvals)
        assertEquals("default-manager", owner.manager)
    }

    @Test fun absentAndBlankSelectorsRetainTheActualDefaultBinding() {
        val owner = ExtensionStreamBinding(connection, "synthetic-owner", "synthetic-group", "default-manager")
        assertSame(owner, nativePageBinding(owner, null))
        assertSame(owner, nativePageBinding(owner, ""))
        assertSame(owner, nativePageBinding(owner, " "))
        assertNull(nativePageBinding(null, "selected-manager"))
    }
}
