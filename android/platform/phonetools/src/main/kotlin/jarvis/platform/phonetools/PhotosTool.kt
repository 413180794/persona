package jarvis.platform.phonetools

import android.content.ContentUris
import android.content.Context
import android.graphics.Bitmap
import android.graphics.ImageDecoder
import android.os.CancellationSignal
import android.provider.MediaStore
import android.util.Base64
import android.util.Size
import jarvis.core.contracts.api.PhoneToolResult
import kotlinx.serialization.json.*
import java.io.ByteArrayOutputStream
import java.time.Instant

internal class PhotosTool(context: Context) {
    private val resolver = context.contentResolver
    private val collection = MediaStore.Files.getContentUri(MediaStore.VOLUME_EXTERNAL)
    fun recent(args: JsonObject): PhoneToolResult {
        val cutoff = Instant.now().minusSeconds(args.int("days", 30) * 86400L).epochSecond
        val selection = "(${MediaStore.Files.FileColumns.MEDIA_TYPE} = ? OR ${MediaStore.Files.FileColumns.MEDIA_TYPE} = ?) AND ${MediaStore.MediaColumns.DATE_ADDED} >= ?"
        val rows = buildJsonArray {
            var count = 0
            resolver.query(collection, arrayOf(MediaStore.MediaColumns._ID, MediaStore.Files.FileColumns.MEDIA_TYPE,
                MediaStore.MediaColumns.DATE_ADDED, MediaStore.MediaColumns.WIDTH, MediaStore.MediaColumns.HEIGHT,
                MediaStore.MediaColumns.RELATIVE_PATH, MediaStore.MediaColumns.IS_FAVORITE, MediaStore.Video.VideoColumns.DURATION),
                selection, arrayOf(MediaStore.Files.FileColumns.MEDIA_TYPE_IMAGE.toString(),
                    MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO.toString(), cutoff.toString()),
                "${MediaStore.MediaColumns.DATE_ADDED} DESC, ${MediaStore.MediaColumns._ID} DESC")?.use { cursor ->
                while (count < args.int("limit", 20) && cursor.moveToNext()) {
                    val path = cursor.getString(5).orEmpty()
                    val screenshot = path.contains("screenshot", true) || path.contains("截屏") || path.contains("截图")
                    if (args.bool("screenshots_only") && !screenshot) continue
                    val kind = if (cursor.getInt(1) == MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO) "video" else "image"
                    count++
                    add(buildJsonObject {
                        put("id", "$kind:${cursor.getLong(0)}"); put("type", kind)
                        put("created", iso(cursor.getLong(2) * 1000)); put("width", cursor.getInt(3)); put("height", cursor.getInt(4))
                        put("screenshot", screenshot); put("favorite", cursor.getInt(6) != 0)
                        if (kind == "video") put("duration_seconds", cursor.getLong(7) / 1000.0)
                    })
                }
            } ?: error("照片库暂不可用。")
        }
        return PhoneToolResult.text(rows.toString())
    }
    fun get(args: JsonObject): PhoneToolResult {
        val id = requireNotNull(args.string("id"))
        val match = Regex("(image|video):([0-9]+)").matchEntire(id)
        require(match != null) { "请使用最近照片返回的 id。" }
        val mediaId = match.groupValues[2].toLongOrNull() ?: throw IllegalArgumentException("照片 id 不正确。")
        val video = match.groupValues[1] == "video"
        val uri = ContentUris.withAppendedId(if (video) MediaStore.Video.Media.EXTERNAL_CONTENT_URI else MediaStore.Images.Media.EXTERNAL_CONTENT_URI, mediaId)
        val max = args.int("max_size", 1280)
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
                check(bitmap.compress(Bitmap.CompressFormat.JPEG, 70, output)) { "图片转换失败。" }; output.toByteArray()
            }
            return PhoneToolResult(buildJsonArray {
                add(buildJsonObject { put("type", "text"); put("text", buildJsonObject {
                    put("id", id); put("width", bitmap.width); put("height", bitmap.height); put("video_preview", video)
                }.toString()) })
                add(buildJsonObject { put("type", "image"); put("mimeType", "image/jpeg"); put("data", Base64.encodeToString(bytes, Base64.NO_WRAP)) })
            })
        } finally { if (bitmap !== decoded) bitmap.recycle(); decoded.recycle() }
    }
}
