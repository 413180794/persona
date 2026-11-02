package jarvis.android.ui.lark

import android.graphics.Bitmap
import android.util.Log
import android.util.LruCache
import jarvis.android.JarvisApp
import jarvis.android.data.ApiClient
import jarvis.android.lark.LarkEmoji
import jarvis.android.lark.LarkEmojiGroup
import jarvis.android.lark.larkDownload
import jarvis.android.lark.larkEmojis
import jarvis.android.lark.larkMediaBytes
import jarvis.android.lark.larkQuickReactions
import jarvis.android.ui.media.ChatImageLoader
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.io.File
import java.security.MessageDigest

/**
 * Feishu media through the hub (`/api/v1/lark/media/…`) or a CDN (iOS `LarkMedia`). A media key never changes its
 * bytes: decoded bitmaps stay in memory (an LRU by bytes), the raw bytes on disk (`cache/lark-media`, counted as 飞书
 * in 设置 › 存储空间). Fetching and decoding are off the main thread; one download per URL at a time. Also the emoji
 * catalogue and the user's quick reactions.
 */
object LarkMedia {
    private const val TAG = "LarkMedia"
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private val memory = object : LruCache<String, Bitmap>((Runtime.getRuntime().maxMemory() / 8).coerceIn(8L shl 20, 64L shl 20).toInt()) {
        override fun sizeOf(key: String, value: Bitmap) = value.allocationByteCount
    }
    private val lock = Mutex()
    private val inflight = HashMap<String, Deferred<Bitmap?>>()

    private val _emojis = MutableStateFlow<Map<String, LarkEmoji>>(emptyMap())
    /** Lower-cased key → label and picture URL (skin-tone variants too). Empty until [loadCatalog] has run. */
    val emojis: StateFlow<Map<String, LarkEmoji>> = _emojis.asStateFlow()
    private val _groups = MutableStateFlow<List<LarkEmojiGroup>>(emptyList())
    /** The picker's sections (默认表情…), keys in order. */
    val groups: StateFlow<List<LarkEmojiGroup>> = _groups.asStateFlow()
    private val _quick = MutableStateFlow(listOf("THUMBSUP", "OK", "DONE", "HEART", "SMILE", "LAUGH", "SURPRISED"))
    /** The user's quick reactions, most used first. */
    val quickReactions: StateFlow<List<String>> = _quick.asStateFlow()
    /** The hub whose catalogue is loaded (or loading): another hub (re-paired) loads its own. */
    @Volatile private var catalogOf: String? = null

    /** The disk folder; tests point it elsewhere. */
    @Volatile var diskDir: File? = null
    private fun dir(): File = diskDir ?: File(cacheRoot(), "lark-media")

    /** The app's cache folder; a temporary one where there is no app (screenshot tests). */
    private fun cacheRoot(): File = runCatching { JarvisApp.instance.cacheDir }.getOrNull() ?: File(System.getProperty("java.io.tmpdir"), "jarvis-cache")

    fun emoji(key: String): LarkEmoji? = _emojis.value[key.lowercase()]

    /** Loads the hub's emoji catalogue once, so reactions and inline emoji draw as pictures; a failure lets the next page try again. */
    fun loadCatalog(api: ApiClient) {
        synchronized(this) {
            if (catalogOf == api.baseUrl) return
            catalogOf = api.baseUrl
        }
        scope.launch {
            try {
                val r = api.larkEmojis()
                _emojis.value = r.emojis.mapKeys { it.key.lowercase() }
                _groups.value = r.groups.orEmpty()
                runCatching { api.larkQuickReactions() }.getOrNull()?.takeIf { it.isNotEmpty() }?.let { _quick.value = it }
            } catch (e: Exception) {
                Log.i(TAG, "emoji catalogue: ${e.message}")
                synchronized(this@LarkMedia) { if (catalogOf == api.baseUrl) catalogOf = null }
            }
        }
    }

    private fun hash(s: String): String =
        MessageDigest.getInstance("SHA-256").digest(s.toByteArray()).take(16).joinToString("") { "%02x".format(it) }

    private fun key(api: ApiClient, url: String, size: Int?) = hash(api.baseUrl + "\u0000" + url + "#" + (size ?: 0))

    /** The decoded picture if it is in memory already (a row coming back: no placeholder flash). */
    fun cached(api: ApiClient, url: String, size: Int?, maxPx: Int): Bitmap? = memory.get(key(api, url, size) + "@" + maxPx)

    /** Raw bytes, from disk when seen before. */
    suspend fun bytes(api: ApiClient, url: String, size: Int? = null): ByteArray = withContext(Dispatchers.IO) {
        val file = File(dir(), key(api, url, size))
        if (file.length() > 0) return@withContext file.readBytes()
        val data = api.larkMediaBytes(url, size)
        runCatching {
            dir().mkdirs()
            val tmp = File(file.parentFile, file.name + ".tmp")
            tmp.writeBytes(data)
            if (!tmp.renameTo(file)) tmp.delete()
        }
        data
    }

    /** The picture with its long side about [maxPx] (a hub thumbnail at [size] when it has one); null when it can't be had. */
    suspend fun image(api: ApiClient, url: String, size: Int? = null, maxPx: Int = 1600): Bitmap? {
        val k = key(api, url, size) + "@" + maxPx
        memory.get(k)?.let { return it }
        val job = lock.withLock {
            inflight[k] ?: scope.async {
                try {
                    ChatImageLoader.decode(bytes(api, url, size), maxPx)?.also { memory.put(k, it) }
                } catch (e: Exception) {
                    Log.i(TAG, "image: ${e.message}")
                    null
                } finally {
                    lock.withLock { inflight.remove(k) }
                }
            }.also { inflight[k] = it }
        }
        return job.await()
    }

    /** Waits until no picture is loading (screenshot tests draw once everything a page asked for is in memory). */
    internal suspend fun settle() {
        while (lock.withLock { inflight.isNotEmpty() }) kotlinx.coroutines.delay(SETTLE_POLL_MS)
    }

    private const val SETTLE_POLL_MS = 20L

    /** A local copy of a Feishu file named as it was sent (`cache/lark-files`), to hand to the app that opens it. */
    suspend fun file(api: ApiClient, url: String, name: String): File = withContext(Dispatchers.IO) {
        val folder = File(cacheRoot(), "lark-files/" + hash(url))
        val f = File(folder, name.replace('/', '_').ifBlank { "file" })
        if (f.length() > 0) return@withContext f
        folder.mkdirs()
        api.larkDownload(url, f)
    }
}
