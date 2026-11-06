package persona.platform.notify

import org.junit.Assert.assertEquals
import org.junit.Test

class ComponentNamesTest {
    @Test fun systemComponentsAndQueuedWatchdogUseCanonicalNames() {
        val components = mapOf(
            "persona.platform.notify.HubService" to HubService::class.java,
            "persona.platform.notify.ActionReceiver" to ActionReceiver::class.java,
            "persona.platform.notify.BootReceiver" to BootReceiver::class.java,
            "persona.platform.notify.ConnectionWatchdog" to ConnectionWatchdog::class.java,
        )
        components.forEach { (name, implementation) ->
            assertEquals(name, implementation.name)
            assertEquals(implementation, Class.forName(name))
        }
    }
}
