package persona.core.data

import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * Retains at most 16 MiB of encoded images in access-order LRU. Individual images above 4 MiB
 * are returned without caching. Limits are policy choices, unmeasured, not memory benchmarks;
 * decoded UI bitmaps and in-flight responses are owned by their callers.
 */
class ImageBytesCache {
    private val lock = Mutex()
    private val images = LinkedHashMap<String, ByteArray>(16, 0.75f, true)
    private var bytes = 0
    suspend fun get(key: String, fetch: suspend () -> ByteArray): ByteArray {
        lock.withLock { images[key]?.let { return it } }
        val value = fetch()
        if (value.size > 4 * 1024 * 1024) return value
        lock.withLock {
            images.put(key, value)?.let { bytes -= it.size }
            bytes += value.size
            while (bytes > 16 * 1024 * 1024) {
                val oldest = images.entries.iterator().next()
                bytes -= oldest.value.size
                images.remove(oldest.key)
            }
        }
        return value
    }
}
