package persona.feature.files

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.shadows.ShadowMediaPlayer
import org.robolectric.shadows.util.DataSource
import persona.core.contracts.api.SessionFileContent
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * The audio player: what counts as audio, the cache name and times (plain), and the page on the JVM with Robolectric's
 * MediaPlayer (a 3:25 file): fetched through download, played, in every style, light and dark (`build/files-shots/`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h500dp-xhdpi")
class AudioFileTest {
    @get:Rule val rule = createComposeRule()

    @Test fun whatIsAudio() {
        assertTrue(AudioFiles.isAudio(SessionFileContent("/a/x.bin", "x.bin", mime = "audio/mpeg", data = "AA==")))
        assertTrue(AudioFiles.isAudio(SessionFileContent("/a/x.m4a", "x.m4a", mime = "application/octet-stream", data = "AA==")))
        assertFalse(AudioFiles.isAudio(SessionFileContent("/a/x.txt", "x.txt", mime = "text/plain", text = "hi")))
        assertFalse(AudioFiles.isAudio(SessionFileContent("/a/x.png", "x.png", mime = "image/png", data = "AA==")))
    }

    @Test fun cacheNamesFollowPathAndSize() {
        val a = AudioFiles.cacheName("/m/a.mp3", 10, "a.mp3")
        assertTrue(a, a.endsWith(".mp3"))
        assertEquals(a, AudioFiles.cacheName("/m/a.mp3", 10, "a.mp3"))
        assertNotEquals(a, AudioFiles.cacheName("/m/a.mp3", 11, "a.mp3"))
        assertFalse(AudioFiles.cacheName("/m/x", 1, "../../evil").contains('/'))
    }

    @Test fun cacheNamesFollowTheRevision() {
        // #4821: a file rewritten at the same size is another revision, so it is fetched again.
        val a = AudioFiles.cacheName("/m/a.wav", 10, "a.wav", "10-1")
        assertNotEquals(a, AudioFiles.cacheName("/m/a.wav", 10, "a.wav", "10-2"))
        assertEquals(AudioFiles.cacheName("/m/a.wav", 10, "a.wav"), AudioFiles.cacheName("/m/a.wav", 10, "a.wav", null))
    }

    @Test fun clock() {
        assertEquals("0:00", AudioFiles.clock(0))
        assertEquals("3:25", AudioFiles.clock(205_400))
        assertEquals("1:02:03", AudioFiles.clock(3_723_000))
        assertEquals("0:00", AudioFiles.clock(-5))
    }

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/files-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun downloadsAndPlaysInEveryStyle() {
        val file = SessionFileContent("/m/voice memo.m4a", "voice memo.m4a", size = 5, mime = "audio/mp4", data = "AAAAAAA=", truncated = true)
        val downloads = mutableListOf<String>()
        val download: suspend (String, File) -> Long = { path, into ->
            downloads += path
            into.writeBytes(ByteArray(5))
            ShadowMediaPlayer.addMediaInfo(DataSource.toDataSource(into.path), ShadowMediaPlayer.MediaInfo(205_400, 0))
            5
        }
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").size(400.dp, 500.dp).background(PersonaTheme.palette.background)) {
                    AudioFile(file, download, PaddingValues(), header = { item { FileMeta(file.size, false) } })
                }
            }
        }
        rule.waitUntil(20_000) { rule.onAllNodesWithText("3:25").fetchSemanticsNodes().isNotEmpty() }
        assertEquals(listOf("/m/voice memo.m4a"), downloads)
        rule.onNodeWithContentDescription("播放").performClick()
        rule.onNodeWithContentDescription("暂停").assertExists()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            capture("audio-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
        rule.onNodeWithText("voice memo.m4a").assertExists()
    }
}
