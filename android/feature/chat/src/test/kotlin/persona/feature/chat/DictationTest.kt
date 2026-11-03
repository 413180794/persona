package persona.feature.chat

import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import org.junit.Assert.assertEquals
import org.junit.Test

class DictationTest {
    @Test fun heardTextGoesAtTheEnd() {
        assertEquals("帮我看下日志", withDictation("", "帮我看下日志"))
        assertEquals("先跑测试然后提交", withDictation("先跑测试", "然后提交"))
        assertEquals("run the tests and commit", withDictation("run the tests", "and commit"))
        assertEquals("改 README然后提交", withDictation("改 README ", "然后提交"))
    }

    @Test fun nothingHeardLeavesTheDraftAlone() {
        assertEquals("先跑测试", withDictation("先跑测试", ""))
        assertEquals("看下日志", withDictation("  ", "看下日志"))
    }

    @Test fun theDraftGetsItAtTheEndWithTheCursorAfterIt() {
        // Wherever the cursor was: dictation appends, it never inserts at the selection or pads with spaces.
        val state = ChatComposerState(ChatComposerDraft("先跑测试"))
        state.value = TextFieldValue("先跑测试", TextRange(1, 2))
        state.appendDictation("然后提交")
        assertEquals("先跑测试然后提交", state.value.text)
        assertEquals(TextRange(8), state.value.selection)
    }
}
