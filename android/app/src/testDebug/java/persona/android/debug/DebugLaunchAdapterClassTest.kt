package persona.android.debug

import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.android.navigation.LaunchAdapter

class DebugLaunchAdapterClassTest {
    @Test fun manifestAdapterCanBeLoadedAndConstructedReflectively() {
        val type = Class.forName("persona.android.debug.DebugLaunchAdapter")
        assertTrue(LaunchAdapter::class.java.isAssignableFrom(type))
        val adapter = type.getDeclaredConstructor().newInstance() as LaunchAdapter
        assertNotNull(adapter.tools)
    }
}
