package persona.feature.files

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.SessionFileContent
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import java.io.File

/** [WholeFiles.reason] and [rememberWholeFile]: what a failure says, the cache it keeps, and fetching again. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class WholeFileTest {
    @get:Rule val rule = createComposeRule()

    /** The fetch runs on Dispatchers.IO: let the page settle a little at a time until [done] (waitUntil alone does not). */
    private fun settle(done: () -> Boolean) {
        repeat(100) { if (!done()) { rule.waitForIdle(); Thread.sleep(50) } }
        rule.waitForIdle()
        assertTrue("not settled", done())
    }

    private val resources get() = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    @Test fun reasonsSayWhyTheFileDidNotCome() {
        val noConverter = HubFailure(HubError.Server, "x", status = 404, code = "no_converter")
        assertEquals(resources.getString(R.string.files_office_no_converter), WholeFiles.reason(resources, noConverter, converting = true))
        val outdated = HubFailure(HubError.Server, "x", status = 409, code = "node_outdated")
        assertEquals(resources.getString(R.string.files_office_node_outdated), WholeFiles.reason(resources, outdated, converting = true))
        assertEquals(resources.getString(R.string.files_audio_node_outdated), WholeFiles.reason(resources, outdated))
        assertEquals(resources.getString(R.string.files_audio_node_outdated), WholeFiles.reason(resources, UnsupportedOperationException("download")))
        // no_converter only means something for a conversion; elsewhere the usual words.
        assertEquals(filesLoadError(resources, noConverter), WholeFiles.reason(resources, noConverter))
    }

    @Test fun aFetchedFileIsKeptTheOldestGoAndAFailureSaysSo() {
        val fetched = mutableListOf<String>()
        var fail: Exception? = null
        var file by mutableStateOf(SessionFileContent("/m/a.mp4", "a.mp4", size = 3, stream = true))
        var load: WholeLoad = WholeLoad.Fetching
        rule.setContent {
            val state by rememberWholeFile(file, "files-test", { into ->
                fail?.let { throw it }
                fetched += file.path
                into.writeBytes(byteArrayOf(1, 2, 3))
                3
            })
            load = state
        }
        fun open(path: String): WholeLoad {
            file = SessionFileContent(path, FilesPaths.name(path), size = 3, stream = true)
            val name = AudioFiles.cacheName(path, 3, FilesPaths.name(path))
            settle { (load as? WholeLoad.Ready)?.file?.name == name || load is WholeLoad.Failed }
            return load
        }
        settle { load !is WholeLoad.Fetching }
        val a = (load as WholeLoad.Ready).file
        assertArrayEquals(byteArrayOf(1, 2, 3), a.readBytes())
        assertTrue(open("/m/b.mp4") is WholeLoad.Ready)
        // Back to a: from the cache, not fetched again.
        assertEquals(a, (open("/m/a.mp4") as WholeLoad.Ready).file)
        assertEquals(listOf("/m/a.mp4", "/m/b.mp4"), fetched)
        // A third: only the newest two stay in the folder.
        assertTrue(open("/m/c.mp4") is WholeLoad.Ready)
        val folder = a.parentFile!!
        assertTrue(folder.listFiles()!!.count { !it.name.contains(".part") } <= 2)
        // A node too old for file/download says so.
        fail = HubFailure(HubError.Server, "x", status = 409, code = "node_outdated")
        val failed = open("/m/d.mp4")
        assertTrue(failed is WholeLoad.Failed)
        assertEquals(resources.getString(R.string.files_audio_node_outdated), (failed as WholeLoad.Failed).reason)
        assertFalse(File(folder, AudioFiles.cacheName("/m/d.mp4", 3, "d.mp4")).exists())
        folder.deleteRecursively()
    }

    @Test fun aPreviewCarryingTheWholeFileIsNotFetched() {
        var fetched = 0
        var load: WholeLoad = WholeLoad.Fetching
        val file = SessionFileContent("/m/s.png", "s.png", size = 3, mime = "image/png", data = "AQID")
        rule.setContent {
            val state by rememberWholeFile(file, "files-test-carried", { fetched++; error("not fetched") })
            load = state
        }
        settle { load !is WholeLoad.Fetching }
        assertArrayEquals(byteArrayOf(1, 2, 3), (load as WholeLoad.Ready).file.readBytes())
        assertEquals(0, fetched)
        (load as WholeLoad.Ready).file.parentFile!!.deleteRecursively()
    }
}
