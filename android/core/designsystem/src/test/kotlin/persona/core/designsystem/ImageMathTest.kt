package persona.core.designsystem

import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import org.junit.Assert.assertEquals
import org.junit.Test

class ImageMathTest {
    private fun bytes(vararg values: Int) = ByteArray(16).also { array -> values.forEachIndexed { i, v -> array[i] = v.toByte() } }

    @Test fun sniffsImageTypes() {
        assertEquals("image/png", ImageMath.sniffMime(bytes(0x89, 'P'.code, 'N'.code, 'G'.code)))
        assertEquals("image/gif", ImageMath.sniffMime(bytes('G'.code, 'I'.code, 'F'.code, '8'.code)))
        assertEquals("image/jpeg", ImageMath.sniffMime(bytes(0xFF, 0xD8, 0xFF)))
        assertEquals("image/webp", ImageMath.sniffMime("RIFF\u0000\u0000\u0000\u0000WEBPVP8 ".toByteArray()))
        // An iPhone photo: an ISO media box with the HEIC brand, or the generic HEIF brand listing heic.
        assertEquals("image/heic", ImageMath.sniffMime("\u0000\u0000\u0000\u0018ftypheic\u0000\u0000\u0000\u0000mif1heic".toByteArray()))
        assertEquals("image/heic", ImageMath.sniffMime("\u0000\u0000\u0000\u0018ftypmif1\u0000\u0000\u0000\u0000mif1heic".toByteArray()))
        assertEquals("image/avif", ImageMath.sniffMime("\u0000\u0000\u0000\u001cftypavif\u0000\u0000\u0000\u0000avifmif1miaf".toByteArray()))
        assertEquals("image/avif", ImageMath.sniffMime("\u0000\u0000\u0000\u0018ftypmif1\u0000\u0000\u0000\u0000mif1avif".toByteArray()))
        // A video's box is not a picture: the fallback decides.
        assertEquals("image/jpeg", ImageMath.sniffMime("\u0000\u0000\u0000\u0018ftypisom\u0000\u0000\u0002\u0000isomiso2".toByteArray()))
        assertEquals("image/heic", ImageMath.sniffMime(bytes(0, 0, 0), "image/heic"))
        assertEquals("image/jpeg", ImageMath.sniffMime(bytes(0, 0, 0), "application/pdf"))
        assertEquals("image/jpeg", ImageMath.sniffMime(ByteArray(0)))
        assertEquals("png", ImageMath.extension("image/PNG"))
        assertEquals("jpg", ImageMath.extension("image/jpeg"))
        assertEquals("heic", ImageMath.extension("image/heic"))
        assertEquals("webp", ImageMath.extension(ImageMath.sniffMime("RIFF\u0000\u0000\u0000\u0000WEBPVP8 ".toByteArray())))
        assertEquals("jpg", ImageMath.extension("application/octet-stream"))
    }

    @Test fun editorMapsScreenPointsToPixels() {
        // A 2000 × 1000 picture in a 400 × 400 box: 400 × 200, centered vertically.
        val fit = ImageMath.fitRect(Size(2000f, 1000f), Size(400f, 400f))
        assertEquals(Rect(0f, 100f, 400f, 300f), fit)
        assertEquals(Offset(1000f, 500f), ImageMath.toImage(Offset(200f, 200f), fit, Size(2000f, 1000f)))
        assertEquals(Offset(0f, 0f), ImageMath.toImage(Offset(-10f, 50f), fit, Size(2000f, 1000f))) // clamped
        assertEquals(Offset(2000f, 1000f), ImageMath.toImage(Offset(450f, 390f), fit, Size(2000f, 1000f))) // clamped
        assertEquals(Rect.Zero, ImageMath.fitRect(Size(0f, 10f), Size(400f, 400f)))
        // Small pictures are scaled up to the box, as ContentScale.Fit draws them.
        assertEquals(Rect(0f, 150f, 400f, 250f), ImageMath.fitRect(Size(200f, 50f), Size(400f, 400f)))
    }

    @Test fun marksKeepTheWidthTheyHadOnScreen() {
        val fit = ImageMath.fitRect(Size(2000f, 1000f), Size(400f, 400f))
        // The picture is drawn at a fifth of its size: 3 px on screen are 15 px in the picture.
        assertEquals(15f, ImageMath.strokeInImage(3f, fit, Size(2000f, 1000f)), 0.001f)
        // Drawn twice its size: never under a pixel.
        assertEquals(1f, ImageMath.strokeInImage(1f, ImageMath.fitRect(Size(200f, 50f), Size(400f, 400f)), Size(200f, 50f)), 0.001f)
        assertEquals(3f, ImageMath.strokeInImage(3f, Rect.Zero, Size(2000f, 1000f)), 0.001f)
    }

    @Test fun decodeSamplesAndOutputSizes() {
        // At least the asked side (a result of that size keeps its pixels).
        assertEquals(1, ImageMath.sampleFor(800, 600, 780, atMost = false))
        assertEquals(1, ImageMath.sampleFor(1500, 1000, 780, atMost = false)) // 750 would be below 780
        assertEquals(2, ImageMath.sampleFor(1600, 1000, 780, atMost = false))
        assertEquals(4, ImageMath.sampleFor(1000, 4000, 780, atMost = false))
        assertEquals(1, ImageMath.sampleFor(100, 100, 780, atMost = false)) // never upscaled
        // At most the cap (the viewer).
        assertEquals(1, ImageMath.sampleFor(4096, 3000, 4096, atMost = true))
        assertEquals(2, ImageMath.sampleFor(4097, 3000, 4096, atMost = true))
        assertEquals(4, ImageMath.sampleFor(12000, 9000, 4096, atMost = true))
        assertEquals(1, ImageMath.sampleFor(0, 0, 4096, atMost = true))
        // The edited photo: long side 1600, never enlarged, never 0.
        assertEquals(1600 to 1200, ImageMath.scaledSize(4000, 3000, 1600))
        assertEquals(900 to 1600, ImageMath.scaledSize(1800, 3200, 1600))
        assertEquals(800 to 600, ImageMath.scaledSize(800, 600, 1600))
        assertEquals(1600 to 1, ImageMath.scaledSize(16000, 2, 1600))
        // The viewer decodes at twice the screen, within 1024 and 4096.
        assertEquals(4096, ImageMath.viewerSide(2400))
        assertEquals(3000, ImageMath.viewerSide(1500))
        assertEquals(1024, ImageMath.viewerSide(300))
    }

    @Test fun zoomKeepsTheFingerAndPanningStaysOnThePicture() {
        // A double tap 100 px right of and 50 px below the centre, from 1× to 2.5×: that point stays under the finger.
        val pan = ImageMath.zoomPan(Offset.Zero, 1f, 2.5f, Offset(100f, 50f))
        assertNear(Offset(-150f, -75f), pan)
        // Zooming back to 1× from there brings the picture home.
        assertNear(Offset.Zero, ImageMath.zoomPan(pan, 2.5f, 1f, Offset(100f, 50f)))
        // A 400 × 200 picture in a 400 × 800 box at 2.5×: 1000 × 500, so it pans 300 sideways and not at all up or down.
        assertNear(Offset(-150f, 0f), ImageMath.clampPan(pan, 2.5f, Size(400f, 200f), Size(400f, 800f)))
        assertNear(Offset(300f, 0f), ImageMath.clampPan(Offset(900f, 40f), 2.5f, Size(400f, 200f), Size(400f, 800f)))
        assertEquals(Offset.Zero, ImageMath.clampPan(Offset(30f, -30f), 1f, Size(400f, 200f), Size(400f, 800f))) // a plain 0, not -0
    }

    private fun assertNear(expected: Offset, actual: Offset) {
        assertEquals(expected.x, actual.x, 0.01f)
        assertEquals(expected.y, actual.y, 0.01f)
    }

    @Test fun pullingFadesTheBackground() {
        assertEquals(1f, ImageMath.pullFade(0f, 300f), 0.001f)
        assertEquals(0.5f, ImageMath.pullFade(450f, 300f), 0.001f)
        assertEquals(0.5f, ImageMath.pullFade(-450f, 300f), 0.001f)
        assertEquals(0.3f, ImageMath.pullFade(5000f, 300f), 0.001f)
    }
}
