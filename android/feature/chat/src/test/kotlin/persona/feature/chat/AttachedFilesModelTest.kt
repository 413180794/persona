package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test

class AttachedFilesModelTest {
    @Test fun aSentVideoIsAFileNotText() {
        val model = messageModel("看这个\n\n<persona-file kind=\"video\" path=\"/up/a.mov\" name=\"a.mov\" size=\"10\"/>", user = true)
        assertEquals(listOf("/up/a.mov"), model.attached.map { it.path })
        assertEquals("看这个", model.plain)
        // Its path is not also a chip.
        assertTrue(model.files.isEmpty())
    }

    @Test fun editingPutsTheFilesBack() {
        val raw = "看这个\n\n<persona-file kind=\"video\" path=\"/up/a &amp; b.mov\" name=\"a.mov\" size=\"10\" duration=\"12.6\"/>"
        val edit = messageModel(raw, user = true).editText()
        assertEquals("看这个\n\n<persona-file kind=\"video\" path=\"/up/a &amp; b.mov\" name=\"a.mov\" size=\"10\" duration=\"12.6\"/>", edit)
        // Sent again, it is the same message.
        assertEquals(messageModel(raw, user = true).attached, messageModel(edit, user = true).attached)
        assertEquals("纯文字", messageModel("纯文字", user = true).editText())
    }

    @Test fun documentsKnowWhatTheyAre() {
        assertEquals(R.string.chat_doc_word, documentKind("/a/b.DOCX"))
        assertEquals(R.string.chat_doc_keynote, documentKind("/a/b.key"))
        assertEquals(R.string.chat_doc_other, documentKind("/a/b"))
    }
}
