package persona.feature.settings

import java.io.File
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class ProtocolCompatibilityStorageTest {
    @get:Rule val temp = TemporaryFolder()

    @Test fun oldAndNewUpdateApksRemainNonClearableAppData() {
        val cache = temp.newFolder("cache")
        val old = File(cache, "jarvis-update-123.apk").apply { writeText("old update") }
        val current = File(cache, "persona-update-123.apk").apply { writeText("new update") }
        val other = File(cache, "download.apk").apply { writeText("discardable") }
        val usage = StorageUsage(cache, emptyList())
        assertEquals(StorageKind.AppData, usage.kindOf(old))
        assertEquals(StorageKind.AppData, usage.kindOf(current))
        assertEquals(StorageKind.Other, usage.kindOf(other))
        usage.clear(setOf(StorageKind.AppData, StorageKind.Other))
        assertTrue(old.exists())
        assertTrue(current.exists())
        assertFalse(other.exists())
    }
}
