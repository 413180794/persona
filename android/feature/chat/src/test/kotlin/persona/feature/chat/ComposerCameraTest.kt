package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test

class ComposerCameraTest {
    private val app = "com.example.persona"
    private val shot = ComposerAttachment("shot", "camera-1.jpg", "content://$app.chat.files/chat-captures/camera-1.jpg", true)
    private val kept = ComposerAttachment("kept", "camera-2.jpg", "content://$app.chat.files/chat-captures/camera-2.jpg", true)
    private val picked = ComposerAttachment("picked", "photo.jpg", "content://media/external/images/media/12", true)
    private val restored = ComposerAttachment("restored", "photo.jpg", "", true, "/tmp/photo.jpg")

    @Test fun capturesAreOnlyThisAppsCameraFiles() {
        assertTrue(isCaptureUri(shot.uri, app))
        assertFalse(isCaptureUri(picked.uri, app))
        assertFalse(isCaptureUri(restored.uri, app))
        assertFalse(isCaptureUri("content://other.app.chat.files/chat-captures/camera-1.jpg", app))
        assertFalse(isCaptureUri("content://$app.chat.files/shared/camera-1.jpg", app))
    }

    @Test fun aCaptureGoesWhenItLeavesTheDraftAndNotBefore() {
        // Removed from the draft.
        assertEquals(listOf(shot.uri), leftCaptures(listOf(shot, kept, picked, restored), listOf(kept, picked, restored), app))
        // Sent and cleared: every capture of the draft; picked photos are not ours to delete.
        assertEquals(listOf(shot.uri, kept.uri), leftCaptures(listOf(shot, kept, picked), emptyList(), app))
        // Updated in place (its upload finished): still in the draft.
        assertEquals(emptyList<String>(), leftCaptures(listOf(shot), listOf(shot.copy(path = "/tmp/camera-1.jpg")), app))
    }
}
