package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test
import java.util.regex.Pattern

class ReaderSnapshotTest {
    @Test fun freezesAndPaginatesWithoutSplittingGraphemesOrLosingText() {
        val clusters = listOf("😀", "e\u0301", "👩🏽‍💻", "🇨🇳", "\r\n")
        clusters.forEach { cluster ->
            var live = "a".repeat(8191) + cluster + "尾".repeat(8192)
            val snapshot = ReaderSnapshot(live)
            live += "新内容"
            assertFalse(snapshot.text.endsWith("新内容"))
            val boundaries = mutableSetOf(0)
            val matcher = Pattern.compile("\\X").matcher(snapshot.text)
            while (matcher.find()) boundaries += matcher.end()
            assertEquals(8191, snapshot.pages.first().end)
            assertEquals(snapshot.text, snapshot.pages.joinToString("") { snapshot.text.substring(it.start, it.end) })
            snapshot.pages.forEachIndexed { index, range ->
                assertTrue(range.start in boundaries && range.end in boundaries)
                assertTrue(range.end - range.start <= 8192)
                assertEquals(index, snapshot.pageAt(range.start))
                assertEquals(index, snapshot.pageAt(range.end - 1))
            }
            assertEquals(snapshot.pages.lastIndex, snapshot.pageAt(snapshot.text.length))
        }
        val oversized = "e" + "\u0301".repeat(8200)
        assertEquals(listOf(ReaderRange(0, oversized.length), ReaderRange(oversized.length, oversized.length + 1)),
            ReaderSnapshot(oversized + "x").pages)
        assertEquals(listOf(ReaderRange(0, 0)), ReaderSnapshot("").pages)
        assertEquals(1, ReaderSnapshot("x".repeat(8192)).pages.size)
    }

    @Test fun searchesAcrossPagesAndNavigatesContiguousDiffChanges() {
        val snapshot = ReaderSnapshot("abcNEEDLE xyz needle", pageSize = 6)
        assertEquals(listOf(ReaderRange(3, 9), ReaderRange(14, 20)), snapshot.search("needle"))
        assertTrue(snapshot.search("needle", page = 0).isEmpty())
        assertEquals(listOf(ReaderRange(0, 3)), snapshot.search("ABC", page = 0))
        assertTrue(snapshot.search("").isEmpty())
        assertTrue(snapshot.search("missing").isEmpty())
        val diff = ToolDiff(listOf(' ', '-', '+', '+', ' ', '-', '+', ' ').map {
            DiffLine("content", it, null, null)
        })
        assertEquals(listOf(1, 5), diffChangeStarts(diff))
        assertTrue(diffChangeStarts(ToolDiff(emptyList())).isEmpty())
        assertTrue(diffChangeStarts(ToolDiff(listOf(DiffLine("same", ' ', null, null)))).isEmpty())
    }
}
