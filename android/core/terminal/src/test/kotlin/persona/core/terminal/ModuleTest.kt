package persona.core.terminal

import org.junit.Assert.assertEquals
import org.junit.Test

class ModuleTest {
    @Test fun packageIsAvailableOnJvm() {
        assertEquals("persona.core.terminal", javaClass.packageName)
    }
}
