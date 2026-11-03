package persona.android.data

import android.content.Context
import persona.core.domain.Pairing

class PairingStore(context: Context) {
    private val prefs = context.getSharedPreferences("pairing", Context.MODE_PRIVATE)

    fun load(): Pairing? {
        val url = prefs.getString("url", null) ?: return null
        val token = prefs.getString("token", null) ?: return null
        return Pairing(url, token)
    }

    fun clear() { prefs.edit().clear().apply() }
}
