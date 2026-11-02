package jarvis.android

import androidx.compose.ui.unit.dp
import jarvis.android.ui.shell.Hinge
import jarvis.android.ui.shell.isWide
import jarvis.android.ui.shell.maxLeft
import jarvis.android.ui.shell.paneSplit
import jarvis.android.ui.shell.settleDivider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Where a wide window's two panes meet and where a dragged divider settles (docs/android.md「折叠屏和大屏」). */
class FoldTest {
    private fun flat(at: Float) = Hinge(at.dp, at.dp)

    @Test fun phonesAndCoverScreensStayOnePage() {
        assertFalse(isWide(373.dp)) // a foldable's cover screen at 480 dpi
        assertFalse(isWide(411.dp))
        assertTrue(isWide(600.dp))
        assertTrue(isWide(771.dp)) // its inner screen
    }

    @Test fun theChatGetsMostOfAnInnerScreen() {
        // A book-style foldable's inner screen, 770.67 dp, the fold flat down the middle: not halves any more.
        val s = paneSplit(770.67.dp, listOf(flat(385.33f)), null)
        assertEquals(320f, s.left.value, 0.01f)
        assertFalse(s.fixed)
        assertEquals(listOf(385.33.dp), s.snaps)
    }

    @Test fun aFlatFoldIsWhereALetGoDividerSnaps() {
        val snaps = listOf(385.dp)
        assertEquals(385f, settleDivider(360.dp, 770.dp, snaps)!!.value, 0.01f)
        assertEquals(350f, settleDivider(350.dp, 770.dp, snaps)!!.value, 0.01f) // too far from it
    }

    @Test fun aSeparatingFoldFixesTheSplit() {
        // Half open like a book, or two screens with a gap: the right pane starts after the gap.
        val s = paneSplit(800.dp, listOf(Hinge(390.dp, 410.dp, separating = true)), 0.3f)
        assertEquals(390f, s.left.value, 0.01f)
        assertEquals(20f, s.gap.value, 0.01f)
        assertTrue(s.fixed)
    }

    @Test fun aFoldTooNearAnEdgeIsIgnored() {
        // A fold 200 dp from the left (a window over part of the screen): the panes would be too narrow there.
        val s = paneSplit(1000.dp, listOf(Hinge(200.dp, 200.dp, separating = true)), null)
        assertEquals(380f, s.left.value, 0.01f)
        assertFalse(s.fixed)
        assertTrue(s.snaps.isEmpty())
    }

    @Test fun aTriFoldTakesTheFoldNearest() {
        // Opened all the way, folds at a third and two thirds: the default lands on the first one.
        val s = paneSplit(1224.dp, listOf(flat(408f), flat(816f)), null)
        assertEquals(408f, s.left.value, 0.01f)
        assertEquals(listOf(408.dp, 816.dp), s.snaps)
        // Both half open: fixed at the one nearest the usual split.
        val bent = paneSplit(1224.dp, listOf(Hinge(408.dp, 408.dp, true), Hinge(816.dp, 816.dp, true)), null)
        assertEquals(408f, bent.left.value, 0.01f)
        assertTrue(bent.fixed)
    }

    @Test fun theDefaultKeepsBothPanesUsable() {
        assertEquals(420f, paneSplit(1280.dp, emptyList(), null).left.value, 0.01f) // a tablet: capped
        assertEquals(300f, paneSplit(700.dp, emptyList(), null).left.value, 0.01f) // a narrower inner screen: the chat keeps 400
        assertEquals(280f, paneSplit(600.dp, emptyList(), null).left.value, 0.01f) // the narrowest wide window
        assertEquals(280f, maxLeft(600.dp).value, 0.01f) // the right pane keeps 320
    }

    @Test fun theUsersShareIsKeptWithinTheLeastWidths() {
        assertEquals(640f, paneSplit(1280.dp, emptyList(), 0.5f).left.value, 0.01f) // wider than the default cap
        assertEquals(480f, paneSplit(800.dp, emptyList(), 0.9f).left.value, 0.01f)
        assertEquals(280f, paneSplit(800.dp, emptyList(), 0.1f).left.value, 0.01f)
    }

    @Test fun draggedFarLeftTheListCloses() {
        assertNull(settleDivider(200.dp, 800.dp, emptyList()))
        assertEquals(280f, settleDivider(230.dp, 800.dp, emptyList())!!.value, 0.01f) // not far enough: back to its least width
        assertEquals(480f, settleDivider(700.dp, 800.dp, emptyList())!!.value, 0.01f)
    }
}
