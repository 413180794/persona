package persona.core.designsystem

import androidx.compose.ui.geometry.Offset
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.api.PersonaManifest

class PersonaDesignTest {
    @Test fun onlyTheDefaultIsDrawnInTheApp() {
        assertEquals(listOf("persona"), PersonaDesign.entries.map { it.id })
        assertEquals(PersonaDesign.Mochi, PersonaDesign.Default)
        assertEquals(R.string.designsystem_face_mochi, PersonaDesign.Default.titleRes)
    }

    @Test fun removedAndUnknownDrawingsFallBackToTheDefault() {
        (listOf(null, "", "future-look", "PERSONA") + PersonaManifest.RETIRED_IDS).forEach {
            assertEquals(PersonaDesign.Mochi, PersonaDesign.fromId(it))
        }
    }

    @Test fun retiredSavedChoicesRestoreAsPersona() {
        (listOf(null, "", " ") + PersonaManifest.RETIRED_IDS).forEach {
            assertEquals("persona", restoredPersonaId(it))
        }
    }

    @Test fun packageChoicesKeepTheirIntentWhileTheirFallbackIsPersona() {
        listOf("persona", "nwverse", "custom-look", "temporarily-unavailable").forEach {
            assertEquals(it, restoredPersonaId(it))
            assertEquals(PersonaDesign.Mochi, PersonaDesign.fromId(it))
        }
    }

    @Test fun mochiIsTracedInsideTheDisc() {
        (Mochi.eyes + Mochi.lines.flatMap { listOf(it.first, it.second) } + Mochi.p(168f, 672f) + Mochi.p(262f, 778f)).forEach {
            val d = (it - Offset(0.5f, 0.5f)).getDistance()
            assert(d < 0.5f) { "$it is outside the disc" }
        }
    }
}
