package jarvis.android.ui.media

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.util.Base64
import android.util.LruCache
import jarvis.android.JarvisApp
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext
import okhttp3.Request
import java.io.File
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger

/**
 * Chat images on demand (iOS `ChatImageLoader`): pages carry only sizes (`lazy_images`); a row asks when it shows.
 * The same picture is downloaded once: decoded bitmaps in a memory LRU (per requested size), bytes on disk under
 * `cache/chat-images` (they never change; the folder is trimmed to [DISK_MAX_BYTES]). Inline base64 is decoded, not
 * stored. Fetching and decoding run off the main thread; [cached] is safe on it.
 */
object ChatImageLoader {
    private const val FULL_MAX_SIDE = 4096
    private const val DISK_MAX_BYTES = 256L * 1024 * 1024

    private val bitmaps = object : LruCache<String, Bitmap>(memoryBudget()) {
        override fun sizeOf(key: String, value: Bitmap) = value.byteCount
    }
    private val locks = ConcurrentHashMap<String, Mutex>()
    // Big pictures decoded at once can run a phone out of memory; four at a time is plenty for a scrolling list.
    private val decoding = Semaphore(4)
    private val writes = AtomicInteger()

    /** The disk cache folder; tests point it elsewhere. */
    @Volatile var diskDir: File? = null
    private fun dir(): File = (diskDir ?: File(JarvisApp.instance.cacheDir, "chat-images")).apply { mkdirs() }

    private fun memoryBudget(): Int =
        (Runtime.getRuntime().maxMemory() / 8).coerceIn(16L * 1024 * 1024, 64L * 1024 * 1024).toInt()

    /** What identifies the picture (the same across rows, thumbnails and the viewer). */
    fun identity(img: ViewerImage): String = ImageMath.identity(img)

    /** The memory-cache key of [img] decoded for [maxPx] (≤ 0 = full size). */
    fun memoryKey(img: ViewerImage, maxPx: Int): String = identity(img) + "@" + maxPx.coerceAtLeast(0)

    /** The decoded bitmap if it is in memory already (a row coming back on screen: no placeholder flash). */
    fun cached(img: ViewerImage, maxPx: Int): Bitmap? = bitmaps.get(memoryKey(img, maxPx))

    /**
     * The picture with its long side about [maxPx] pixels (between maxPx and 2 × maxPx, never upscaled; ≤ 0 = full
     * size, capped at 4096 so Canvas can draw it). Memory → disk → network; null when it can't be had.
     */
    suspend fun load(hub: Hub, img: ViewerImage, maxPx: Int): Bitmap? = withContext(Dispatchers.IO) {
        val key = memoryKey(img, maxPx)
        bitmaps.get(key)?.let { return@withContext it }
        val bytes = bytes(hub, img) ?: return@withContext null
        decoding.withPermit {
            bitmaps.get(key) ?: decode(bytes, if (maxPx > 0) maxPx else FULL_MAX_SIDE, atMost = maxPx <= 0)?.also { bitmaps.put(key, it) }
        }
    }

    /** The original bytes (save / share / copy / edit); null when they can't be had. */
    suspend fun bytes(hub: Hub, img: ViewerImage): ByteArray? = withContext(Dispatchers.IO) {
        img.data?.takeIf { it.isNotEmpty() }?.let { return@withContext runCatching { Base64.decode(it, Base64.DEFAULT) }.getOrNull() }
        val id = identity(img)
        val file = File(dir(), ImageMath.diskName(hub.api.baseUrl, id))
        locks.getOrPut(id) { Mutex() }.withLock {
            if (file.length() > 0) {
                file.setLastModified(System.currentTimeMillis()) // LRU order for the trim
                return@withLock runCatching { file.readBytes() }.getOrNull()
            }
            val fetched = runCatching { fetch(hub.api, img) }.getOrNull()?.takeIf { it.isNotEmpty() } ?: return@withLock null
            runCatching {
                val tmp = File(file.parentFile, file.name + ".tmp")
                tmp.writeBytes(fetched)
                tmp.renameTo(file)
            }
            if (writes.incrementAndGet() % 50 == 0) trimDisk()
            fetched
        }
    }

    private suspend fun fetch(api: ApiClient, img: ViewerImage): ByteArray? {
        val key = img.sessionKey
        val item = img.itemId
        if (key != null && item != null) return api.image(key, item)
        val url = img.url ?: return null
        val target = ImageMath.resolve(api.baseUrl, url) ?: return null
        // The hub's own URLs (Feishu media is rewritten to `/api/v1/lark/media/…`) carry the token; any other host
        // is fetched without credentials.
        val req = Request.Builder().url(target.url).apply { if (target.hub) header("Authorization", "Bearer ${api.token}") }.get().build()
        return api.http.newCall(req).execute().use { r -> if (r.isSuccessful) r.body?.bytes() else null }
    }

    /**
     * Decodes at a power-of-two sample: the largest one that still keeps the long side ≥ [maxSide] (thumbnails), or
     * with [atMost] the smallest one whose long side fits in [maxSide] (the bitmap never exceeds it).
     */
    fun decode(bytes: ByteArray, maxSide: Int, atMost: Boolean = false): Bitmap? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
        val sample = ImageMath.sampleFor(bounds.outWidth, bounds.outHeight, maxSide, atMost)
        return runCatching {
            BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = sample })
        }.getOrNull() // OutOfMemoryError is an Error, not caught: a picture that big fails the sample math first
    }

    /** Deletes the least recently used files until the folder is under [maxBytes]. */
    fun trimDisk(maxBytes: Long = DISK_MAX_BYTES) {
        val files = dir().listFiles()?.filter { it.isFile } ?: return
        var total = files.sumOf { it.length() }
        if (total <= maxBytes) return
        for (f in files.sortedBy { it.lastModified() }) {
            if (total <= maxBytes) break
            total -= f.length()
            f.delete()
        }
    }
}
