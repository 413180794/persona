package persona.core.designsystem

import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import kotlin.math.abs
import kotlin.math.max

/**
 * The pure parts of showing and marking up pictures (the image viewer and editor, saving and sharing): no Android
 * classes, unit-tested on the JVM.
 */
object ImageMath {
    /** The viewer's decode cap never goes above this: Canvas cannot draw bigger bitmaps on every phone. */
    const val MaxDecodeSide = 4096
    private const val MinDecodeSide = 1024

    /** Where a picture of [image] size sits in a [box], fitted (scaled up or down) and centered: ContentScale.Fit. */
    fun fitRect(image: Size, box: Size): Rect {
        if (image.width <= 0f || image.height <= 0f) return Rect.Zero
        val scale = minOf(box.width / image.width, box.height / image.height)
        val width = image.width * scale
        val height = image.height * scale
        return Rect(Offset((box.width - width) / 2, (box.height - height) / 2), Size(width, height))
    }

    /** A point on screen to picture pixels ([fit] is where the picture is drawn), clamped to the picture. */
    fun toImage(point: Offset, fit: Rect, image: Size): Offset {
        if (fit.width <= 0f) return Offset.Zero
        val scale = image.width / fit.width
        return Offset(
            ((point.x - fit.left) * scale).coerceIn(0f, image.width),
            ((point.y - fit.top) * scale).coerceIn(0f, image.height),
        )
    }

    /**
     * A stroke of [screen] pixels on screen in picture pixels: what the marked-up picture gets, so a mark keeps the
     * width it had on screen however big the picture is. Never thinner than a pixel.
     */
    fun strokeInImage(screen: Float, fit: Rect, image: Size): Float =
        if (fit.width <= 0f) max(1f, screen) else max(1f, screen * image.width / fit.width)

    /**
     * The power-of-two decode sample for a [width] × [height] picture: the largest one that still keeps the long side at
     * least [maxSide] (enough pixels for a result of that size), or with [atMost] the smallest one whose long side fits
     * in [maxSide] (the bitmap never exceeds it). Never upscales.
     */
    fun sampleFor(width: Int, height: Int, maxSide: Int, atMost: Boolean): Int {
        val side = maxOf(width, height)
        val limit = maxSide.coerceAtLeast(1)
        var sample = 1
        if (atMost) while (side / sample > limit) sample *= 2
        else while (side / (sample * 2) >= limit) sample *= 2
        return sample
    }

    /** [width] × [height] shrunk so the long side is at most [maxSide]; never enlarged, never 0. */
    fun scaledSize(width: Int, height: Int, maxSide: Int): Pair<Int, Int> {
        val side = maxOf(width, height)
        if (side <= maxSide || side <= 0) return width to height
        val scale = maxSide.toDouble() / side
        return (width * scale).toInt().coerceAtLeast(1) to (height * scale).toInt().coerceAtLeast(1)
    }

    /**
     * How big the viewer decodes a picture for a screen whose long side is [screenLongSide] pixels: twice the screen, so
     * zooming in stays sharp for a while, within [MinDecodeSide] and [MaxDecodeSide].
     */
    fun viewerSide(screenLongSide: Int): Int = (screenLongSide * 2).coerceIn(MinDecodeSide, MaxDecodeSide)

    /**
     * The pan after zooming from [scale] to [target] so the point under [focus] (relative to the box centre, where the
     * zoom is anchored) stays where it is.
     */
    fun zoomPan(pan: Offset, scale: Float, target: Float, focus: Offset): Offset {
        if (scale <= 0f) return pan
        val ratio = target / scale
        return focus * (1f - ratio) + pan * ratio
    }

    /**
     * [pan] kept so a picture [fitted] in a [box] and zoomed to [scale] never moves an edge into view while it is
     * bigger than the box that way; where it is smaller it stays centred.
     */
    fun clampPan(pan: Offset, scale: Float, fitted: Size, box: Size): Offset {
        val maxX = max(0f, (fitted.width * scale - box.width) / 2f)
        val maxY = max(0f, (fitted.height * scale - box.height) / 2f)
        return Offset(pan.x.within(maxX), pan.y.within(maxY))
    }

    /** Clamped to ±[limit]; a plain 0 when there is no room (coercing into -0..0 would give -0). */
    private fun Float.within(limit: Float): Float = if (limit <= 0f) 0f else coerceIn(-limit, limit)

    /** How opaque the viewer's background is while a picture is pulled [pull] pixels away; [closeAt] closes it. */
    fun pullFade(pull: Float, closeAt: Float): Float =
        if (closeAt <= 0f) 1f else (1f - abs(pull) / (closeAt * 3f)).coerceIn(0.3f, 1f)

    /**
     * The picture type from its first bytes: PNG, GIF, WebP, JPEG, HEIC (and AVIF, which shares HEIC's container);
     * [fallback] when it is none of those but an image type, else JPEG.
     */
    fun sniffMime(bytes: ByteArray, fallback: String? = null): String = when {
        bytes.size > 3 && bytes[0] == 0x89.toByte() && bytes.ascii(1, "PNG") -> "image/png"
        bytes.size > 3 && bytes.ascii(0, "GIF8") -> "image/gif"
        bytes.size >= 12 && bytes.ascii(0, "RIFF") && bytes.ascii(8, "WEBP") -> "image/webp"
        bytes.size > 2 && bytes[0] == 0xFF.toByte() && bytes[1] == 0xD8.toByte() -> "image/jpeg"
        else -> isoMedia(bytes) ?: fallback?.lowercase()?.takeIf { it.startsWith("image/") } ?: "image/jpeg"
    }

    /** The file extension for a picture type ([sniffMime]); JPEG for anything else. */
    fun extension(mime: String): String = when (mime.lowercase()) {
        "image/png" -> "png"
        "image/gif" -> "gif"
        "image/webp" -> "webp"
        "image/heic", "image/heif" -> "heic"
        "image/avif" -> "avif"
        else -> "jpg"
    }

    private val heicBrands = setOf("heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs")
    private val avifBrands = setOf("avif", "avis")
    private val heifBrands = setOf("mif1", "msf1")

    /** HEIC / AVIF: an ISO media `ftyp` box whose brands say which. */
    private fun isoMedia(bytes: ByteArray): String? {
        if (bytes.size < 12 || !bytes.ascii(4, "ftyp")) return null
        val major = bytes.text(8)
        if (major in heicBrands) return "image/heic"
        if (major in avifBrands) return "image/avif"
        if (major !in heifBrands) return null
        // The generic HEIF brand: the compatible brands after the minor version say HEIC or AVIF.
        val boxSize = ((bytes[0].toInt() and 0xFF) shl 24) or ((bytes[1].toInt() and 0xFF) shl 16) or
            ((bytes[2].toInt() and 0xFF) shl 8) or (bytes[3].toInt() and 0xFF)
        val end = minOf(bytes.size, if (boxSize >= 16) boxSize else bytes.size)
        val compatible = (16 until end - 3 step 4).map { bytes.text(it) }
        return if (compatible.any { it in avifBrands } && compatible.none { it in heicBrands }) "image/avif" else "image/heic"
    }

    private fun ByteArray.ascii(at: Int, text: String): Boolean =
        at + text.length <= size && text.indices.all { this[at + it] == text[it].code.toByte() }

    private fun ByteArray.text(at: Int): String =
        if (at + 4 > size) "" else String(CharArray(4) { (this[at + it].toInt() and 0xFF).toChar() })
}
