package persona.core.contracts.generated

import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.*
import org.junit.Test

class ExtensionCustomContributesTest {
    private val fixture = File("../../../schema/extension/examples/Manifest.custom-contributions.json")

    @Test fun customContributionsRoundTripWithStrictAndLenientCodecs() {
        for (ignoreUnknown in listOf(false, true)) {
            val codec = Json { ignoreUnknownKeys = ignoreUnknown; explicitNulls = false }
            val raw = codec.parseToJsonElement(fixture.readText()) as JsonObject
            val manifest = codec.decodeFromJsonElement(ExtensionManifest.serializer(), raw)
            val fields = requireNotNull(manifest.contributes)
            assertEquals("home", fields.ui?.single()?.id)
            assertEquals(8, fields.additionalProperties.size)
            assertTrue(fields.additionalProperties.containsKey("UI"))
            assertTrue(fields.additionalProperties.containsKey("__proto__"))
            assertTrue(fields.additionalProperties.containsKey("constructor"))
            assertTrue(fields.additionalProperties.containsKey("additionalProperties"))
            val back = codec.encodeToJsonElement(ExtensionManifest.serializer(), manifest)
            assertEquals("extra keys or nested values changed", raw, back)
        }
    }

    @Test fun extraMapCannotShadowPresentOrOmittedDeclaredFields() {
        val codec = Json { explicitNulls = false }
        val manifest = codec.decodeFromString(ExtensionManifest.serializer(), fixture.readText())
        val original = requireNotNull(manifest.contributes)
        val changed = original.copy(additionalProperties = original.additionalProperties + mapOf(
            "ui" to JsonPrimitive("not a list"),
            "providers" to JsonPrimitive("must not reintroduce a known field"),
        ))
        val encoded = codec.encodeToJsonElement(ExtensionContributes.serializer(), changed) as JsonObject
        assertFalse(encoded.containsKey("providers"))
        val back = codec.decodeFromJsonElement(ExtensionContributes.serializer(), encoded)
        assertEquals(original.ui, back.ui)
        assertNull(back.providers)
        assertEquals(original.additionalProperties, back.additionalProperties)
    }
}
