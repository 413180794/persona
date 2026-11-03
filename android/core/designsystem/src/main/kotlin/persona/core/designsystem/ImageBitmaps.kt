package persona.core.designsystem

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.ImageDecoder
import android.graphics.Paint
import android.graphics.Rect
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer

/**
 * Decodes a picture's encoded bytes (PNG, JPEG, GIF, WebP, HEIC…) with its EXIF orientation applied, sampled down by a
 * power of two ([ImageMath.sampleFor]): with [atMost] the long side ends up at most [maxSide], otherwise at least
 * [maxSide] (when the picture has that many pixels). [software] gives a mutable-friendly software bitmap (for drawing
 * into it or re-encoding it); otherwise the system may use a hardware bitmap, which only the screen can draw.
 * Blocking: call it off the main thread. Throws when the bytes are not a picture.
 */
fun decodePicture(bytes: ByteArray, maxSide: Int, atMost: Boolean = true, software: Boolean = false): Bitmap =
    ImageDecoder.decodeBitmap(ImageDecoder.createSource(ByteBuffer.wrap(bytes))) { decoder, info, _ ->
        decoder.setTargetSampleSize(ImageMath.sampleFor(info.size.width, info.size.height, maxSide, atMost))
        if (software) decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
    }

/**
 * This picture as JPEG bytes at [quality] (0–100), its long side shrunk to at most [maxSide]. Transparent parts are
 * laid on white first: JPEG has no alpha, and the encoder would otherwise turn them black. Blocking.
 */
fun Bitmap.jpegBytes(maxSide: Int, quality: Int): ByteArray {
    val (width, height) = ImageMath.scaledSize(this.width, this.height, maxSide)
    // A hardware bitmap cannot be drawn into a software canvas.
    val source = if (config == Bitmap.Config.HARDWARE) checkNotNull(copy(Bitmap.Config.ARGB_8888, false)) else this
    val flat = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
    try {
        Canvas(flat).apply {
            drawColor(android.graphics.Color.WHITE)
            drawBitmap(source, null, Rect(0, 0, width, height), Paint(Paint.FILTER_BITMAP_FLAG or Paint.ANTI_ALIAS_FLAG))
        }
        return ByteArrayOutputStream().use { out ->
            check(flat.compress(Bitmap.CompressFormat.JPEG, quality.coerceIn(0, 100), out)) { "JPEG encoding failed" }
            out.toByteArray()
        }
    } finally {
        flat.recycle()
        if (source !== this) source.recycle()
    }
}
