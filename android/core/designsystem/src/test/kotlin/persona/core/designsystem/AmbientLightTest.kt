package persona.core.designsystem

import androidx.compose.ui.geometry.Size
import org.junit.Assert.*
import org.junit.Test

class AmbientLightTest {
    private val area = Size(1080f, 2400f)
    private val unit = area.minDimension
    private val pools = (0..3).map { Pool(0.1f + 0.25f * it, 0.2f + 0.2f * it, 0.8f, seed = it) }
    private val step = 1.0 / 12

    @Test
    fun reduceMotionKeepsEachPoolWhereItRests() {
        for (pool in pools) for (t in listOf(0.0, 77.7, 1e6)) {
            val blobs = pool.blobs(t, area, still = true)
            assertEquals(1, blobs.size)
            assertEquals(area.width * pool.x, blobs[0].center.x, 0.001f)
            assertEquals(area.height * pool.y, blobs[0].center.y, 0.001f)
            assertEquals(unit * pool.radius, blobs[0].radius, 0.001f)
            assertEquals(1f, blobs[0].weight, 0f)
        }
    }

    @Test
    fun theSameMomentDrawsTheSameLight() {
        // Every pane paints its own copy; they only line up if the light depends on nothing but the time.
        val t = 123_456.789
        for (pool in pools) {
            val a = pool.blobs(t, area, still = false)
            val b = pool.blobs(t, area, still = false)
            assertEquals(a.map { it.center to it.radius }, b.map { it.center to it.radius })
        }
    }

    @Test
    fun eachPoolSplitsAndMergesWithinTenMinutes() {
        for (pool in pools) {
            val counts = (0..600).map { pool.blobs(it.toDouble(), area, still = false).size }.toSet()
            assertEquals("seed ${pool.seed}", setOf(1, 2), counts)
        }
    }

    @Test
    fun lightMovesSlowlyAndStaysSoft() {
        // Two hours at the redraw rate: nothing jumps, nothing races, nothing flares or vanishes abruptly.
        for (pool in pools) {
            var previous = pool.blobs(0.0, area, still = false)
            var t = step
            while (t < 7200) {
                val now = pool.blobs(t, area, still = false)
                for (blob in now) {
                    assertTrue(blob.weight in 0f..1.11f)
                    assertTrue(blob.lean in 0f..1f)
                    assertTrue(blob.radius in unit * pool.radius * 0.3f..unit * pool.radius * 1.15f)
                }
                if (now.size == previous.size) for (i in now.indices) {
                    val moved = (now[i].center - previous[i].center).getDistance() / unit
                    // Under 0.05 of the short side a second (~54 px/s on a phone), and the size and strength in step.
                    assertTrue("seed ${pool.seed} t=$t moved $moved", moved / step < 0.05)
                    assertTrue(kotlin.math.abs(now[i].weight - previous[i].weight) / step < 0.05)
                    assertTrue(kotlin.math.abs(now[i].radius - previous[i].radius) / unit / step < 0.03)
                }
                previous = now
                t += step
            }
        }
    }

    @Test
    fun aBudAppearsAndMeltsBackFaint() {
        // The second light fades in from nothing at the main light's edge, never popping in at full strength.
        for (pool in pools) {
            var t = 0.0
            var last = pool.blobs(0.0, area, still = false).size
            while (t < 900) {
                val now = pool.blobs(t, area, still = false)
                if (now.size != last) assertTrue("seed ${pool.seed} t=$t", (now.getOrNull(1)?.weight ?: 0f) < 0.02f)
                if (now.size == 2) assertTrue(now[1].weight <= 0.6f * 1.1f + 1e-4f)
                last = now.size
                t += step
            }
        }
    }
}
