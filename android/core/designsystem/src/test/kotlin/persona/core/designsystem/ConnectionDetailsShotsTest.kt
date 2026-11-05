package persona.core.designsystem

import android.content.res.Configuration
import android.graphics.Bitmap
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.SessionKey
import persona.core.contracts.CoreText
import persona.core.contracts.api.ConnectionDiagnostics
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.DeliveryState
import persona.core.contracts.api.OutboxMessage
import java.io.File
import java.time.Instant

/**
 * The connection details a page's title opens while the hub is out of reach (ConnectionDetails.kt), drawn on the JVM
 * in every style, light and dark, and written to `build/connection-details-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class ConnectionDetailsShotsTest {
    @get:Rule val rule = createComposeRule()

    private val now = Instant.parse("2026-11-05T10:00:00Z")
    private val key = SessionKey("m", "s")
    private val source = ConnectionDetailsSource(
        diagnostics = MutableStateFlow(ConnectionDiagnostics(
            lastAttemptFailure = CoreText.of(CoreText.Key.EndpointTimeout), nextRetryAt = now.plusSeconds(7).toString(), lastReceivedAt = now.minusSeconds(95).toString())),
        outbox = MutableStateFlow(listOf(OutboxMessage("a", key, "x", DeliveryState.Queued), OutboxMessage("b", key, "y", DeliveryState.WaitingForNetwork))),
        reconnect = { reconnects++ },
    )
    private var reconnects = 0

    @Test fun aRefusedConnectionReadsInTheUsersLanguage() {
        // The transport records HubFailure(ConnectionRefused) as its label, never the enum's name.
        val failure = persona.core.hub.HubFailure(persona.core.hub.HubError.ConnectionRefused)
        val diagnostics = ConnectionDiagnostics(lastDisconnectReason = failure.message,
            lastAttemptFailure = persona.core.hub.endpointResultLabel(failure.kind))
        val fact = connectionFacts(ConnectionState.Connecting, diagnostics, 0, now).single() as ConnectionFact.LastFailure
        val resources = androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>().resources
        val text = fact.reason.resolve(resources)
        assertEquals(resources.getString(R.string.designsystem_core_endpoint_refused), text)
        assertTrue(text, "ConnectionRefused" !in text)
    }

    @Test fun detailsInEveryStyleAndAppearance() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var state by mutableStateOf(ConnectionState.Connecting)
        var retried = 0
        var settings = 0
        rule.setContent {
            val configuration = Configuration(LocalConfiguration.current).apply {
                uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                    if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
            }
            CompositionLocalProvider(LocalConfiguration provides configuration, LocalConnectionDetails provides source) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    key(style, dark, state) {
                        ConnectionDetailsSheet(state, onDismiss = {}, onRetry = { retried++ }, onSettings = { settings++ }, clock = { now })
                    }
                }
            }
        }
        val shots = buildList {
            AppStyle.entries.forEach { s -> listOf(false, true).forEach { d -> add(Triple(s, d, ConnectionState.Connecting)) } }
            listOf(false, true).forEach { d ->
                add(Triple(AppStyle.Glass, d, ConnectionState.WaitingForNetwork))
                add(Triple(AppStyle.Glass, d, ConnectionState.Revoked))
            }
        }
        for ((s, d, st) in shots) {
            style = s; dark = d; state = st
            rule.mainClock.advanceTimeBy(1_000)
            rule.waitForIdle()
            when (st) {
                ConnectionState.Connecting -> {
                    rule.onNodeWithText("上次失败：连接超时").assertExists()
                    rule.onNodeWithText("7 秒后重试").assertExists()
                    rule.onNodeWithText("上次连上：1 分钟前").assertExists()
                    rule.onNodeWithText("2 条消息连上后自动发送").assertExists()
                    rule.onNodeWithText("立即重试").assertExists()
                }
                ConnectionState.WaitingForNetwork -> rule.onNodeWithText("等待网络").assertExists()
                else -> rule.onNodeWithText("立即重试").assertDoesNotExist()
            }
            val bitmap = rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
            val background = bitmap.getPixel(1, 1)
            var other = 0
            for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) {
                if (bitmap.getPixel(x, y) != background) other++
            }
            assertTrue("$s/$d/$st drew nothing", other > 100)
            val out = File(System.getProperty("persona.shots") ?: "build/connection-details-shots").apply { mkdirs() }
            File(out, "details-${st.name.lowercase()}-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)
            }
        }
        state = ConnectionState.Connecting
        rule.waitForIdle()
        rule.onNodeWithTag("connection-retry").performClick()
        assertEquals(1, reconnects)
        assertEquals(1, retried)
        rule.onNodeWithText("连接设置").performClick()
        assertEquals(1, settings)
    }
}
