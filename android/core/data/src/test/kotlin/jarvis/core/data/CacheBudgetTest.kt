package jarvis.core.data

import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class CacheBudgetTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test fun fileCacheEvictsLeastRecentlyReadAndPreservesOpenFilesAcrossOwners() {
        val directory = temporary.newFolder()
        val cache = BudgetFileCache(directory, 8)
        val first = cache.put("first", ByteArray(4))
        val second = cache.put("second", ByteArray(4))
        first.setLastModified(1)
        second.setLastModified(2)
        cache.read("first")
        val third = cache.put("third", ByteArray(4))
        assertTrue(first.exists())
        assertFalse(second.exists())
        CacheFiles.retain(first).use {
            first.setLastModified(1)
            BudgetFileCache(directory, 8).put("fourth", ByteArray(4))
            assertTrue(first.exists())
            assertFalse(third.exists())
            assertTrue(runCatching { cache.put("large", ByteArray(9)) }.isFailure)
            assertTrue(runCatching { cache.put("blocked", ByteArray(8)) }.isFailure)
        }
        cache.put("last", ByteArray(8))
        assertFalse(first.exists())
        assertEquals(8L, directory.listFiles()!!.sumOf { it.length() })
    }

    @Test fun activeItemsSurviveOverBudgetUntilReleaseAndImagesFitWithoutUpscaling() {
        val entries = listOf(BudgetEntry("active", 12, 1), BudgetEntry("old", 4, 2), BudgetEntry("recent", 4, 3))
        assertEquals(listOf("old", "recent"), evictionCandidates(entries, 8) { it == "active" })
        assertEquals(listOf("active"), evictionCandidates(entries, 8) { false })
        assertEquals(300 to 150, displayImageSize(2400, 1200, 300, 200))
        assertEquals(20 to 40, displayImageSize(20, 40, 300, 200))
        assertEquals(1 to 200, displayImageSize(1, 10000, 300, 200))
    }
}
