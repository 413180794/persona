package persona.platform.phonetools

import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Test

class PhotoContentTest {
    @Test fun imageBlocksUseTheHubsMimeTypeKey() {
        val block = jpegContent(byteArrayOf(1, 2, 3))
        // server/internal/proto Content: type, data (base64), mime_type. A "mimeType" key reaches Claude without a type.
        assertEquals(setOf("type", "data", "mime_type"), block.keys)
        assertEquals("image", block.getValue("type").jsonPrimitive.content)
        assertEquals("image/jpeg", block.getValue("mime_type").jsonPrimitive.content)
        assertEquals("AQID", block.getValue("data").jsonPrimitive.content)
    }
}
