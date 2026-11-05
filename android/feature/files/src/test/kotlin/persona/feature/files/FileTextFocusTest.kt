package persona.feature.files

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class FileTextFocusTest {
    @Test fun theLineAskedForIsItsRowAsFarAsTheTextGoes() {
        assertEquals(11, focusRow(12, 40))
        assertEquals(0, focusRow(1, 40))
        // A file cut at 2 MB ends before the line: its last row.
        assertEquals(39, focusRow(500, 40))
        assertNull(focusRow(null, 40))
        assertNull(focusRow(0, 40))
        assertNull(focusRow(3, 0))
    }

    @Test fun theRowsComeAfterThePagesHeader() {
        // The size line (and a truncation notice) are items before the first row.
        assertEquals(12, focusItem(11, 1))
        assertEquals(13, focusItem(11, 2))
        assertEquals(11, focusItem(11, 0))
    }
}
