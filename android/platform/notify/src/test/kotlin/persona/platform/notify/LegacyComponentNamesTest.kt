package persona.platform.notify

import org.junit.Assert.assertTrue
import org.junit.Test

class LegacyComponentNamesTest {
    @Test fun systemComponentsAndQueuedWatchdogKeepLoadableLegacyNames() {
        val implementations = mapOf(
            "jarvis.platform.notify.HubService" to HubService::class.java,
            "jarvis.platform.notify.ActionReceiver" to ActionReceiver::class.java,
            "jarvis.platform.notify.BootReceiver" to BootReceiver::class.java,
            "jarvis.platform.notify.ConnectionWatchdog" to ConnectionWatchdog::class.java,
        )
        implementations.forEach { (legacy, implementation) ->
            assertTrue(implementation.isAssignableFrom(Class.forName(legacy)))
        }
    }
}
