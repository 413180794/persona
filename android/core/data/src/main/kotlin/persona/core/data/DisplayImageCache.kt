package persona.core.data

import android.graphics.Bitmap
import android.graphics.ImageDecoder
import persona.core.contracts.SessionKey
import java.io.ByteArrayOutputStream
import java.io.File
import java.nio.ByteBuffer
import kotlin.math.min
import kotlin.math.roundToInt

internal fun displayImageSize(width: Int, height: Int, targetWidth: Int, targetHeight: Int): Pair<Int, Int> {
    require(width > 0 && height > 0 && targetWidth > 0 && targetHeight > 0)
    val scale = min(1.0, min(targetWidth.toDouble() / width, targetHeight.toDouble() / height))
    return (width * scale).roundToInt().coerceAtLeast(1) to (height * scale).roundToInt().coerceAtLeast(1)
}

/** Stores only display-sized PNGs. ImageDecoder applies orientation before target sizing. */
class DisplayImageCache internal constructor(
    directory: File,
    limit: Long,
    private val activeSessions: () -> Set<SessionKey>,
) {
    private val files = BudgetFileCache(directory, limit) { file ->
        activeSessions().any { file.name.startsWith(cacheDigest(it.path) + "-") }
    }

    internal fun read(session: SessionKey, itemId: String, width: Int, height: Int): File? =
        files.read(name(session, itemId, width, height))

    internal fun put(session: SessionKey, itemId: String, width: Int, height: Int, bytes: ByteArray): File {
        val bitmap = ImageDecoder.decodeBitmap(ImageDecoder.createSource(ByteBuffer.wrap(bytes))) { decoder, info, _ ->
            decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
            val size = displayImageSize(info.size.width, info.size.height, width, height)
            decoder.setTargetSize(size.first, size.second)
        }
        val encoded = try {
            ByteArrayOutputStream().use { output ->
                check(bitmap.compress(Bitmap.CompressFormat.PNG, 100, output))
                output.toByteArray()
            }
        } finally { bitmap.recycle() }
        return files.put(name(session, itemId, width, height), encoded)
    }

    internal fun trim() = files.trim()
    private fun name(session: SessionKey, itemId: String, width: Int, height: Int): String {
        require(width > 0 && height > 0)
        return "${cacheDigest(session.path)}-${cacheDigest(itemId)}-${width}x$height.png"
    }
}
