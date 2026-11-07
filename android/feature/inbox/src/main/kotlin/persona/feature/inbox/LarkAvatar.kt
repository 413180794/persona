package persona.feature.inbox

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.platform.LocalContext
import coil3.Image
import coil3.SingletonImageLoader
import coil3.compose.asPainter
import coil3.memory.MemoryCache
import coil3.request.ImageRequest
import coil3.request.SuccessResult
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.ensureActive
import persona.core.contracts.api.ConnectionState
import persona.core.designsystem.JAvatar

/** The edge asked of the hub for a person's or chat's picture: avatars are drawn at most 48 dp, here at 3×. */
internal const val LarkAvatarPixels = 144

/**
 * [ref] at the hub's thumbnail size (the Lark gateway's steps of 24 px), as iOS asks for it; a URL not on the hub, or one that
 * already names a size, is left as it is.
 */
internal fun larkAvatarRef(ref: String): String =
    if (!ref.startsWith("/") || ref.startsWith("//") || Regex("[?&]size=").containsMatchIn(ref)) ref
    else ref + (if ('?' in ref) "&" else "?") + "size=$LarkAvatarPixels"

/** A person's or chat's picture at the thumbnail size, read with [read] (bound to one pairing), or null without one. */
fun larkAvatar(ref: String?, scope: String, read: suspend (String) -> ByteArray): AuthenticatedLarkImage? =
    ref?.takeIf { it.isNotBlank() }?.let(::larkAvatarRef)?.let { sized -> AuthenticatedLarkImage("$scope:$sized") { read(sized) } }

/**
 * One read per picture however many rows show it at once, finishing in [scope] even when the row that asked has
 * scrolled away (it is in the memory cache when the row comes back). A picture that failed is not asked for again for
 * [retryAfterMillis], so scrolling past it does not ask the hub each time; a failure from before the connection last
 * came up does not count (the read gave up waiting for a route), so reconnecting asks again at once.
 */
internal class LarkAvatarLoads<T : Any>(
    private val scope: CoroutineScope,
    private val retryAfterMillis: Long = 60_000,
    private val now: () -> Long = System::currentTimeMillis,
) {
    private val running = HashMap<String, Deferred<T?>>()
    private val failed = HashMap<String, Long>()

    /** The decoded picture, or null when there is none to have now; [onlineSince] is when the connection came up. */
    suspend fun get(image: AuthenticatedLarkImage, onlineSince: Long, decode: suspend (ByteArray) -> T?): T? {
        val key = image.cacheKey
        val load = synchronized(this) {
            failed[key]?.let { at -> if (at >= onlineSince && now() - at < retryAfterMillis) return null else failed.remove(key) }
            running[key] ?: scope.async(start = CoroutineStart.LAZY) {
                var result: T? = null
                try {
                    result = try { decode(image.read()) } catch (e: Exception) {
                        if (e is CancellationException) ensureActive()
                        null
                    }
                    result
                } finally {
                    synchronized(this@LarkAvatarLoads) {
                        running.remove(key)
                        if (result == null) failed[key] = now()
                    }
                }
            }.also { running[key] = it }
        }
        load.start()
        return load.await()
    }
}

private val avatarLoads = LarkAvatarLoads<Image>(CoroutineScope(SupervisorJob() + Dispatchers.IO))

/** When the hub connection last came up (the same clock as [LarkAvatarLoads]), or null while it is not up. */
@Composable
internal fun rememberOnlineSince(connection: ConnectionState): Long? = remember(connection == ConnectionState.Online) {
    if (connection == ConnectionState.Online) System.currentTimeMillis() else null
}

/**
 * [image] for [JAvatar]'s picture: null while it loads, when it fails and without one, so the initial stands in. A
 * picture Coil still holds shows on the first frame when its row comes back on screen, without asking again. Coil gets
 * the bytes, never the pairing's credentials. Nothing is asked while the hub is not connected ([onlineSince] null);
 * once it is, the rows on screen ask again.
 */
@Composable
internal fun rememberLarkAvatar(image: AuthenticatedLarkImage?, onlineSince: Long?): Painter? {
    val context = LocalContext.current.applicationContext
    val key = image?.cacheKey
    val picture = remember(key) {
        mutableStateOf(key?.let { SingletonImageLoader.get(context).memoryCache?.get(MemoryCache.Key(it))?.image?.asPainter(context) })
    }
    LaunchedEffect(key, onlineSince) {
        if (image == null || onlineSince == null || picture.value != null) return@LaunchedEffect
        picture.value = avatarLoads.get(image, onlineSince) { bytes ->
            val request = ImageRequest.Builder(context).data(bytes).memoryCacheKey(image.cacheKey).size(LarkAvatarPixels).build()
            (SingletonImageLoader.get(context).execute(request) as? SuccessResult)?.image
        }?.asPainter(context)
    }
    return picture.value
}

/** A Feishu person or chat: their picture once it has loaded, the initial of [name] until then and without one. */
@Composable
internal fun LarkAvatar(name: String, image: AuthenticatedLarkImage?, onlineSince: Long?, modifier: Modifier = Modifier) {
    JAvatar(name, modifier, image = rememberLarkAvatar(image, onlineSince))
}
