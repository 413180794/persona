package persona.feature.chat

import org.junit.Assert.assertEquals
import org.junit.Test

/** Options read with the number or key the terminal shows (old App's InteractionCard, iOS QuestionForm). */
class InteractionOptionsTest {
    @Test fun shortKeysNumberTheOption() {
        assertEquals("1. Swift Testing", numberedOption("1", "Swift Testing"))
        assertEquals("y. 继续", numberedOption("y", "继续"))
        // Long keys (key names) and options that already carry their number stay as they are.
        assertEquals("继续", numberedOption("enter", "继续"))
        assertEquals("2. 拒绝", numberedOption("2", "2. 拒绝"))
        assertEquals("继续", numberedOption("", "继续"))
    }
}
