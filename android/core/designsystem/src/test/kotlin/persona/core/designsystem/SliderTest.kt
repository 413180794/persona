package persona.core.designsystem

import org.junit.Assert.assertEquals
import org.junit.Test

class SliderTest {
    @Test fun stepsCountTheStopsBetweenTheEnds() {
        assertEquals(19, sliderSteps(0f..100f, 5f))
        assertEquals(0, sliderSteps(0f..100f, null))
        assertEquals(0, sliderSteps(0f..1f, 0f))
        assertEquals(0, sliderSteps(0f..5f, 5f))
    }

    @Test fun valuesSnapToTheNearestStopInRange() {
        assertEquals(55f, snapToStep(53.4f, 0f..100f, 5f))
        assertEquals(50f, snapToStep(52.4f, 0f..100f, 5f))
        assertEquals(100f, snapToStep(140f, 0f..100f, 5f))
        assertEquals(0f, snapToStep(-3f, 0f..100f, 5f))
        assertEquals(0.37f, snapToStep(0.37f, 0f..1f, null))
    }
}
