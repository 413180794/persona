package jarvis.android.ui.media

import android.Manifest
import android.content.ClipData
import android.content.ClipboardManager
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.media.MediaScannerConnection
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import android.util.Base64
import android.widget.Toast
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import jarvis.android.data.ImagePayload
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream
import java.io.File

/**
 * Saving, sharing and copying pictures and text (the viewer's 保存 / 分享 / 拷贝, the file viewer's menus), files
 * for the camera, and the JPEG payload a picture is sent as. Shared files go through the app's FileProvider
 * ([ChatFileProvider], `<package>.chatfiles`, `cache/shared/`, AndroidManifest.xml + res/xml/chat_file_paths.xml).
 */
object MediaShare {
    private const val SHARED_MAX_AGE_MS = 24 * 3600 * 1000L

    fun authority(ctx: Context) = ctx.packageName + ".chatfiles"

    private fun sharedDir(ctx: Context) = File(ctx.cacheDir, "shared").apply { mkdirs() }

    /** The image type from the first bytes (PNG, GIF, WebP, else JPEG); [fallback] when it is not a known image. */
    fun sniffMime(bytes: ByteArray, fallback: String? = null): String = when {
        bytes.size > 3 && bytes[0] == 0x89.toByte() && bytes[1] == 'P'.code.toByte() && bytes[2] == 'N'.code.toByte() -> "image/png"
        bytes.size > 3 && bytes[0] == 'G'.code.toByte() && bytes[1] == 'I'.code.toByte() && bytes[2] == 'F'.code.toByte() -> "image/gif"
        bytes.size > 11 && bytes[0] == 'R'.code.toByte() && bytes[8] == 'W'.code.toByte() && bytes[9] == 'E'.code.toByte() -> "image/webp"
        bytes.size > 2 && bytes[0] == 0xFF.toByte() && bytes[1] == 0xD8.toByte() -> "image/jpeg"
        else -> fallback?.takeIf { it.startsWith("image/") } ?: "image/jpeg"
    }

    fun extension(mime: String): String = when (mime.lowercase()) {
        "image/png" -> "png"
        "image/gif" -> "gif"
        "image/webp" -> "webp"
        "image/heic" -> "heic"
        else -> "jpg"
    }

    /** Android 8–9 write to the public Pictures folder with the storage permission; 10+ need none (MediaStore). */
    fun needsStoragePermission(ctx: Context): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.Q &&
            ContextCompat.checkSelfPermission(ctx, Manifest.permission.WRITE_EXTERNAL_STORAGE) != PackageManager.PERMISSION_GRANTED

    /**
     * Into Pictures/Jarvis. Android 10+: MediaStore, no permission. Android 8–9: a file in the public Pictures folder
     * plus a media scan; needs WRITE_EXTERNAL_STORAGE (ask first, see [needsStoragePermission]), false without it.
     */
    suspend fun saveImage(ctx: Context, bytes: ByteArray, mime: String): Boolean = withContext(Dispatchers.IO) {
        val type = sniffMime(bytes, mime)
        val name = "jarvis-${System.currentTimeMillis()}.${extension(type)}"
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            val values = ContentValues().apply {
                put(MediaStore.Images.Media.DISPLAY_NAME, name)
                put(MediaStore.Images.Media.MIME_TYPE, type)
                put(MediaStore.Images.Media.RELATIVE_PATH, Environment.DIRECTORY_PICTURES + "/Jarvis")
                put(MediaStore.Images.Media.IS_PENDING, 1)
            }
            val resolver = ctx.contentResolver
            val uri = runCatching { resolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values) }.getOrNull()
                ?: return@withContext false
            runCatching {
                resolver.openOutputStream(uri)?.use { it.write(bytes) } ?: error("no output stream")
                values.clear()
                values.put(MediaStore.Images.Media.IS_PENDING, 0)
                resolver.update(uri, values, null, null)
            }.onFailure { runCatching { resolver.delete(uri, null, null) } }.isSuccess
        } else {
            if (needsStoragePermission(ctx)) return@withContext false
            runCatching {
                @Suppress("DEPRECATION")
                val dir = File(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_PICTURES), "Jarvis").apply { mkdirs() }
                val f = File(dir, name)
                f.writeBytes(bytes)
                MediaScannerConnection.scanFile(ctx, arrayOf(f.absolutePath), arrayOf(type), null)
            }.isSuccess
        }
    }

    /** [bytes] as a file in `cache/shared` and its content URI (old shared files are dropped on the way). */
    suspend fun sharedUri(ctx: Context, bytes: ByteArray, ext: String): Uri = withContext(Dispatchers.IO) {
        val dir = sharedDir(ctx)
        val now = System.currentTimeMillis()
        dir.listFiles()?.forEach { if (now - it.lastModified() > SHARED_MAX_AGE_MS) it.delete() }
        val f = File(dir, "image-$now.$ext")
        f.writeBytes(bytes)
        FileProvider.getUriForFile(ctx, authority(ctx), f)
    }

    suspend fun shareImage(ctx: Context, bytes: ByteArray, mime: String) {
        val type = sniffMime(bytes, mime)
        val uri = sharedUri(ctx, bytes, extension(type))
        val send = Intent(Intent.ACTION_SEND).apply {
            this.type = type
            putExtra(Intent.EXTRA_STREAM, uri)
            clipData = ClipData.newRawUri("", uri)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        ctx.startActivity(Intent.createChooser(send, "分享图片").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }

    /** The picture on the clipboard (as a content URI: apps that paste images read it from there). */
    suspend fun copyImage(ctx: Context, bytes: ByteArray, mime: String) {
        val type = sniffMime(bytes, mime)
        val uri = sharedUri(ctx, bytes, extension(type))
        ctx.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newUri(ctx.contentResolver, "图片", uri))
    }

    fun shareText(ctx: Context, text: String) {
        val send = Intent(Intent.ACTION_SEND).apply { type = "text/plain"; putExtra(Intent.EXTRA_TEXT, text) }
        ctx.startActivity(Intent.createChooser(send, "分享").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }

    fun copyText(ctx: Context, text: String, label: String = "文本") {
        ctx.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText(label, text))
    }

    /** A picture for a message (as iOS: long side at most [maxSide] px, JPEG quality 75), as the hub's base64 payload. */
    fun jpegPayload(bmp: Bitmap, maxSide: Int = 1600): ImagePayload {
        val (w, h) = ImageMath.scaledSize(bmp.width, bmp.height, maxSide)
        val scaled = if (w != bmp.width || h != bmp.height) Bitmap.createScaledBitmap(bmp, w, h, true) else bmp
        val out = ByteArrayOutputStream()
        scaled.compress(Bitmap.CompressFormat.JPEG, 75, out)
        return ImagePayload("image/jpeg", Base64.encodeToString(out.toByteArray(), Base64.NO_WRAP), width = w, height = h)
    }

    /** A short message at the bottom (the app has no snackbar host on pushed screens). */
    fun toast(ctx: Context, text: String) = Toast.makeText(ctx.applicationContext, text, Toast.LENGTH_SHORT).show()
}
