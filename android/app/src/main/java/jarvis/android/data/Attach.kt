package jarvis.android.data

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.ImageDecoder
import android.net.Uri
import android.os.Build
import android.provider.OpenableColumns
import android.util.Base64
import java.io.ByteArrayOutputStream

/** Up to this many images per message, each scaled so its long side is at most MAX_SIDE (iOS ChatView `add`). */
const val MAX_IMAGES = 4
const val MAX_SIDE = 1600
/** The hub refuses bigger uploads (proto.MaxUploadBytes). */
const val MAX_UPLOAD_BYTES = 200L shl 20

/** A picked photo as the hub takes it: JPEG at 0.75 quality, scaled down; null when it can't be read. */
fun imagePayload(context: Context, uri: Uri): ImagePayload? = runCatching {
    val bmp: Bitmap = if (Build.VERSION.SDK_INT >= 28) {
        ImageDecoder.decodeBitmap(ImageDecoder.createSource(context.contentResolver, uri)) { d, info, _ ->
            val (w, h) = info.size.width to info.size.height
            val scale = scaleFor(w, h)
            if (scale < 1f) d.setTargetSize((w * scale).toInt().coerceAtLeast(1), (h * scale).toInt().coerceAtLeast(1))
            d.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
        }
    } else {
        val raw = context.contentResolver.openInputStream(uri)!!.use { BitmapFactory.decodeStream(it) }
        val s = scaleFor(raw.width, raw.height)
        if (s < 1f) Bitmap.createScaledBitmap(raw, (raw.width * s).toInt(), (raw.height * s).toInt(), true) else raw
    }
    val out = ByteArrayOutputStream()
    bmp.compress(Bitmap.CompressFormat.JPEG, 75, out)
    ImagePayload(mime = "image/jpeg", data = Base64.encodeToString(out.toByteArray(), Base64.NO_WRAP), width = bmp.width, height = bmp.height)
}.getOrNull()

/** How much to scale an image down so its long side fits MAX_SIDE (1 = leave it). */
fun scaleFor(w: Int, h: Int): Float = maxOf(w, h).let { if (it <= MAX_SIDE || it <= 0) 1f else MAX_SIDE.toFloat() / it }

/** A picked file's name and exact size (the upload needs a Content-Length). */
fun fileInfo(context: Context, uri: Uri): Pair<String, Long>? =
    context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { c ->
        if (!c.moveToFirst()) return@use null
        val name = c.getString(0)?.takeIf { it.isNotBlank() } ?: "file"
        val size = if (c.isNull(1)) -1L else c.getLong(1)
        if (size < 0) null else name to size
    }

/** A path with spaces or quotes is single-quoted, so the agent reads it as one path (iOS FileAttach.quoted). */
fun quotedPath(path: String): String =
    if (path.none { it.isWhitespace() || it == '\'' || it == '"' }) path else "'" + path.replace("'", "'\\''") + "'"

/** The composer text with `paths` added at the end, spaced from what's there (the app doesn't track the cursor). */
fun withPaths(text: String, paths: List<String>): String {
    val add = paths.joinToString(" ") { quotedPath(it) }
    return when {
        text.isEmpty() -> "$add "
        text.last().isWhitespace() -> "$text$add "
        else -> "$text $add "
    }
}
