package persona.feature.agents

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Interaction
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import java.io.File
import java.time.Instant

/**
 * A request card whose answer was not confirmed (buttons disabled) next to one after the check (buttons work again,
 * #3791), in light and dark. Written as PNGs to `build/request-retry-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
class RequestRetryShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/request-retry-shots").also { it.mkdirs() }

    private val now = Instant.parse("2026-11-06T08:00:00Z")
    private fun card(id: String) = Interaction(id, machineId = "m", sessionId = "s", kind = "permission", toolName = "Edit",
        sessionTitle = "修 Agents 页的回答", expiresAt = now.plusSeconds(600).toString())

    @Test fun drawsUnconfirmedAndRetryCardsInLightAndDark() {
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = dark, reduceMotion = true) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    RequestCard(card("unknown"), "mac", AgentsMetadata(), now, online = true, delivery = RequestDelivery.Unknown,
                        onOpen = {}, onAnswer = { _, _, _, _ -> })
                    RequestCard(card("retry"), "mac", AgentsMetadata(), now, online = true, delivery = RequestDelivery.Retry,
                        onOpen = {}, onAnswer = { _, _, _, _ -> })
                }
            }
        }
        rule.onNodeWithText("上次没确认，可以再点一次").assertExists()
        // Each card has its "Allow" twice (full actions and quick actions): the unconfirmed card's are disabled, the retry card's are not.
        val allow = rule.onAllNodesWithText("允许")
        val nodes = allow.fetchSemanticsNodes()
        assertTrue("Both cards draw 允许", nodes.size >= 2)
        allow[0].assertIsNotEnabled()
        allow[nodes.size - 1].assertIsEnabled()

        val shots = mutableListOf<Bitmap>()
        for (nextDark in listOf(false, true)) {
            rule.runOnIdle { dark = nextDark }
            Snapshot.sendApplyNotifications()
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            shots += bitmap
            File(out(), "request-retry-${if (nextDark) "dark" else "light"}.png").outputStream().use {
                assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it))
            }
        }
        assertFalse("Light and dark must differ", shots[0].sameAs(shots[1]))
    }
}
