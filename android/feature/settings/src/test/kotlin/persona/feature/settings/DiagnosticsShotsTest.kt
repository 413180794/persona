package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

/**
 * "Connection diagnostics" after a pairing that failed at TLS, before any connection exists, drawn on the JVM in every style, light
 * and dark. Written as PNGs to `build/diagnostics-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h2100dp-xhdpi")
class DiagnosticsShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/diagnostics-shots").also { it.mkdirs() }

    @Test fun showsTheLastFailedPairing() {
        val attempts = PairingAttempts()
        val handshake = javax.net.ssl.SSLHandshakeException("Trust anchor for certification path not found.")
        attempts.record(pairingAttempt(PairingRequest("https://hub.example.com/persona", "code"),
            PairingException(PairingFailure.Certificate).apply { initCause(handshake) }, PairingStage.Probe,
            listOf(ConnectionCandidate("https://hub.example.com/persona", "name", persona.core.contracts.CoreText.plain("证书指纹不符"), java.time.Instant.now().toString()))))
        val unpaired = demoSettingsServices(androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>().resources).let { demo ->
            demo.copy(connection = object : HubConnection by demo.connection {
                override val state = MutableStateFlow(ConnectionState.Connecting)
            }, diagnostics = null, demo = false, pairingAttempts = attempts)
        }
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(2000.dp).background(PersonaTheme.palette.background)) {
                    DiagnosticsScreen(unpaired)
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            rule.onNodeWithText("最近一次配对").assertExists()
            rule.onNodeWithText("失败，停在「安全证书」").assertExists()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val background = bitmap.getPixel(1, 1)
            var other = 0
            for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
            assertTrue("drew nothing", other > 100)
            val name = "diagnostics-pairing-${s.name.lowercase()}-${if (d) "dark" else "light"}.png"
            File(out(), name).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
