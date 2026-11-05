package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.ExtensionQuickPrompt

class ExtensionPromptsTest {
    private val prompt = ExtensionQuickPrompt("example", "示例", "today", "今天做了什么", "请总结今天的工作")

    @Test fun matchesSlashTitleOrIdAndNeverTreatsNormalWordsAsCommands() {
        assertEquals(listOf(prompt), ExtensionPromptText.matching(listOf(prompt), "/"))
        assertEquals(listOf(prompt), ExtensionPromptText.matching(listOf(prompt), "/今天"))
        assertEquals(listOf(prompt), ExtensionPromptText.matching(listOf(prompt), "/TOD"))
        assertTrue(ExtensionPromptText.matching(listOf(prompt), "今天").isEmpty())
        assertTrue(ExtensionPromptText.matching(listOf(prompt), "/今天 明天").isEmpty())
    }

    @Test fun selectionOnlyFillsTheDraftAndKeepsAttachments() {
        val attachment = ComposerAttachment("image", "note.png", "content://example/image", true)
        val state = ChatComposerState(ChatComposerDraft("/today", listOf(attachment)))
        state.fillQuickPrompt(prompt)
        assertEquals(prompt.text, state.value.text)
        assertEquals(prompt.text.length, state.value.selection.start)
        assertEquals(listOf(attachment), state.attachments)
        assertEquals(1, state.focusRequest)
    }
}
