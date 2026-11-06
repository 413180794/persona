package persona.feature.inbox

import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.designsystem.Haptic

class InboxHapticsTest {
    // As iOS InboxStore: "Done" buzzes success, "Move to Inbox" a light tap (the list, its bulk bar and the Lark chat's button).
    @Test fun filingBuzzesSuccessAndPuttingBackTaps() {
        assertEquals(Haptic.Success, doneHaptic(true))
        assertEquals(Haptic.Tap, doneHaptic(false))
    }
}
