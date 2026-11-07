package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.LookCurrent
import persona.core.contracts.api.LookListing
import persona.core.contracts.api.LookView
import persona.core.contracts.api.PersonaInfo
import persona.core.contracts.api.PersonaManifest
import persona.core.contracts.api.PersonaPacks
import persona.core.designsystem.AppStyle
import persona.core.designsystem.AppStyleStore
import persona.core.designsystem.LocalAppStyleStore
import persona.core.designsystem.LocalPersonaDesign
import persona.core.designsystem.LocalPersonaPack
import persona.core.designsystem.LocalPersonaStore
import persona.core.designsystem.PersonaDesign
import persona.core.designsystem.PersonaStore
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import java.io.File

/**
 * Settings › Appearance › Avatar with a hub that keeps the choice and has the example extension's looks (#3200,
 * schema/extension/examples/example-looks), drawn on the JVM (no emulator) light and dark: a built-in look, the
 * extension's Aurora (its dark avatar on the dark page, its five states) and Pebble (an avatar only). Written as PNGs to
 * `build/looks-shots/` (or `-Dpersona.shots=<dir>`). The store's sync with the hub is checked on the way.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1500dp-xhdpi")
class LooksShotsTest {
    @get:Rule val rule = createComposeRule()

    private val example = generateSequence(File("").absoluteFile) { it.parentFile }
        .map { File(it, "schema/extension/examples/example-looks") }.first { it.isDirectory }

    /** The extension's looks as the hub serves them: a made persona.json each (a look without a dot gets its avatar as idle). */
    private fun extensionLooks(): List<LookView> {
        val ext = Json.parseToJsonElement(File(example, "persona-extension.json").readText()).jsonObject
        return ext.getValue("contributes").jsonObject.getValue("looks").jsonArray.mapIndexed { i, raw ->
            val l = raw.jsonObject
            val avatar = l.getValue("avatar").jsonObject
            val sizes = avatar.getValue("sizes").jsonObject
            val dot = l["dot"] ?: buildJsonObject {
                val side = sizes.keys.maxBy { it.toInt() }
                put("size", side.toInt())
                put("states", buildJsonObject { put("idle", buildJsonObject { put("file", sizes.getValue(side).jsonPrimitive.content) }) })
            }
            val manifest = JsonObject(l + mapOf("format" to Json.parseToJsonElement("1"), "dot" to dot,
                "source" to buildJsonObject { put("kind", "extension"); put("extension_name", ext.getValue("name").jsonPrimitive.content) }))
            LookView(l.getValue("id").jsonPrimitive.content, "extension", l.getValue("name").jsonPrimitive.content,
                appIcon = l.getValue("app_icon_fallback").jsonPrimitive.content, extensionName = ext.getValue("name").jsonPrimitive.content,
                rev = "0123456789abcde$i", manifest = JsonCodec.decodeFromJsonElement(PersonaManifest.serializer(), manifest))
        }
    }

    private inner class FakeHub(
        var current: LookCurrent,
        val looks: List<LookView> = PersonaDesign.entries.map { LookView(it.id, "builtin", it.id, appIcon = it.id) } + extensionLooks(),
        private val files: File = example,
    ) : PersonaPacks {
        val chosen = mutableListOf<Pair<String, Boolean>>()
        override suspend fun list(): List<PersonaInfo> = looks.mapNotNull { it.pack }
        override suspend fun file(id: String, file: String): ByteArray = File(files, file).readBytes()
        override suspend fun looks(): LookListing = LookListing(looks, current)
        override suspend fun choose(id: String, ifUnset: Boolean): LookCurrent {
            chosen += id to ifUnset
            if (!(ifUnset && current.id != null)) current = LookCurrent(id, id)
            return current
        }
    }

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/looks-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun drawsTheLooksAndKeepsTheChoiceWithTheHub() {
        // Native graphics loaded first on the test's thread: the store decodes the pictures on a background thread,
        // where Robolectric's loader would not find the classes it needs to start them.
        android.graphics.BitmapFactory.decodeFile(File(example, "looks/pebble/avatar-256.png").path)
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        context.getSharedPreferences("persona-appearance", 0).edit().clear().putString("persona", "persona").commit()
        val style = AppStyleStore(context)
        val hub = FakeHub(LookCurrent(null, "persona"))
        val store = PersonaStore(context, style) { hub }
        runBlocking { store.refresh() }
        // This phone's explicit default moved up to a hub that had none.
        assertEquals(listOf("persona" to true), hub.chosen)
        assertEquals(listOf("aurora", "pebble"), store.offered.value.map { it.id })

        // Another device's choice comes over.
        runBlocking { store.follow("aurora") }
        assertEquals("aurora", style.personaId.value)
        assertEquals("aurora", store.current.value?.id)

        var dark by mutableStateOf(false)
        var run by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = AppStyle.Glass) {
                key(run) {
                    val pack by store.current.collectAsState()
                    val face by style.face.collectAsState()
                    CompositionLocalProvider(
                        LocalAppStyleStore provides style, LocalPersonaStore provides store,
                        LocalPersonaPack provides pack, LocalPersonaDesign provides face,
                    ) {
                        Box(Modifier.testTag("shot").width(400.dp).height(1500.dp).background(PersonaTheme.palette.background)
                            .padding(Tokens.Space.l)) {
                            PersonaSettings(demo = false)
                        }
                    }
                }
            }
        }
        for (look in listOf("aurora", "pebble", "persona")) {
            val design = PersonaDesign.entries.firstOrNull { it.id == look }
            if (design != null) store.choose(design) else runBlocking { store.choose(store.offered.value.first { it.id == look }) }
            for (d in listOf(false, true)) {
                dark = d; run++
                capture("looks-$look-${if (d) "dark" else "light"}")
            }
        }
        // Choosing here goes to the hub, for every device.
        rule.waitUntil(5_000) { hub.current.id == "persona" }
    }

    @Test fun retiredPreferencesAndHubChoicesFallBackWithoutBlockingExtensions() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val preferences = context.getSharedPreferences("persona-appearance", 0)
        PersonaManifest.RETIRED_IDS.forEach { id ->
            preferences.edit().clear().putString("persona", id).commit()
            val style = AppStyleStore(context)
            assertEquals("persona", style.personaId.value)
            assertEquals(PersonaDesign.Default, style.face.value)
            val hub = FakeHub(LookCurrent(), emptyList())
            val store = PersonaStore(context, style) { hub }
            runBlocking { store.refresh() }
            assertEquals(listOf("persona" to true), hub.chosen)
            style.setPersonaId("custom-look")
            runBlocking { store.follow(id) }
            assertEquals("persona", style.personaId.value)
            assertNull(store.current.value)
            assertEquals("persona", AppStyleStore(context).personaId.value)
        }
    }

    @Test fun unknownHubChoiceStopsShowingThePreviousPackageAndKeepsItsIntent() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        context.getSharedPreferences("persona-appearance", 0).edit().clear().commit()
        val style = AppStyleStore(context)
        val hub = FakeHub(LookCurrent("pebble", "pebble"))
        val store = PersonaStore(context, style) { hub }
        runBlocking { store.refresh() }
        assertEquals("pebble", store.current.value?.id)
        hub.current = LookCurrent("future-look", "persona")
        runBlocking { store.follow("future-look") }
        assertNull(store.current.value)
        assertEquals(PersonaDesign.Default, style.face.value)
        assertEquals("future-look", style.personaId.value)
        assertEquals("future-look", AppStyleStore(context).personaId.value)
    }

    @Test fun drawsOnlyTheTwoBundledLooksInEveryStyleAndAppearance() {
        val frog = generateSequence(File("").absoluteFile) { it.parentFile }
            .map { File(it, "server/internal/hub/personas/nwverse") }.first { it.isDirectory }
        android.graphics.BitmapFactory.decodeFile(File(frog, "tiers/frog/avatar/256.png").path)
        val manifest = JsonCodec.decodeFromString<PersonaManifest>(File(frog, "tiers/frog/persona.json").readText())
        val hub = FakeHub(LookCurrent("persona", "persona"), listOf(
            LookView("persona", "builtin"),
            LookView("nwverse", "hub", rev = "0123456789abcdef", manifest = manifest),
        ), frog)
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        context.getSharedPreferences("persona-appearance", 0).edit().clear().commit()
        val appearance = AppStyleStore(context)
        val store = PersonaStore(context, appearance) { hub }
        runBlocking { store.refresh() }
        assertEquals(listOf("nwverse"), store.offered.value.map { it.id })
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                val pack by store.current.collectAsState()
                CompositionLocalProvider(LocalAppStyleStore provides appearance, LocalPersonaStore provides store,
                    LocalPersonaPack provides pack) {
                    Box(Modifier.testTag("shot").width(400.dp).height(760.dp)
                        .background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                        PersonaSettings(demo = false)
                    }
                }
            }
        }
        rule.waitUntil(5_000) { store.pictures.value > 0 }
        rule.onAllNodes(hasClickAction()).assertCountEquals(2)
        rule.onNodeWithText("团子").assertIsSelected()
        rule.onNode(hasText(manifest.name) and hasClickAction()).performClick()
        rule.waitUntil(5_000) { store.current.value?.id == "nwverse" }
        rule.onNode(hasText(manifest.name) and hasClickAction()).assertIsSelected()
        rule.onNodeWithText("团子").performClick()
        rule.waitUntil(5_000) { appearance.personaId.value == "persona" && hub.current.id == "persona" }
        val shots = mutableListOf<ByteArray>()
        for (s in AppStyle.entries) {
            style = s
            for (d in listOf(false, true)) {
                dark = d
                Snapshot.sendApplyNotifications()
                capture("bundled-${s.key}-${if (d) "dark" else "light"}")
                shots += File(out(), "bundled-${s.key}-${if (d) "dark" else "light"}.png").readBytes()
            }
            assertNotEquals(shots[shots.lastIndex - 1].contentHashCode(), shots.last().contentHashCode())
        }
        appearance.setPersonaId("unknown-look")
        Snapshot.sendApplyNotifications()
        rule.onNodeWithText("团子").assertIsSelected()
    }
}
