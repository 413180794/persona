package jarvis.android

import jarvis.android.data.JsonCodec
import jarvis.android.phone.PhoneTools
import jarvis.android.phone.Source
import jarvis.android.phone.parseTime
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

/**
 * The hub keeps one phone tool list, from whichever phone registered last: Android's must look exactly like the
 * iOS app's, or a phone switching changes what Claude sees. The fixture is read from ios/Jarvis/PhoneTools.
 */
class PhoneToolsTest {
    private val ios: JsonObject = JsonCodec.parseToJsonElement(javaClass.classLoader!!.getResource("ios-phone-tools.json").readText()).jsonObject

    @Test fun sameToolsAsIos() {
        assertEquals(ios.keys, PhoneTools.all.map { it.name }.toSet())
    }

    @Test fun sameParametersAsIos() {
        for (t in PhoneTools.all) {
            val want = ios[t.name]!!.jsonObject
            val props = t.schema["properties"]!!.jsonObject.keys.sorted()
            val required = (t.schema["required"] as? JsonArray)?.map { it.jsonPrimitive.content }?.sorted().orEmpty()
            assertEquals(t.name, want["properties"]!!.jsonArray.map { it.jsonPrimitive.content }, props)
            assertEquals(t.name, want["required"]!!.jsonArray.map { it.jsonPrimitive.content }, required)
            assertEquals(t.name, "object", t.schema["type"]!!.jsonPrimitive.content)
        }
    }

    @Test fun onlyEnabledSourcesAreDeclared() {
        assertTrue(PhoneTools.specs(emptySet()).isEmpty())
        val names = PhoneTools.specs(setOf(Source.Calendar)).map { it.jsonObject["name"]!!.jsonPrimitive.content }
        assertEquals(listOf("phone_calendar_events", "phone_calendar_create_event"), names)
        // The wire shape register_phone sends: name, description, input_schema.
        val one = PhoneTools.specs(setOf(Source.Device)).single().jsonObject
        assertEquals(setOf("name", "description", "input_schema"), one.keys)
    }

    @Test fun onlyWritesAskFirst() {
        assertEquals(listOf("phone_calendar_create_event"), PhoneTools.all.filter { it.confirm != null }.map { it.name })
    }

    @Test fun times() {
        assertEquals(Instant.parse("2026-10-24T06:00:00Z"), parseTime("2026-10-24T14:00:00+08:00"))
        assertEquals(Instant.parse("2026-10-24T06:00:00Z"), parseTime("2026-10-24T06:00:00Z"))
        assertFalse(runCatching { parseTime("tomorrow") }.isSuccess)
    }
}
