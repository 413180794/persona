package persona.core.designsystem

import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import dev.chrisbanes.haze.HazeState
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/**
 * A page embedded in another (the conversation on the Persona page, #3040) is the only glass source of the pair. When
 * the host's content was a source of the same state as well, the embedded page's bottom edge, composer and task chips
 * all sat inside it, and Haze does not let an effect draw an ancestor source (nor anything below its z-index), so they
 * blurred nothing: the conversation showed through raw under the chips and between them and the composer. Robolectric
 * does not render the blur itself, so this checks the sources the state ends up with.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class NestedPageGlassTest {
    @get:Rule val rule = createComposeRule()

    private val dock: @Composable () -> Unit = { Spacer(Modifier.fillMaxWidth().height(80.dp)) }

    /** The source areas registered with [state] (Haze keeps the list internal). */
    private fun sources(state: HazeState): Int =
        (HazeState::class.java.getMethod("getAreas\$haze_root_haze").invoke(state) as List<*>).size

    private fun render(state: HazeState, content: @Composable () -> Unit) {
        rule.setContent {
            PersonaTheme(darkTheme = true, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalGlassSource provides state) { content() }
            }
        }
        rule.waitForIdle()
    }

    @Test fun aPageIsAGlassSource() {
        val state = HazeState()
        render(state) { JPage("page", topBar = {}, bottomBar = dock) { Text("正文") } }
        assertEquals(1, sources(state))
    }

    @Test fun anEmbeddedPageIsTheOnlyGlassSource() {
        val state = HazeState()
        render(state) {
            // The Persona page around its conversation: no edges of its own, the conversation's page inside it.
            JPage("host", topBar = {}, edges = false) {
                JPage("conversation", Modifier.fillMaxSize(), topBar = {}, bottomBar = dock) { Text("正文") }
            }
        }
        assertEquals(1, sources(state))
    }
}
