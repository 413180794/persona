package persona.feature.chat

import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.DraftUploadState
import java.io.IOException

class ComposerPhotoEditTest {
    private fun photo(id: String, path: String? = "/tmp/$id.jpg") = ComposerAttachment(id, "$id.jpg",
        "content://example.com/$id", true, path, size = 10, uploadState = if (path != null) DraftUploadState.Uploaded else DraftUploadState.Pending)
    private val file = ComposerAttachment("notes", "notes.txt", "content://example.com/notes", false, "/tmp/notes.txt",
        uploadState = DraftUploadState.Uploaded)
    private val edited = ComposerAttachment("edited", "edited-1.jpg", "content://example.com/edited", true, size = 20)

    @Test fun anEditReplacesTheOriginalInPlaceAndTheMessageCarriesOnlyTheNewPath() = runBlocking {
        val state = ChatComposerState(ChatComposerDraft("看这里", listOf(photo("a"), file, photo("b"), photo("c"))))
        val uploaded = mutableListOf<ComposerAttachment>()
        val outcome = state.replaceWithEdited(state.attachments[2], edited) { uploaded += it; "/tmp/edited-b.jpg" }
        assertEquals(PhotoEditOutcome.Replaced, outcome)
        assertEquals(listOf(edited), uploaded)
        assertEquals(listOf("a", "notes", "edited", "c"), state.attachments.map { it.id })
        val now = state.attachments[2]
        assertEquals("/tmp/edited-b.jpg", now.path)
        assertEquals(DraftUploadState.Uploaded, now.uploadState)
        assertEquals("content://example.com/edited", now.uri)
        assertTrue(state.ready)
        val message = state.message()
        assertTrue(message.contains("/tmp/a.jpg\n/tmp/edited-b.jpg\n/tmp/c.jpg"))
        assertFalse(message.contains("/tmp/b.jpg"))
    }

    @Test fun aFailedEditUploadKeepsTheOriginal() = runBlocking {
        val before = listOf(photo("a"), photo("b"))
        val state = ChatComposerState(ChatComposerDraft("", before))
        val outcome = state.replaceWithEdited(before[0], edited) { throw IOException("offline") }
        assertTrue(outcome is PhotoEditOutcome.Failed)
        assertEquals(before, state.attachments)
    }

    @Test fun anOriginalRemovedDuringTheUploadIsNotBroughtBack() = runBlocking {
        val state = ChatComposerState(ChatComposerDraft("", listOf(photo("a"), photo("b"))))
        val original = state.attachments[0]
        val outcome = state.replaceWithEdited(original, edited) {
            state.attachments = state.attachments.filterNot { it.id == original.id }
            "/tmp/edited-a.jpg"
        }
        assertEquals(PhotoEditOutcome.Gone, outcome)
        assertEquals(listOf("b"), state.attachments.map { it.id })
    }

    @Test fun onlyPhotosOnTheComputerWithALocalCopyOpenTheEditor() {
        assertTrue(photo("a").editable)
        assertFalse(photo("a", path = null).editable) // still uploading
        assertFalse(photo("a", path = null).copy(error = "上传失败", uploadState = DraftUploadState.Failed).editable)
        assertFalse(photo("a").copy(uploadState = DraftUploadState.Invalid).editable)
        assertFalse(photo("a").copy(uri = "").editable) // restored from a sent message
        assertFalse(file.editable)
    }

    @Test fun replaceEditedLeavesAListWithoutTheOriginalAlone() {
        val list = listOf(photo("a"), file)
        assertEquals(list, replaceEdited(list, "gone", edited))
        assertEquals(listOf(edited, file), replaceEdited(list, "a", edited))
    }
}
