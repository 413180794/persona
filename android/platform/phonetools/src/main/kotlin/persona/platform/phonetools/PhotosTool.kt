package persona.platform.phonetools

import android.Manifest.permission.READ_MEDIA_IMAGES
import android.content.ContentUris
import android.content.Context
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.ImageDecoder
import android.os.Build
import android.os.CancellationSignal
import android.provider.MediaStore
import android.util.Size
import persona.core.contracts.api.PhoneToolResult
import kotlinx.serialization.json.*
import java.io.ByteArrayOutputStream
import java.time.Instant
import kotlin.math.roundToInt

/**
 * phone_photos_recent / phone_photo_get on the gallery (MediaStore), answering as the iOS app's PhotosTool does. Ids are
 * `image:<row>` / `video:<row>`. Without ACCESS_MEDIA_LOCATION Android hides where a photo was taken: rows carry no
 * `location`.
 */
internal class PhotosTool(private val context: Context) {
    private val resolver = context.contentResolver
    private val collection = MediaStore.Files.getContentUri(MediaStore.VOLUME_EXTERNAL)

    fun recent(args: JsonObject): PhoneToolResult {
        val limit = args.int("limit", 20, 1, 100)
        val days = args.intOrNull("days", 1, 3650)
        val screenshotsOnly = args.bool("screenshots_only")
        val selection = StringBuilder("(${MediaStore.Files.FileColumns.MEDIA_TYPE} = ? OR ${MediaStore.Files.FileColumns.MEDIA_TYPE} = ?)")
        val values = mutableListOf(MediaStore.Files.FileColumns.MEDIA_TYPE_IMAGE.toString(), MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO.toString())
        days?.let {
            selection.append(" AND ${MediaStore.MediaColumns.DATE_ADDED} >= ?")
            values += Instant.now().minusSeconds(it * 86_400L).epochSecond.toString()
        }
        val rows = mutableListOf<JsonObject>()
        resolver.query(collection, arrayOf(MediaStore.MediaColumns._ID, MediaStore.Files.FileColumns.MEDIA_TYPE,
            MediaStore.MediaColumns.DATE_ADDED, MediaStore.MediaColumns.DATE_TAKEN, MediaStore.MediaColumns.WIDTH,
            MediaStore.MediaColumns.HEIGHT, MediaStore.MediaColumns.RELATIVE_PATH, MediaStore.MediaColumns.IS_FAVORITE,
            MediaStore.Video.VideoColumns.DURATION), selection.toString(), values.toTypedArray(),
            "${MediaStore.MediaColumns.DATE_ADDED} DESC, ${MediaStore.MediaColumns._ID} DESC")?.use { cursor ->
            while (rows.size < limit && cursor.moveToNext()) {
                val video = cursor.getInt(1) == MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO
                val screenshot = !video && isScreenshot(cursor.getString(6))
                if (screenshotsOnly && !screenshot) continue
                rows += buildJsonObject {
                    put("id", "${if (video) "video" else "image"}:${cursor.getLong(0)}")
                    put("created", iso(created(cursor.getLong(3), cursor.getLong(2))))
                    put("type", if (video) "video" else "image")
                    put("width", cursor.getInt(4)); put("height", cursor.getInt(5))
                    put("favorite", cursor.getInt(7) != 0)
                    if (screenshot) put("subtypes", buildJsonArray { add("screenshot") })
                    if (video) put("duration_seconds", (cursor.getLong(8) / 100.0).roundToInt() / 10.0)
                }
            }
        } ?: unavailable("照片库暂不可用 / The photo library is not available on this phone.")  // i18n-ignore: tool result for the model
        return PhoneToolResult.text(buildJsonObject {
            put("count", rows.size)
            if (rows.isNotEmpty()) put("access", if (limitedAccess()) "limited" else "full")
            put("assets", JsonArray(rows))
            if (rows.isEmpty()) put("note", if (screenshotsOnly) "No screenshots matched. Try without screenshots_only."
                else "No photos matched. The user may have granted access to only some photos.")
        }.toString())
    }

    fun get(args: JsonObject): PhoneToolResult {
        val id = args.text("id") ?: invalid("id is required")
        val max = args.int("max_size", 1280, 256, 2048)
        val (kind, row) = parsePhotoId(id) ?: invalid("no photo with id \"$id\"; call phone_photos_recent for current ids")
        val video = kind == "video"
        val uri = ContentUris.withAppendedId(if (video) MediaStore.Video.Media.EXTERNAL_CONTENT_URI else MediaStore.Images.Media.EXTERNAL_CONTENT_URI, row)
        var original: Triple<Instant, Int, Int>? = null
        resolver.query(uri, arrayOf(MediaStore.MediaColumns.DATE_ADDED, MediaStore.MediaColumns.DATE_TAKEN,
            MediaStore.MediaColumns.WIDTH, MediaStore.MediaColumns.HEIGHT), null, null, null)?.use { cursor ->
            if (cursor.moveToFirst()) original = Triple(created(cursor.getLong(1), cursor.getLong(0)), cursor.getInt(2), cursor.getInt(3))
        }
        val (taken, width, height) = original ?: invalid("no photo with id \"$id\"; call phone_photos_recent for current ids")
        // A video gives its preview frame (iOS answers videos with an error; a frame is more use to the model).
        val decoded = if (video) resolver.loadThumbnail(uri, Size(max, max), CancellationSignal()) else {
            ImageDecoder.decodeBitmap(ImageDecoder.createSource(resolver, uri)) { decoder, info, _ ->
                val ratio = minOf(1.0, max.toDouble() / maxOf(info.size.width, info.size.height))
                decoder.setTargetSize(maxOf(1, (info.size.width * ratio).toInt()), maxOf(1, (info.size.height * ratio).toInt()))
                decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
            }
        }
        val ratio = minOf(1.0, max.toDouble() / maxOf(decoded.width, decoded.height))
        val bitmap = if (ratio < 1) Bitmap.createScaledBitmap(decoded,
            maxOf(1, (decoded.width * ratio).toInt()), maxOf(1, (decoded.height * ratio).toInt()), true) else decoded
        try {
            val bytes = ByteArrayOutputStream().use { output ->
                if (!bitmap.compress(Bitmap.CompressFormat.JPEG, JPEG_QUALITY, output)) {
                    unavailable("无法编码这张照片 / Could not encode the photo as JPEG")  // i18n-ignore: tool result for the model
                }
                output.toByteArray()
            }
            return PhoneToolResult(buildJsonArray {
                add(buildJsonObject { put("type", "text"); put("text", buildJsonObject {
                    put("id", id); put("created", iso(taken))
                    put("width", if (width > 0) width else decoded.width); put("height", if (height > 0) height else decoded.height)
                    put("returned_width", bitmap.width); put("returned_height", bitmap.height); put("bytes", bytes.size)
                    if (video) put("video_preview", true)
                }.toString()) })
                add(jpegContent(bytes))
            })
        } finally { if (bitmap !== decoded) bitmap.recycle(); decoded.recycle() }
    }

    /** Android 14 lets the user share only some photos: then only READ_MEDIA_VISUAL_USER_SELECTED is held. */
    private fun limitedAccess(): Boolean = Build.VERSION.SDK_INT >= 34 &&
        context.checkSelfPermission(READ_MEDIA_IMAGES) != PackageManager.PERMISSION_GRANTED

    private companion object { const val JPEG_QUALITY = 70 } // iOS 0.7
}

/** When it was taken (DATE_TAKEN, milliseconds) or else added (DATE_ADDED, seconds). */
private fun created(takenMillis: Long, addedSeconds: Long): Instant =
    if (takenMillis > 0) Instant.ofEpochMilli(takenMillis) else Instant.ofEpochSecond(addedSeconds)

/** `image:<row>` / `video:<row>`; null for anything else. */
internal fun parsePhotoId(id: String): Pair<String, Long>? {
    val match = Regex("(image|video):([0-9]+)").matchEntire(id.trim()) ?: return null
    return match.groupValues[2].toLongOrNull()?.let { match.groupValues[1] to it }
}

/** A photo in a screenshots folder (Screenshots, or the Chinese-named screenshot folders some Chinese ROMs use). */
internal fun isScreenshot(path: String?): Boolean =
    path != null && (path.contains("screenshot", true) || path.contains("截屏") || path.contains("截图"))  // i18n-ignore: matches folder names of Chinese phones

/**
 * The image block of a tool result. The hub reads `mime_type` (proto.Content); with `mimeType` it forwarded the
 * picture to Claude without a media type, so Claude never saw it.
 */
internal fun jpegContent(bytes: ByteArray): JsonObject = buildJsonObject {
    put("type", "image"); put("data", java.util.Base64.getEncoder().encodeToString(bytes)); put("mime_type", "image/jpeg")
}
