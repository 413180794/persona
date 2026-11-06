package persona.core.designsystem

import androidx.compose.ui.geometry.Offset
import org.junit.Assert.assertEquals
import org.junit.Test

class PersonaDesignTest {
    @Test fun storedIdsMatchIosAndUnknownFallsBackToTheDefault() {
        assertEquals(listOf("persona", "persona-visor", "persona-circuit"), PersonaDesign.entries.map { it.id })
        PersonaDesign.entries.forEach { assertEquals(it, PersonaDesign.fromId(it.id)) }
        listOf(null, "", "doudou", "PERSONA").forEach { assertEquals(PersonaDesign.Mochi, PersonaDesign.fromId(it)) }
    }

    /** Mochi is the default; the distinct Visor id restores Visor. */
    @Test fun mochiIsTheDefaultAndASavedVisorStays() {
        assertEquals(PersonaDesign.Mochi, PersonaDesign.Default)
        assertEquals(PersonaDesign.Visor, PersonaDesign.fromId("persona-visor"))
        assertEquals(PersonaDesign.Mochi, PersonaDesign.fromId("persona"))
        assertEquals(R.string.designsystem_face_mochi, PersonaDesign.Default.titleRes)
    }

    @Test fun mochiIsTracedInsideTheDisc() {
        (Mochi.eyes + Mochi.lines.flatMap { listOf(it.first, it.second) } + Mochi.p(168f, 672f) + Mochi.p(262f, 778f)).forEach {
            val d = (it - Offset(0.5f, 0.5f)).getDistance()
            assert(d < 0.5f) { "$it is outside the disc" }
        }
    }

    @Test fun circuitLightsRunFromThePadIntoTheCore() {
        val (top, middle, _) = Circuit.traces
        // The middle trace runs straight in; the outer ones bend at 45° on the way.
        assertEquals(2, middle.size)
        assertEquals(3, top.size)
        assertNear(Offset(Circuit.PAD_X, 0.31f), Circuit.along(top, 0f))
        assertNear(Circuit.core, Circuit.along(top, 1f))
        assertNear(Offset((Circuit.PAD_X + Circuit.core.x) / 2, 0.5f), Circuit.along(middle, 0.5f))
    }

    private fun assertNear(expected: Offset, actual: Offset) {
        assertEquals(expected.x, actual.x, 1e-4f)
        assertEquals(expected.y, actual.y, 1e-4f)
    }
}
