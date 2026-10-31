package jarvis.android.media

import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import jarvis.android.data.SessionKey
import jarvis.android.ui.media.ImageMath
import jarvis.android.ui.media.MediaShare
import jarvis.android.ui.media.fitRect
import jarvis.android.ui.media.toImage
import jarvis.android.ui.media.ViewerImage
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ImageMathTest {
    @Test fun thumbnailSampleKeepsAtLeastTheAskedSide() {
        assertEquals(1, ImageMath.sampleFor(800, 600, 780, atMost = false))
        assertEquals(1, ImageMath.sampleFor(1500, 1000, 780, atMost = false)) // 750 would be below 780
        assertEquals(2, ImageMath.sampleFor(1600, 1000, 780, atMost = false))
        assertEquals(4, ImageMath.sampleFor(1000, 4000, 780, atMost = false))
        assertEquals(1, ImageMath.sampleFor(100, 100, 780, atMost = false)) // never upscaled
    }

    @Test fun fullSizeSampleFitsUnderTheCap() {
        assertEquals(1, ImageMath.sampleFor(4096, 3000, 4096, atMost = true))
        assertEquals(2, ImageMath.sampleFor(4097, 3000, 4096, atMost = true))
        assertEquals(4, ImageMath.sampleFor(12000, 9000, 4096, atMost = true))
    }

    @Test fun identityIsStablePerPicture() {
        val k = SessionKey("mac", "s1")
        assertEquals("item:mac/s1/i9", ImageMath.identity(ViewerImage(sessionKey = k, itemId = "i9", data = "AAAA")))
        assertEquals("url:/api/v1/lark/media/x", ImageMath.identity(ViewerImage(url = "/api/v1/lark/media/x")))
        val a = ImageMath.identity(ViewerImage(data = "AAAA"))
        assertEquals(a, ImageMath.identity(ViewerImage(data = "AAAA", mime = "image/png")))
        assertNotEquals(a, ImageMath.identity(ViewerImage(data = "AAAB")))
        assertTrue(a.startsWith("data:"))
    }

    @Test fun diskNamesDependOnTheHub() {
        val n = ImageMath.diskName("http://hub.example:1/", "item:a/b/c")
        assertEquals(n, ImageMath.diskName("http://hub.example:1", "item:a/b/c"))
        assertNotEquals(n, ImageMath.diskName("http://other.example:1", "item:a/b/c"))
        assertEquals(40, n.length)
    }

    @Test fun resolveTellsHubUrlsFromOthers() {
        val base = "http://hub.example:18660/"
        val rel = ImageMath.resolve(base, "/api/v1/lark/media/k%2Fv?w=1")!!
        assertTrue(rel.hub)
        assertEquals("http://hub.example:18660/api/v1/lark/media/k%2Fv?w=1", rel.url.toString())
        // A hub behind a path prefix keeps it.
        assertEquals("http://hub.example/jarvis/api/x", ImageMath.resolve("http://hub.example/jarvis", "/api/x")!!.url.toString())
        assertTrue(ImageMath.resolve(base, "http://hub.example:18660/api/x")!!.hub)
        assertFalse(ImageMath.resolve(base, "https://cdn.example/a.png")!!.hub)
        assertFalse(ImageMath.resolve(base, "http://hub.example:9/api/x")!!.hub)
        assertNull(ImageMath.resolve(base, "ftp://x/y"))
        assertNull(ImageMath.resolve(base, "//cdn.example/a.png"))
        assertNull(ImageMath.resolve("not a url", "/api/x"))
    }

    @Test fun fitKeepsAspectAndNeverEnlarges() {
        assertEquals(260f to 195f, ImageMath.fit(1170, 877, 260f, 320f, minSide = 48f)?.let { (w, h) -> w to Math.round(h).toFloat() })
        assertEquals(160f to 320f, ImageMath.fit(500, 1000, 260f, 320f))
        assertEquals(100f to 50f, ImageMath.fit(100, 50, 260f, 320f))
        assertEquals(260f to 48f, ImageMath.fit(3000, 10, 260f, 320f, minSide = 48f)) // a thin strip stays tappable
        assertNull(ImageMath.fit(0, 10, 260f, 320f))
    }

    @Test fun scaledSizeForThePayload() {
        assertEquals(1600 to 1200, ImageMath.scaledSize(4000, 3000, 1600))
        assertEquals(900 to 1600, ImageMath.scaledSize(1800, 3200, 1600))
        assertEquals(800 to 600, ImageMath.scaledSize(800, 600, 1600))
        assertEquals(1600 to 1, ImageMath.scaledSize(16000, 2, 1600))
    }

    @Test fun sniffsImageTypes() {
        fun b(vararg x: Int) = ByteArray(16).also { a -> x.forEachIndexed { i, v -> a[i] = v.toByte() } }
        assertEquals("image/png", MediaShare.sniffMime(b(0x89, 'P'.code, 'N'.code, 'G'.code)))
        assertEquals("image/gif", MediaShare.sniffMime(b('G'.code, 'I'.code, 'F'.code, '8'.code)))
        assertEquals("image/jpeg", MediaShare.sniffMime(b(0xFF, 0xD8, 0xFF)))
        val webp = "RIFF\u0000\u0000\u0000\u0000WEBPVP8 ".toByteArray()
        assertEquals("image/webp", MediaShare.sniffMime(webp))
        assertEquals("image/heic", MediaShare.sniffMime(b(0, 0, 0), "image/heic"))
        assertEquals("image/jpeg", MediaShare.sniffMime(b(0, 0, 0), "application/pdf"))
        assertEquals("png", MediaShare.extension("image/PNG"))
        assertEquals("jpg", MediaShare.extension("image/jpeg"))
    }

    @Test fun editorMapsScreenPointsToPixels() {
        // A 2000 × 1000 picture in a 400 × 400 box: 400 × 200, centered vertically.
        val fit = fitRect(Size(2000f, 1000f), Size(400f, 400f))
        assertEquals(Rect(0f, 100f, 400f, 300f), fit)
        assertEquals(Offset(1000f, 500f), toImage(Offset(200f, 200f), fit, Size(2000f, 1000f)))
        assertEquals(Offset(0f, 0f), toImage(Offset(-10f, 50f), fit, Size(2000f, 1000f))) // clamped
        assertEquals(Rect.Zero, fitRect(Size(0f, 10f), Size(400f, 400f)))
    }
}
