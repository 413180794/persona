package persona.core.designsystem

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** [FileThumbnailIcon] asks again for a new key instead of keeping the last file's picture. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35])
class FileThumbnailIconTest {
    @get:Rule val rule = createComposeRule()

    @Before fun forget() = FileThumbnails.clear()
    @After fun forgetAgain() = FileThumbnails.clear()

    private val picture = ImageBitmap(1, 1)
    private val decode: suspend (ByteArray) -> ImageBitmap? = { picture }

    @Test fun theIconLoadsAgainWhenItsKeyChanges() {
        // a.png's picture is already there: the icon shows it without asking.
        kotlinx.coroutines.runBlocking { FileThumbnails.picture("a.png:1", 64, { byteArrayOf(1) }, decode) }
        val asked = mutableListOf<Int>()
        var shown by mutableStateOf("a.png:1")
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                FileThumbnailIcon(shown, persona.core.designsystem.icons.PhIcons.Image, { asked += it; null })
            }
        }
        rule.waitForIdle()
        assertEquals(0, asked.size)
        // A rewritten file (a new size or time in the key) or a reused row asks for its own picture, not keeping a's.
        shown = "a.png:2"
        // The load runs off the main thread: wait for it a little at a time (waitUntil alone does not let it finish).
        repeat(50) { if (asked.isEmpty()) { rule.waitForIdle(); Thread.sleep(100) } }
        assertEquals(1, asked.size)
    }
}
