package persona.core.hub

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * Pictures that are not on the hub (a bot's avatar is a full https URL on Feishu's CDN), read straight from where they
 * are, as iOS does. The client is a plain one of its own, never the hub's: no pairing token, no hub header, no cookies,
 * no pinned hub certificate, so neither the request nor a redirect it follows can carry the pairing's credentials.
 * Only https, never to http on a redirect, and at most [maxBytes].
 */
class PublicImageReader(
    private val client: OkHttpClient = defaultClient,
    private val maxBytes: Long = MaxBytes,
) {
    suspend fun read(url: String): ByteArray {
        val target = requireNotNull(publicImageUrl(url)) { "Invalid public image URL" }
        val request = Request.Builder().url(target).get().build()
        return client.newCall(request).awaitResponse(buffered = false).use { response ->
            if (!response.isSuccessful) throw IOException("Public image answered ${response.code}")
            val body = requireNotNull(response.body) { "Public image without a body" }
            if (body.contentLength() > maxBytes) throw IOException("Public image too large")
            withContext(Dispatchers.IO) {
                val source = body.source()
                // More than the cap is buffered only when the picture is larger than allowed.
                if (source.request(maxBytes + 1)) throw IOException("Public image too large")
                source.buffer.readByteArray()
            }
        }
    }

    companion object {
        const val MaxBytes: Long = 2L * 1024 * 1024

        /** Shared by every pairing: it holds nothing of any of them. */
        val shared by lazy { PublicImageReader() }

        private val defaultClient: OkHttpClient by lazy {
            OkHttpClient.Builder()
                .connectTimeout(10, TimeUnit.SECONDS)
                .readTimeout(15, TimeUnit.SECONDS)
                .callTimeout(30, TimeUnit.SECONDS)
                // An https picture may move to another https host, never down to http.
                .followSslRedirects(false)
                .build()
        }

        /** [url] when it is a full https URL without user info, else null (a hub path, http, another scheme). */
        fun publicImageUrl(url: String): HttpUrl? {
            if (!url.startsWith("https://", ignoreCase = true)) return null
            val parsed = url.toHttpUrlOrNull() ?: return null
            return parsed.takeIf { it.isHttps && it.username.isEmpty() && it.password.isEmpty() && it.host.isNotEmpty() }
        }
    }
}
