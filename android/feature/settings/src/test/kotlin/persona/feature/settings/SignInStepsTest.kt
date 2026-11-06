package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.flow.flowOf
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.annotation.Implementation
import org.robolectric.annotation.Implements
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * A sign-in step (Settings › Connectors) or a binding step (Settings › Channels) the app cannot draw says so, with "Start again", never an
 * empty form whose "Next" stays grey forever; a QR step without its modules shows the link it encodes (#4488, #1693).
 * In English; the pictures of each style, light and dark, are in build/signin-shots.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1600dp-xhdpi")
class SignInStepsTest {
    @get:Rule val rule = createComposeRule()

    private val next = ApplicationProvider.getApplicationContext<android.content.Context>().getString(R.string.settings_src_next)

    /** A connector that signs in here, and the one step it answers; asked again by itself, it never answers. */
    private class Connector(private val answer: BindPrompt) : ConnectorsApiStub() {
        val steps = mutableListOf<String>()
        override suspend fun connectors(all: Boolean) =
            listOf(HubConnector("example", "Example", status = "not_configured", enabled = true, capabilities = listOf("login")))
        override suspend fun connectorLogin(id: String, step: String, values: Map<String, String>): BindPrompt {
            steps += step
            if (step.isNotEmpty()) awaitCancellation()
            return answer
        }
    }

    /** A QR channel not bound yet, and the one binding step it answers; asked again by itself, it never answers. */
    private class Channels(private val answer: BindPrompt) : ChannelManagement {
        val steps = mutableListOf<String?>()
        private val chat = HubChannel(id = "chat", name = "Example Chat", kind = "im", source = "plugin", installed = true, enabled = true,
            state = "not_connected", bind = "qr")
        override val available = flowOf(true)
        override suspend fun channels() = listOf(chat)
        override suspend fun channel(id: String) = ChannelDetail(chat)
        override suspend fun update(id: String, change: ChannelChange) = channel(id)
        override suspend fun bind(id: String, step: String?, values: Map<String, String>?): BindPrompt {
            steps += step
            if (step != null) awaitCancellation()
            return answer
        }
        override suspend fun unbind(id: String) = channel(id)
        override suspend fun test(id: String) {}
    }

    private fun signIn(hub: Connector) {
        rule.setContent { PersonaTheme(darkTheme = false, reduceMotion = true) { HubConnectorPage(hub, "example", onBack = {}) } }
        rule.onNode(hasText("Sign in to Example") and hasClickAction()).performClick()
        rule.waitForIdle()
        rule.mainClock.advanceTimeBy(5_000)
        rule.waitForIdle()
    }

    private fun bind(hub: Channels) {
        rule.setContent { PersonaTheme(darkTheme = false, reduceMotion = true) { ChannelPage(hub, "chat", onBack = {}, onOpen = {}) } }
        rule.waitForIdle()
        rule.onNode(hasText("Scan to link") and hasClickAction()).performClick()
        rule.waitForIdle()
        rule.mainClock.advanceTimeBy(5_000)
        rule.waitForIdle()
    }

    @Test fun aKindItDoesNotKnowSaysSoAndStartsOver() {
        val hub = Connector(BindPrompt(step = "auth", kind = "open_url", url = "https://example.com/auth"))
        signIn(hub)
        rule.onAllNodesWithText(next).assertCountEquals(0)
        rule.onNodeWithText("Update the app", substring = true).assertExists()
        rule.onNode(hasText("Start over") and hasClickAction()).performClick()
        rule.waitForIdle()
        // "Start again" asks for the first step again; nothing asked again by itself.
        assertEquals(listOf("", ""), hub.steps)
    }

    @Test fun aFormWithNoFieldsSaysSo() {
        signIn(Connector(BindPrompt(step = "wait", hint = "Hold on")))
        rule.onAllNodesWithText(next).assertCountEquals(0)
        rule.onNodeWithText("missing what it should show", substring = true).assertExists()
        rule.onNode(hasText("Start over") and hasClickAction()).assertExists()
    }

    /** GitHub's sign-in that did not check out: its error says what happened, not that the step is missing something. */
    @Test fun aStepWithOnlyTheHubsErrorShowsTheError() {
        signIn(Connector(BindPrompt(step = "device", error = "Token expired")))
        rule.onNodeWithText("Token expired").assertExists()
        rule.onAllNodesWithText("missing what it should show", substring = true).assertCountEquals(0)
        rule.onNode(hasText("Start over") and hasClickAction()).assertExists()
        rule.onAllNodesWithText(next).assertCountEquals(0)
    }

    /** A device code with only its page (any case, http or https): a button to open it, no empty code box, still asked. */
    @Test fun aDeviceCodeWithOnlyItsPageOpensIt() {
        val hub = Connector(BindPrompt(step = "device", kind = "device_code", url = "http://example.com/device", interval = 1))
        signIn(hub)
        rule.onNode(hasText("Open webpage") and hasClickAction()).assertExists()
        rule.onAllNodesWithText("Copy code").assertCountEquals(0)
        assertEquals(listOf("", "device"), hub.steps)
    }

    @Test fun aChannelsDeviceCodeWithOnlyItsPageOpensIt() {
        val channel = Channels(BindPrompt(step = "device", kind = "device_code", url = "HTTPS://example.com/device", interval = 1))
        bind(channel)
        rule.onNode(hasText("Open webpage") and hasClickAction()).assertExists()
        assertEquals(listOf(null, "device"), channel.steps)
    }

    @Test fun anExtensionsDeviceCodeWithOnlyItsPageOpensIt() {
        val prompt = BindPrompt(step = "device", kind = "device_code", url = "HTTPS://example.com/device", interval = 1)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                ConnectorSignInStep("Example", prompt, BindState(prompt), ConnectorSignInModel({ _, _ -> prompt }, {}), deviceLink = true)
            }
        }
        rule.onNode(hasText("Open the sign-in page") and hasClickAction()).assertExists()
    }

    @Test fun aQrCodeWithNothingToShowSaysSo() {
        val hub = Channels(BindPrompt(step = "qr", kind = "qr", interval = 1, hint = "Scan it"))
        bind(hub)
        rule.onNodeWithText("missing what it should show", substring = true).assertExists()
        rule.onNode(hasText("Start over") and hasClickAction()).assertExists()
        // Not asked again by itself: it would never change.
        assertEquals(listOf<String?>(null), hub.steps)
    }

    @Test fun aQrCodeWithoutItsModulesShowsTheLinkAndWaits() {
        val hub = Connector(BindPrompt(step = "qr", kind = "qr", url = "https://example.com/q", interval = 1))
        signIn(hub)
        rule.onNodeWithText("https://example.com/q").assertExists()
        rule.onAllNodesWithText(next).assertCountEquals(0)
        rule.onAllNodesWithText("Start over").assertCountEquals(0)
        // Still asked again by itself until it is confirmed.
        assertEquals(listOf("", "qr"), hub.steps)
    }

    /** A pair code on Settings › Connectors is sent to the bot from the chat app: the page waits for that, not for an authorization (#4983). */
    @Test fun aPairCodeWaitsForItToBeSent() {
        val prompt = BindPrompt(step = "pair", kind = "pair_code", userCode = "482913", interval = 3)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                ConnectorSignInStep("Example Chat", prompt, BindState(prompt), ConnectorSignInModel({ _, _ -> prompt }, {}))
            }
        }
        rule.onNodeWithText("Waiting for you to send it to the bot in Example Chat…").assertExists()
        rule.onAllNodesWithText("Waiting for you to authorize…").assertCountEquals(0)
    }

    /** The link of a QR step without its modules can be long-pressed and copied, not only with the copy button (#4983). */
    @OptIn(ExperimentalTestApi::class)
    @Config(shadows = [NoMagnifier::class])
    @Test fun theQrLinkCanBeSelectedAndCopied() {
        val prompt = BindPrompt(step = "qr", kind = "qr", url = "https://example.com/q", interval = 1)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                ConnectorSignInStep("Example", prompt, BindState(prompt), ConnectorSignInModel({ _, _ -> prompt }, {}))
            }
        }
        // Long press at the start selects; dragging to the end takes the whole link; Ctrl+C copies it, as Copy does.
        val link = rule.onNodeWithText("https://example.com/q")
        link.performTouchInput {
            down(centerLeft + androidx.compose.ui.geometry.Offset(2f, 0f))
            advanceEventTime(viewConfiguration.longPressTimeoutMillis + 100)
            moveTo(centerRight - androidx.compose.ui.geometry.Offset(2f, 0f))
            up()
        }
        rule.waitForIdle()
        link.performKeyInput { withKeyDown(Key.CtrlLeft) { pressKey(Key.C) } }
        rule.waitForIdle()
        val clipboard = ApplicationProvider.getApplicationContext<android.content.Context>()
            .getSystemService(android.content.ClipboardManager::class.java)
        assertEquals("https://example.com/q", clipboard.primaryClip?.getItemAt(0)?.text?.toString())
    }

    /** The two new states in every style, light and dark, to look at. */
    @Test fun drawsTheNewStepsInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var link by mutableStateOf(false)
        var run by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                key(run) {
                    val prompt = if (link) BindPrompt(step = "qr", kind = "qr", url = "https://example.com/q", interval = 1, hint = "Scan it")
                        else BindPrompt(step = "qr", kind = "qr", interval = 1, hint = "Scan it")
                    val model = ConnectorSignInModel({ _, _ -> prompt }, {})
                    Box(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background)) {
                        ConnectorSignInStep("Example", prompt, BindState(prompt), model)
                    }
                }
            }
        }
        val dir = File("build/signin-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) for (l in listOf(false, true)) {
            style = s; dark = d; link = l; run++
            rule.waitForIdle()
            rule.onNodeWithText(if (l) "https://example.com/q" else "Start over").assertExists()
            val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(dir, "${if (l) "qr-link" else "cannot-draw"}-${s.key}-${if (d) "dark" else "light"}.png").outputStream()
                .use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}

/** Robolectric's [android.widget.Magnifier] has no surface and throws when a selection's magnifier is dismissed. */
@Implements(android.widget.Magnifier::class)
class NoMagnifier {
    @Implementation fun show(sourceCenterX: Float, sourceCenterY: Float, magnifierCenterX: Float, magnifierCenterY: Float) {}
    @Implementation fun dismiss() {}
    @Implementation fun update() {}
}
