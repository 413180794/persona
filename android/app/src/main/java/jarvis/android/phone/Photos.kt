package jarvis.android.phone

import android.content.ContentUris
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.ImageDecoder
import android.net.Uri
import android.os.Build
import android.provider.MediaStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import java.io.ByteArrayOutputStream
import java.time.Instant
import kotlin.math.roundToInt

/**
 * phone_photos_recent / phone_photo_get on the gallery (MediaStore), as the iOS app's PhotosTool does on Photos.
 * Ids are "image:<id>" / "video:<id>", the MediaStore row; a screenshot is one in a Screenshots folder.
 */
internal object Photos {
    private const val JPEG_QUALITY = 70 // iOS 0.7

    suspend fun recent(c: Context, a: JsonObject): ToolOutput = withContext(Dispatchers.IO) {
        need(c, Source.Photos)
        val limit = a.i("limit", 20, 1, 100)
        val days = (a["days"] as? kotlinx.serialization.json.JsonPrimitive)?.content?.toIntOrNull()?.coerceIn(1, 3650)
        val screenshotsOnly = a.b("screenshots_only")
        val since = days?.let { Instant.now().minusSeconds(it * 86_400L) }
        val rows = (query(c, image = true, since, limit * 3) + if (screenshotsOnly) emptyList() else query(c, image = false, since, limit))
            .filter { !screenshotsOnly || it.screenshot }
            .sortedByDescending { it.taken }
            .take(limit)
        if (rows.isEmpty()) {
            return@withContext ToolOutput.json(buildJsonObject {
                put("count", 0)
                putJsonArray("assets") {}
                put("note", if (screenshotsOnly) "No screenshots matched. Try without screenshots_only." else "No photos matched. The user may have granted access to only some photos.")
            })
        }
        ToolOutput.json(buildJsonObject {
            put("count", rows.size)
            put("access", if (partialAccess(c)) "limited" else "full")
            putJsonArray("assets") { rows.forEach { r -> addJsonObject { encode(r) } } }
        })
    }

    suspend fun get(c: Context, a: JsonObject): ToolOutput = withContext(Dispatchers.IO) {
        need(c, Source.Photos)
        val id = a.s("id") ?: throw ToolFailure("id is required")
        val maxSize = a.i("max_size", 1280, 256, 2048)
        val (kind, row) = parseId(id) ?: throw ToolFailure("no photo with id \"$id\"; call phone_photos_recent for current ids")
        if (kind == "video") throw ToolFailure("这条是视频，只支持图片 / That asset is a video; only still images can be returned")
        val asset = query(c, image = true, null, 1, rowId = row).firstOrNull()
            ?: throw ToolFailure("no photo with id \"$id\"; call phone_photos_recent for current ids")
        val bitmap = decode(c, asset.uri, maxSize) ?: throw ToolFailure("无法解码这张照片 / Could not decode the photo data")
        val jpeg = ByteArrayOutputStream().use { out ->
            if (!bitmap.compress(Bitmap.CompressFormat.JPEG, JPEG_QUALITY, out)) throw ToolFailure("无法编码这张照片 / Could not encode the photo as JPEG")
            out.toByteArray()
        }
        ToolOutput.image(buildJsonObject {
            put("id", id)
            put("created", isoLocal(asset.taken))
            put("width", asset.width)
            put("height", asset.height)
            put("returned_width", bitmap.width)
            put("returned_height", bitmap.height)
            put("bytes", jpeg.size)
        }, jpeg)
    }

    internal data class Asset(
        val id: String, val uri: Uri, val taken: Instant, val video: Boolean, val width: Int, val height: Int,
        val favorite: Boolean, val screenshot: Boolean, val durationMs: Long?,
    )

    private fun kotlinx.serialization.json.JsonObjectBuilder.encode(r: Asset) {
        put("id", r.id)
        put("created", isoLocal(r.taken))
        put("type", if (r.video) "video" else "image")
        put("width", r.width)
        put("height", r.height)
        put("favorite", r.favorite)
        if (r.screenshot) putJsonArray("subtypes") { add(kotlinx.serialization.json.JsonPrimitive("screenshot")) }
        r.durationMs?.let { put("duration_seconds", (it / 100.0).roundToInt() / 10.0) }
        // Android strips a photo's location unless the app holds ACCESS_MEDIA_LOCATION, which this one doesn't ask for.
    }

    internal fun parseId(id: String): Pair<String, Long>? {
        val kind = id.substringBefore(':', "")
        val row = id.substringAfter(':', "").toLongOrNull() ?: return null
        return if (kind == "image" || kind == "video") kind to row else null
    }

    internal fun isScreenshot(path: String?): Boolean = path?.contains("screenshot", ignoreCase = true) == true

    private fun query(c: Context, image: Boolean, since: Instant?, limit: Int, rowId: Long? = null): List<Asset> {
        val base = if (image) MediaStore.Images.Media.EXTERNAL_CONTENT_URI else MediaStore.Video.Media.EXTERNAL_CONTENT_URI
        val cols = buildList {
            add(MediaStore.MediaColumns._ID); add(MediaStore.MediaColumns.DATE_TAKEN); add(MediaStore.MediaColumns.DATE_ADDED)
            add(MediaStore.MediaColumns.WIDTH); add(MediaStore.MediaColumns.HEIGHT)
            add(if (Build.VERSION.SDK_INT >= 29) MediaStore.MediaColumns.RELATIVE_PATH else MediaStore.MediaColumns.DATA)
            if (Build.VERSION.SDK_INT >= 30) add(MediaStore.MediaColumns.IS_FAVORITE)
            if (!image) add(MediaStore.Video.VideoColumns.DURATION)
        }.toTypedArray()
        val where = mutableListOf<String>(); val args = mutableListOf<String>()
        if (rowId != null) { where += "${MediaStore.MediaColumns._ID} = ?"; args += rowId.toString() }
        if (since != null) { where += "${MediaStore.MediaColumns.DATE_ADDED} >= ?"; args += (since.epochSecond).toString() }
        val out = mutableListOf<Asset>()
        c.contentResolver.query(base, cols, where.joinToString(" AND ").ifEmpty { null }, args.toTypedArray().ifEmpty { null },
            "${MediaStore.MediaColumns.DATE_ADDED} DESC")?.use { cur ->
            fun idx(n: String) = cur.getColumnIndex(n)
            while (cur.moveToNext() && out.size < limit) {
                val row = cur.getLong(idx(MediaStore.MediaColumns._ID))
                val takenMs = cur.getLong(idx(MediaStore.MediaColumns.DATE_TAKEN)).takeIf { it > 0 }
                val taken = takenMs?.let(Instant::ofEpochMilli) ?: Instant.ofEpochSecond(cur.getLong(idx(MediaStore.MediaColumns.DATE_ADDED)))
                val path = cur.getString(idx(cols[5]))
                out += Asset(
                    id = (if (image) "image:" else "video:") + row,
                    uri = ContentUris.withAppendedId(base, row),
                    taken = taken, video = !image,
                    width = cur.getInt(idx(MediaStore.MediaColumns.WIDTH)), height = cur.getInt(idx(MediaStore.MediaColumns.HEIGHT)),
                    favorite = Build.VERSION.SDK_INT >= 30 && cur.getInt(idx(MediaStore.MediaColumns.IS_FAVORITE)) == 1,
                    screenshot = image && isScreenshot(path),
                    durationMs = if (image) null else cur.getLong(idx(MediaStore.Video.VideoColumns.DURATION)),
                )
            }
        }
        return out
    }

    /** Partial access (Android 14 "selected photos"): only what the user picked is visible. */
    private fun partialAccess(c: Context): Boolean =
        Build.VERSION.SDK_INT >= 34 &&
            androidx.core.content.ContextCompat.checkSelfPermission(c, android.Manifest.permission.READ_MEDIA_IMAGES) != android.content.pm.PackageManager.PERMISSION_GRANTED

    /** Decoded at most maxSize on the long edge (subsampled while decoding, then scaled), upright. */
    private fun decode(c: Context, uri: Uri, maxSize: Int): Bitmap? = runCatching {
        if (Build.VERSION.SDK_INT >= 28) {
            ImageDecoder.decodeBitmap(ImageDecoder.createSource(c.contentResolver, uri)) { d, info, _ ->
                val long = maxOf(info.size.width, info.size.height)
                if (long > maxSize) {
                    val f = maxSize.toFloat() / long
                    d.setTargetSize((info.size.width * f).roundToInt().coerceAtLeast(1), (info.size.height * f).roundToInt().coerceAtLeast(1))
                }
                d.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
            }
        } else {
            val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            c.contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
            var sample = 1
            while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= maxSize) sample *= 2
            val b = c.contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample }) } ?: return@runCatching null
            val long = maxOf(b.width, b.height)
            if (long <= maxSize) b else Bitmap.createScaledBitmap(b, (b.width * maxSize / long).coerceAtLeast(1), (b.height * maxSize / long).coerceAtLeast(1), true)
        }
    }.getOrNull()
}
