package persona.core.contracts.stream

import java.io.File
import org.junit.Assert.*
import org.junit.Test

class NativeStreamSchemaTest {
    private val examples = File("../../../schema/extension/examples")

    @Test fun canonicalStreamFixturesValidateBeforeProductionDecoding() {
        val valid = examples.listFiles()!!.filter { it.name.startsWith("UIStream") && it.extension == "json" }
        assertTrue(valid.size > 15)
        valid.forEach { NativeStreamSchema.parse(it.readText(), it.name.substringBefore('.')) }
        val invalid = File(examples, "invalid").listFiles()!!.filter { it.name.startsWith("UIStream") }
        assertTrue(invalid.size >= 16)
        invalid.forEach { assertTrue(it.name, runCatching { NativeStreamSchema.parse(it.readText(), it.name.substringBefore('.')) }.isFailure) }
    }

    @Test fun duplicateObjectMembersAreNotSilentlyNormalized() {
        val snapshot = File(examples, "UIStreamSnapshot.empty.json").readText()
        val duplicate = snapshot.replace("\"protocol\": 1", "\"protocol\": 1, \"protocol\": 1")
        assertNotEquals(snapshot, duplicate)
        assertTrue(runCatching { NativeStreamSchema.parse(duplicate, "UIStreamSnapshot") }.isFailure)
    }

    @Test fun nearLimitFractionAndNestedUnknownPayloadFailBeforeRoundingOrRecursion() {
        val snapshot = File(examples, "UIStreamSnapshot.empty.json").readText()
        val fraction = snapshot.replace("\"version\": 1", "\"version\": 9007199254740991.0000000000000000000001")
        assertTrue(runCatching { NativeStreamSchema.parse(fraction, "UIStreamSnapshot") }.isFailure)
        val deep = "[".repeat(17) + "0" + "]".repeat(17)
        assertTrue(runCatching { NativeStreamSchema.parse(deep, "UIStreamSnapshot") }.isFailure)
    }
}
