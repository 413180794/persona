package persona.feature.inbox

import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.designsystem.Haptic

class InboxHapticsTest {
    // As iOS InboxStore: 已处理 buzzes success, 移回 a light tap (the list, its bulk bar and the Lark chat's button).
    @Test fun filingBuzzesSuccessAndPuttingBackTaps() {
        assertEquals(Haptic.Success, doneHaptic(true))
        assertEquals(Haptic.Tap, doneHaptic(false))
    }
}
