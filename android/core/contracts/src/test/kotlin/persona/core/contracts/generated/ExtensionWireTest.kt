package persona.core.contracts.generated

import java.io.File
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.doubleOrNull
import org.junit.Assert.*
import org.junit.Test

// schema/extension (#1849): the generated types read the schema's examples and write them back the same; a component
// this app does not know, or cannot read, is kept as Unknown and the rest of the view still decodes.
class ExtensionWireTest {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; explicitNulls = false }
    private val examples = File("../../../schema/extension/examples")

    private val types: Map<String, KSerializer<*>> = mapOf(
        "View" to ExtensionView.serializer(),
        "ActionResult" to ExtensionActionResult.serializer(),
        "UIParams" to ExtensionUIParams.serializer(),
        "UIPlaces" to ExtensionUIPlaces.serializer(),
        "InitializeParams" to ExtensionInitializeParams.serializer(),
        "InitializeResult" to ExtensionInitializeResult.serializer(),
        "Manifest" to ExtensionManifest.serializer(),
        "PersonaCard" to ExtensionPersonaCard.serializer(),
        "WidgetPlaces" to ExtensionWidgetPlaces.serializer(),
        "WidgetAnswer" to ExtensionWidgetAnswer.serializer(),
        "DeviceWidgets" to ExtensionDeviceWidgets.serializer(),
        "ShortcutPlaces" to ExtensionShortcutPlaces.serializer(),
        "ShortcutResult" to ExtensionShortcutResult.serializer(),
        "ExtensionNotification" to ExtensionNotification.serializer(),
        "AssistantView" to ExtensionAssistantView.serializer(),
        "ViewAction" to ExtensionViewAction.serializer(),
    )

    @Suppress("UNCHECKED_CAST")
    @Test fun examplesRoundTrip() {
        val files = examples.listFiles { f -> f.isFile && f.name.endsWith(".json") }.orEmpty()
        assertTrue("no examples in ${examples.absolutePath}", files.size > 10)
        for (f in files) {
            val serializer = types[f.name.substringBefore(".")] as KSerializer<Any?>? ?: fail("no type for ${f.name}") as Nothing
            val raw = json.parseToJsonElement(f.readText())
            val back = json.encodeToJsonElement(serializer, json.decodeFromJsonElement(serializer, raw))
            assertTrue("${f.name} round trip differs:\n$raw\n$back", same(raw, back))
        }
    }

    @Test fun assistantExamplesRoundTrip() {
        val files = File(examples, "assistant").listFiles { f -> f.name.endsWith(".json") }.orEmpty()
        assertTrue("assistant examples missing", files.size >= 7)
        for (f in files) {
            val raw = json.parseToJsonElement(f.readText())
            val view = json.decodeFromJsonElement(ExtensionView.serializer(), raw)
            assertFalse("${f.name} has no fallback", view.fallback.isNullOrBlank())
            val back = json.encodeToJsonElement(ExtensionView.serializer(), view)
            assertTrue("${f.name} round trip differs:\n$raw\n$back", same(raw, back))
        }
    }

    @Test fun unknownComponentsAreKept() {
        val view = json.decodeFromString(ExtensionView.serializer(), """{"sections":[{"items":[
            {"type":"hologram","depth":3},
            {"type":"metric","label":"今天","value":"1.2M","tone":"sparkly"},
            {"type":"chart","kind":"bar","series":"not a list"}]}]}""")
        val items = view.sections.single().items
        assertEquals(ExtensionComponent.Unknown("hologram", items[0].let { (it as ExtensionComponent.Unknown).raw }), items[0])
        assertEquals("sparkly", (items[1] as ExtensionMetricComponent).tone)
        assertTrue(items[2] is ExtensionComponent.Unknown)
        assertEquals("chart", items[2].type)
        // An unknown component writes back what it was.
        val out = json.encodeToJsonElement(ExtensionView.serializer(), view) as JsonObject
        assertTrue(out.toString().contains("hologram"))
    }

    /** JSON equality with numbers compared as numbers (8432 and 8432.0 are the same). */
    private fun same(a: JsonElement, b: JsonElement): Boolean = when {
        a is JsonObject && b is JsonObject -> a.keys == b.keys && a.keys.all { same(a[it]!!, b[it]!!) }
        a is JsonArray && b is JsonArray -> a.size == b.size && a.indices.all { same(a[it], b[it]) }
        a is JsonNull || b is JsonNull -> a == b
        a is JsonPrimitive && b is JsonPrimitive ->
            if (!a.isString && !b.isString && a.doubleOrNull != null) a.doubleOrNull == b.doubleOrNull else a == b
        else -> false
    }
}
