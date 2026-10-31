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
 * iOS app's, or a phone switching changes what Claude sees. The fixture is read from apple/Jarvis/PhoneTools.
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

class PhotosTest {
    @Test fun idsNameTheMediaStoreRow() {
        assertEquals("image" to 42L, jarvis.android.phone.Photos.parseId("image:42"))
        assertEquals("video" to 7L, jarvis.android.phone.Photos.parseId("video:7"))
        assertEquals(null, jarvis.android.phone.Photos.parseId("42"))
        assertEquals(null, jarvis.android.phone.Photos.parseId("audio:1"))
        assertEquals(null, jarvis.android.phone.Photos.parseId("image:x"))
    }

    @Test fun screenshotsAreTheScreenshotsFolder() {
        assertTrue(jarvis.android.phone.Photos.isScreenshot("Pictures/Screenshots/"))
        assertTrue(jarvis.android.phone.Photos.isScreenshot("DCIM/Screenshots/"))
        assertFalse(jarvis.android.phone.Photos.isScreenshot("DCIM/Camera/"))
        assertFalse(jarvis.android.phone.Photos.isScreenshot(null))
    }

    @Test fun photosAreReadOnlyAndDeclaredTogether() {
        val names = jarvis.android.phone.PhoneTools.specs(setOf(jarvis.android.phone.Source.Photos)).map { it.jsonObject["name"]!!.jsonPrimitive.content }
        assertEquals(listOf("phone_photos_recent", "phone_photo_get"), names)
        assertTrue(jarvis.android.phone.PhoneTools.all.filter { it.source == jarvis.android.phone.Source.Photos }.all { it.confirm == null })
    }
}
