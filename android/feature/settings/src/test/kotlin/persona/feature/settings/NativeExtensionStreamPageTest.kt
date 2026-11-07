package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.*
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.generated.*
import persona.core.contracts.stream.*
import persona.core.designsystem.*

/** Real Compose/native graphics, injected payloads only. Not a live backend, Deno or emulator E2E. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class NativeExtensionStreamPageTest {
    @get:Rule val rule = createComposeRule()
    private val identity = NativeStreamIdentity("owner", "group-a", "example", "page", "navigation-a")
    private val requests = mutableListOf<ExtensionUIStreamRequest>()
    private lateinit var session: NativeStreamSession
    private val raw = """{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"root","view":{"title":"Native page","sections":[{"key":"main","title":"Details","items":[{"type":"text","key":"text","text":"Original text"},{"type":"field","key":"draft","name":"body","label":"Draft","kind":"text","value":"initial"},{"type":"button","key":"send","title":"Send","action":{"id":"submit","submit":true}}]},{"key":"other","items":[]}]}}"""
    private fun delta(op: String, base: Int) = """{"protocol":1,"session":"session-a","epoch":"epoch-a","base_version":$base,"version":${base + 1},"operations":[$op]}"""
    private fun createSession(payload: String = raw, components: List<String> = listOf("text", "field", "button")) {
        session = NativeStreamSession(identity, ExtensionUIContext(platform = "android", locale = "en", uiVersion = 6,
            stream = ExtensionUIStreamCapability(protocol = 1, components = components)),
            NativeStreamTransport { request ->
                requests.add(request)
                if (request is ExtensionUIStreamOpenRequest) session.snapshot(identity, request.requestId, payload)
            })
    }

    @Test fun realNativeRendererKeepsDraftAndCallbackLocalityAcrossUpdatesAndMoves() {
        createSession()
        rule.setContent {
            PersonaTheme(style = AppStyle.Board, reduceMotion = true) {
                ExtensionUiPage(null, "example", "page", "Page", {}, availableAdapter = NativeExtensionStreamAdapter(session, shown = true))
            }
        }
        rule.onNodeWithText("Original text").assertExists()
        rule.onNodeWithText("initial").performTextReplacement("local draft")
        rule.waitForIdle()
        assertEquals(JsonPrimitive("local draft"), session.state.value.drafts.getValue("draft").value)
        runBlocking { assertTrue(session.delta(identity, delta("""{"op":"replace","key":"text","item":{"type":"text","key":"text","text":"Updated text"}}""", 1))) }
        rule.onNodeWithText("Updated text").assertExists()
        rule.onNodeWithText("local draft").assertExists().assertIsFocused()
        runBlocking { assertTrue(session.delta(identity, delta("""{"op":"move","key":"draft","parent":"other"}""", 2))) }
        rule.onNodeWithText("local draft").assertExists().assertIsFocused()
        rule.onNodeWithText("Send").performClick()
        rule.waitForIdle()
        val event = requests.filterIsInstance<ExtensionUIStreamEventRequest>().single().interaction
        assertEquals("send", event.key)
        assertEquals("submit", event.action)
        assertEquals("press", event.event)
        assertEquals("""{"body":"local draft"}""", event.value.toString())
        runBlocking { session.close() }
    }

    @Test fun nativePageDrawsAllStylesLightAndDarkWithoutChangingTheLegacyDefault() {
        createSession()
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                    ExtensionUiPage(null, "example", "page", "Page", {}, availableAdapter = NativeExtensionStreamAdapter(session, shown = true))
                }
            }
        }
        val out = File("build/native-extension-stream-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) {
            val checksums = mutableListOf<Long>()
            for (d in listOf(false, true)) {
                style = s; dark = d
                Snapshot.sendApplyNotifications()
                rule.waitForIdle()
                rule.onNodeWithText("Original text").assertExists()
                val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                var checksum = 0L
                for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) checksum += bitmap.getPixel(x, y)
                checksums.add(checksum)
                File(out, "${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                    bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)
                }
            }
            assertNotEquals("Light and dark must really differ", checksums[0], checksums[1])
        }
        runBlocking { session.close() }
    }

    @Test fun realLazyScrollAnchorSurvivesAnUnrelatedUpdateAndInsertionAboveIt() {
        val rows = (1..60).joinToString(",") { """{"type":"text","key":"row-$it","text":"Row $it"}""" }
        val longSnapshot = """{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"root","view":{"title":"Native page","sections":[{"key":"main","title":"Details","items":[$rows]}]}}"""
        createSession(longSnapshot)
        rule.setContent {
            PersonaTheme(style = AppStyle.Brief, reduceMotion = true) {
                ExtensionUiPage(null, "example", "page", "Page", {}, availableAdapter = NativeExtensionStreamAdapter(session, shown = true))
            }
        }
        rule.onNode(hasScrollAction()).performScrollToIndex(30)
        rule.onNodeWithText("Row 30").assertIsDisplayed()
        runBlocking { assertTrue(session.delta(identity, delta("""{"op":"replace","key":"row-1","item":{"type":"text","key":"row-1","text":"Updated first row"}}""", 1))) }
        rule.onNodeWithText("Row 30").assertIsDisplayed()
        runBlocking { assertTrue(session.delta(identity, delta("""{"op":"insert","parent":"main","before":"row-1","item":{"type":"text","key":"new","text":"Inserted above"}}""", 2))) }
        rule.onNodeWithText("Row 30").assertIsDisplayed()
        runBlocking { session.close() }
    }

    @Test fun composerConfirmationPreservesValuesAndCanonicalInputName() {
        val payload = """{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"root","view":{"sections":[{"key":"main","items":[{"type":"composer","key":"draft","name":"body","value":"comment","buttons":[{"title":"Post","action":{"id":"post","confirm":"Post this comment?","values":{"target":"selected"}}}]}]}]}}"""
        createSession(payload, listOf("composer"))
        rule.setContent { PersonaTheme(style = AppStyle.Board, reduceMotion = true) {
            NativeExtensionStreamPage(NativeExtensionStreamAdapter(session, true), "Page", {})
        } }
        rule.onNodeWithText("Post").performClick()
        rule.onNodeWithText("Post this comment?").assertExists()
        assertTrue(requests.none { it is ExtensionUIStreamEventRequest })
        rule.onNodeWithText("OK").performClick()
        rule.waitForIdle()
        val event = requests.filterIsInstance<ExtensionUIStreamEventRequest>().single().interaction
        assertEquals("draft", event.key)
        assertEquals("""{"body":"comment","target":"selected"}""", event.value.toString())
        runBlocking { session.close() }
    }

    @Test fun confirmationFromPreviousTreeCannotAuthorizeReplacementAction() {
        val payload = raw.replace("\"id\":\"submit\",\"submit\":true", "\"id\":\"submit\",\"submit\":true,\"confirm\":\"Confirm old action?\"")
        createSession(payload)
        rule.setContent { PersonaTheme(style = AppStyle.Board, reduceMotion = true) {
            NativeExtensionStreamPage(NativeExtensionStreamAdapter(session, true), "Page", {})
        } }
        rule.onNodeWithText("Send").performClick()
        rule.onNodeWithText("Confirm old action?").assertExists()
        runBlocking { assertTrue(session.delta(identity, delta("""{"op":"replace","key":"send","item":{"type":"button","key":"send","title":"Replacement","action":{"id":"submit","submit":true,"values":{"target":"replacement"}}}}""", 1))) }
        rule.onAllNodesWithText("Send").onLast().performClick()
        rule.waitForIdle()
        assertTrue(requests.none { it is ExtensionUIStreamEventRequest })
        runBlocking { session.close() }
    }

    @Test fun splitFileTreeUsesItsOwnKeyAndDoesNotRenameGenericPathArgument() {
        val payload = """{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"root","view":{"layout":"split","sections":[{"key":"side","pane":"side","title":"Files","items":[{"type":"file_tree","key":"files","entries":[{"path":"file.txt","kind":"file"}],"action":{"id":"pick"}}]},{"key":"main","items":[{"type":"field","key":"path","name":"query","label":"Query","kind":"text","value":"initial"}]}]}}"""
        createSession(payload, listOf("file_tree", "field"))
        rule.setContent { PersonaTheme(style = AppStyle.Board, reduceMotion = true) {
            NativeExtensionStreamPage(NativeExtensionStreamAdapter(session, true), "Page", {})
        } }
        rule.onNodeWithText("Files · 1").performClick()
        rule.onNodeWithText("file.txt").performClick()
        rule.waitForIdle()
        val event = requests.filterIsInstance<ExtensionUIStreamEventRequest>().single().interaction
        assertEquals("files", event.key)
        assertEquals("""{"path":"file.txt"}""", event.value.toString())
        runBlocking { session.close() }
    }

    @Test fun chosenRootTabSurvivesUnrelatedDeltaAndIsIncludedInSubmit() {
        val payload = raw.replace("\"title\":\"Native page\"", "\"title\":\"Native page\",\"tabs\":{\"name\":\"scope\",\"value\":\"first\",\"items\":[{\"value\":\"first\",\"title\":\"First tab\"},{\"value\":\"second\",\"title\":\"Second tab\"}],\"action\":{\"id\":\"tab\"}}")
        createSession(payload.replace("\"id\":\"tab\"", "\"id\":\"tab\",\"confirm\":\"Change scope?\""))
        rule.setContent { PersonaTheme(style = AppStyle.Board, reduceMotion = true) {
            NativeExtensionStreamPage(NativeExtensionStreamAdapter(session, true), "Page", {})
        } }
        rule.onNodeWithText("Second tab").performClick()
        rule.onNodeWithText("Change scope?").assertExists()
        assertTrue(requests.none { it is ExtensionUIStreamEventRequest })
        rule.onNodeWithText("OK").performClick()
        rule.waitForIdle()
        val changed = requests.filterIsInstance<ExtensionUIStreamEventRequest>().single()
        assertEquals("root", changed.interaction.key)
        assertEquals("change", changed.interaction.event)
        runBlocking {
            session.acknowledged(identity, changed.requestId)
            assertTrue(session.delta(identity, delta("""{"op":"replace","key":"text","item":{"type":"text","key":"text","text":"Updated text"}}""", 1)))
        }
        rule.onNodeWithText("Send").performClick()
        rule.waitForIdle()
        val submitted = requests.filterIsInstance<ExtensionUIStreamEventRequest>().last().interaction
        assertEquals("""{"body":"initial","scope":"second"}""", submitted.value.toString())
        runBlocking { session.close() }
    }

    @Test fun untouchedToggleSubmitsItsDisplayedOffValueWithoutMutatingRawPresence() {
        val payload = raw.replace("\"type\":\"field\",\"key\":\"draft\",\"name\":\"body\",\"label\":\"Draft\",\"kind\":\"text\",\"value\":\"initial\"", "\"type\":\"field\",\"key\":\"draft\",\"name\":\"enabled\",\"label\":\"Enabled\",\"kind\":\"toggle\"")
        createSession(payload)
        rule.setContent { PersonaTheme(style = AppStyle.Board, reduceMotion = true) {
            NativeExtensionStreamPage(NativeExtensionStreamAdapter(session, true), "Page", {})
        } }
        rule.onNodeWithText("Send").performClick()
        rule.waitForIdle()
        val submitted = requests.filterIsInstance<ExtensionUIStreamEventRequest>().single().interaction
        assertEquals("""{"enabled":"false"}""", submitted.value.toString())
        assertFalse(session.state.value.tree!!.nodes.getValue("draft").containsKey("value"))
        runBlocking { session.close() }
    }

    @Test fun adapterRejectsTagsItsDisplayProjectionCannotFullyRender() {
        listOf("html", "unknown").forEach { component ->
            createSession(components = listOf(component))
            assertTrue(component, runCatching { NativeExtensionStreamAdapter(session, true) }.isFailure)
        }
        assertTrue(requests.isEmpty())
    }

    @Test fun displayProjectionKeepsCanonicalNamesSeparateFromDraftKeys() = runBlocking {
        createSession()
        session.visibility(true, true)
        session.edit("draft", JsonPrimitive("local"))
        val display = nativeExtensionDisplay(session.state.value)
        assertEquals("draft", display.view!!.sections[0].items[1].name)
        assertEquals("local", display.values["draft"])
        assertEquals("body", session.state.value.tree!!.nodes.getValue("draft")["name"]!!.toString().trim('"'))
        session.close()
    }
}
