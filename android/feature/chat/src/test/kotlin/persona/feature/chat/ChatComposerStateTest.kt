package persona.feature.chat

import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.HubScope
import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.serialization.encodeToString

class ChatComposerStateTest {
    @Test fun structuredDraftRestoresPendingInvalidAndUploadedFilesAndKeepsOriginalHistory() {
        val version = DraftSessionVersion("original", "pane")
        val draft = ChatComposerDraft("检查附件", listOf(
            ComposerAttachment("image", "image.jpg", "content://example.com/image", true,
                size = 123, uploadState = DraftUploadState.Uploading),
            ComposerAttachment("file", "notes.txt", "content://example.com/file", false,
                "/tmp/notes.txt", size = 456, uploadState = DraftUploadState.Uploaded),
            ComposerAttachment("invalid", "missing.txt", "content://example.com/missing", false,
                error = "附件已失效", size = 789, uploadState = DraftUploadState.Invalid),
        ), version)
        val stored = JsonCodec.encodeToString(draft.storedAttachments())
        val snapshot = DraftSnapshot(HubScope("hub", "pairing"), DraftKey.Chat(SessionKey("machine", "session")),
            draft.text, 7, JsonCodec.decodeFromString(stored), version)
        val state = ChatComposerState(snapshot.composerDraft())
        assertEquals(draft, state.draft)
        assertFalse(state.ready)
        state.currentSessionVersion = version
        assertFalse(state.oldVersion)
        state.currentSessionVersion = version.copy(replaced = true)
        assertTrue(state.oldVersion)
        state.currentSessionVersion = DraftSessionVersion("replacement", "pane")
        assertTrue(state.oldVersion)
        state.insert("补充", false)
        assertEquals(version, state.draft.sessionVersion)
        state.attachments = state.attachments.filter { it.id == "file" }
        assertTrue(state.ready)
        assertTrue(state.message().endsWith("/tmp/notes.txt"))
        state.clearIfUnchanged(state.draft)
        state.insert("新草稿", false)
        assertFalse(state.oldVersion)
    }

    @Test fun insertsAtSelectionAndRetainsDraftUntilAllUploadsSucceed() {
        val state = ChatComposerState(ChatComposerDraft("看这里"))
        state.value = TextFieldValue("看这里", TextRange(1, 3))
        state.insert(quoteComposerPath("/tmp/a'b c.txt"), true)
        assertEquals("看 '/tmp/a'\\''b c.txt'", state.value.text)
        state.insert("末尾", false)
        assertTrue(state.value.text.endsWith(" 末尾"))
        state.attachments = listOf(ComposerAttachment("photo", "照片", "content://example.com/photo", true, error = "上传失败"))
        assertFalse(state.ready)
        assertThrows(IllegalStateException::class.java) { state.message() }
        assertTrue(state.value.text.contains("看"))
        state.attachments = state.attachments.map { it.copy(path = "/tmp/photo.jpg", error = null) }
        assertTrue(state.message().endsWith("<attached_images>\n/tmp/photo.jpg\n</attached_images>"))
        val restored = ChatComposerState().apply { editMessage(state.message()) }
        assertEquals(state.message(), restored.message())
        assertFalse(composerMessagePreview(state.message()).contains("<attached_images>"))
        val sent = state.draft
        state.insert("补充", false)
        state.clearIfUnchanged(sent)
        assertTrue(state.hasContent)
        state.clearIfUnchanged(state.draft)
        assertFalse(state.hasContent)
    }
}
