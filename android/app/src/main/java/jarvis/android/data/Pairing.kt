package jarvis.android.data

import android.content.Context
import android.net.Uri

/** The paired hub: its address and this device's token (docs/protocol.md「鉴权」). Kept in app-private storage. */
data class Pairing(val url: String, val token: String)

class PairingStore(context: Context) {
    private val prefs = context.getSharedPreferences("pairing", Context.MODE_PRIVATE)

    fun load(): Pairing? {
        val url = prefs.getString("url", null) ?: return null
        val token = prefs.getString("token", null) ?: return null
        return Pairing(url, token)
    }

    fun save(p: Pairing) = prefs.edit().putString("url", p.url).putString("token", p.token).apply()
    fun clear() = prefs.edit().clear().apply()

    companion object {
        /** `jarvis://pair?url=…&token=…`, what `jarvisd hub pair` prints as a QR code. */
        fun parse(text: String): Pairing? {
            val uri = runCatching { Uri.parse(text.trim()) }.getOrNull() ?: return null
            if (uri.scheme != "jarvis" || uri.host != "pair") return null
            val url = uri.getQueryParameter("url")?.trim().orEmpty()
            val token = uri.getQueryParameter("token")?.trim().orEmpty()
            if (!url.startsWith("http") || token.isEmpty()) return null
            return Pairing(url, token)
        }
    }
}
