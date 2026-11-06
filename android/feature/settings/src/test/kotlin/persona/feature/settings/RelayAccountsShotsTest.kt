package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.*
import java.io.File

/**
 * quota-relay's sidebar page (#4576) drawn on the JVM from the extension's made-up fixture: the default view (its
 * top and its end), the Kiro type and one account's page, every style light and dark. Written to
 * `build/relay-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h4000dp-xhdpi")
class RelayAccountsShotsTest {
    @get:Rule val rule = createComposeRule()

    private val pages = listOf("default", "kiro", "detail").associateWith(RelayFixture::view)
    private val handle = ExtensionViewHandle({ _, _ -> }, { _, _ -> }, {}, {})

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/relay-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        repeat(2) { Thread.sleep(100); rule.mainClock.advanceTimeBy(100); rule.waitForIdle() }
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        File(out(), "android-$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        assertTrue("$name drew nothing", other > 50)
    }

    @Test fun drawsThePage() {
        var name by mutableStateOf("default")
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        lateinit var list: LazyListState
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val view = pages.getValue(name)
                val state = ExtensionViewState(view, values = ExtensionViewText.fields(view))
                key(name, style, dark) {
                    list = remember { LazyListState() }
                    Box(Modifier.testTag("shot").width(400.dp).height(1400.dp).background(PersonaTheme.palette.background)) {
                        LazyColumn(Modifier.fillMaxWidth(), state = list, contentPadding = PaddingValues(Tokens.Space.l)) {
                            extensionViewItems(name, state, handle)
                        }
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            val tail = "${s.name.lowercase()}-${if (d) "dark" else "light"}"
            for (p in pages.keys) {
                name = p; style = s; dark = d
                capture("$p-$tail")
            }
            name = "default"
            rule.waitForIdle()
            rule.runOnIdle { runBlocking { list.scrollToItem(list.layoutInfo.totalItemsCount - 1) } }
            capture("default-end-$tail")
        }
    }
}
