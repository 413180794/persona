package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.ExtensionView
import persona.core.designsystem.*
import java.io.File

/**
 * The component showcase extension's pages (#1852: schema/extension/examples/showcase, copied from the extension's snapshots/) drawn
 * on the JVM with Robolectric's native graphics, to compare with the web's, iOS's and the Mac's screenshots of the same
 * views. Every page in every style, light and dark. Written to
 * `build/extension-showcase-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h4000dp-xhdpi")
class ExtensionShowcaseShotsTest {
    @get:Rule val rule = createComposeRule()

    private val pages: Map<String, ExtensionView> = File("../../../schema/extension/examples/showcase").listFiles { f -> f.extension == "json" }!!
        .sortedBy { it.name }
        .associate { it.nameWithoutExtension to JsonCodec.decodeFromString(ExtensionView.serializer(), it.readText()) }

    private val handle = ExtensionViewHandle({ _, _ -> }, { _, _ -> }, {}, {})

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/extension-showcase-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        // markdown is parsed off the main thread (the renderer's own coroutine), which waitForIdle does not wait for
        if (name.contains("markdown") || name.contains("detail")) repeat(3) { Thread.sleep(200); rule.mainClock.advanceTimeBy(100); rule.waitForIdle() }
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        assertTrue("$name drew nothing", other > 50)
    }

    private fun compact(name: String) = name == "card" || name.startsWith("widget-") || name.startsWith("shortcut-")

    @Test fun readsEveryPage() {
        assertTrue("${pages.size} pages", pages.size > 30)
        // every component the schema has is on some page, and the app reads it as that component
        val types = pages.values.flatMap { v -> v.sections.flatMap { s -> s.items.map { it.type } } }.toSet()
        assertEquals(emptySet<String>(), setOf("text", "metric", "progress", "row", "button", "link", "field", "divider", "chart",
            "stat_grid", "segmented", "markdown", "table", "empty", "loading") - types)
    }

    @Test fun drawsEveryPage() {
        var name by mutableStateOf(pages.keys.first())
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val view = pages.getValue(name)
                val state = ExtensionViewState(view, values = ExtensionViewText.fields(view), loadingMore = null)
                // each page its own composition, as each page of the app is (the search box keeps what was typed)
                key(name) { Box(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background)) {
                    if (compact(name)) JCard(Modifier.padding(Tokens.Space.l)) { ExtensionCardContent(state, handle) }
                    else LazyColumn(Modifier.fillMaxWidth(), contentPadding = PaddingValues(horizontal = Tokens.Space.l, vertical = Tokens.Space.l)) {
                        extensionViewItems(name, state, handle)
                    }
                } }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) for (p in pages.keys) {
            name = p; dark = d; style = s
            capture("showcase-$p-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }
}
