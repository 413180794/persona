package jarvis.core.terminal

import org.junit.Assert.assertEquals
import org.junit.Test

class ModuleTest {
    @Test fun packageIsAvailableOnJvm() {
        assertEquals("jarvis.core.terminal", javaClass.packageName)
    }
}
