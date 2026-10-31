package jarvis.android.ui.media

import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.security.MessageDigest

/** The pure parts of loading and showing pictures (no Android classes: unit-tested on the JVM). */
object ImageMath {
    /**
     * The power-of-two decode sample for a [w] × [h] picture: the largest one that still keeps the long side ≥
     * [maxSide] (sharp thumbnails), or with [atMost] the smallest one whose long side fits in [maxSide].
     */
    fun sampleFor(w: Int, h: Int, maxSide: Int, atMost: Boolean): Int {
        val side = maxOf(w, h)
        val m = maxSide.coerceAtLeast(1)
        var sample = 1
        if (atMost) while (side / sample > m) sample *= 2
        else while (side / (sample * 2) >= m) sample *= 2
        return sample
    }

    /** What identifies a picture (the same across rows, thumbnails and the viewer); see [ChatImageLoader.identity]. */
    fun identity(img: ViewerImage): String = when {
        img.sessionKey != null && img.itemId != null -> "item:${img.sessionKey.machineId}/${img.sessionKey.sessionId}/${img.itemId}"
        img.url != null -> "url:${img.url}"
        else -> "data:" + sha1(img.data.orEmpty().take(4096) + "#" + img.data?.length)
    }

    /** Where a viewer URL points: [hub] when it is the hub's own (relative paths are), so it gets the token. */
    data class Target(val url: HttpUrl, val hub: Boolean)

    /** A hub-relative path ("/api/v1/…?x=1") or an absolute http(s) URL, against [baseUrl]; null when unusable. */
    fun resolve(baseUrl: String, url: String): Target? {
        val b = baseUrl.trimEnd('/')
        val base = b.toHttpUrlOrNull() ?: return null
        if (url.startsWith("/") && !url.startsWith("//")) return (b + url).toHttpUrlOrNull()?.let { Target(it, true) }
        val abs = url.toHttpUrlOrNull() ?: return null
        return Target(abs, abs.scheme == base.scheme && abs.host == base.host && abs.port == base.port)
    }

    /** The disk file name of [identity] on the hub at [baseUrl] (two hubs can have the same machine / session ids). */
    fun diskName(baseUrl: String, identity: String): String = sha1(baseUrl.trimEnd('/') + "\u0000" + identity)

    /**
     * The on-screen size of a [w] × [h] picture in a [maxW] × [maxH] box, aspect kept, never enlarged, at least
     * [minSide] on each side (a 1-px line stays tappable). Unknown sizes (≤ 0) give null.
     */
    fun fit(w: Int, h: Int, maxW: Float, maxH: Float, minSide: Float = 24f): Pair<Float, Float>? {
        if (w <= 0 || h <= 0) return null
        val s = minOf(1f, maxW / w, maxH / h)
        return (w * s).coerceIn(minSide, maxW) to (h * s).coerceIn(minSide, maxH)
    }

    /** [w] × [h] shrunk so the long side is at most [maxSide] (never enlarged, never 0). */
    fun scaledSize(w: Int, h: Int, maxSide: Int): Pair<Int, Int> {
        val side = maxOf(w, h)
        if (side <= maxSide || side <= 0) return w to h
        val s = maxSide.toDouble() / side
        return (w * s).toInt().coerceAtLeast(1) to (h * s).toInt().coerceAtLeast(1)
    }

    fun sha1(s: String): String =
        MessageDigest.getInstance("SHA-1").digest(s.toByteArray()).joinToString("") { "%02x".format(it) }
}
