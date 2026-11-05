package persona.platform.capsule

import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.net.Uri
import android.provider.Settings
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow

class CapsulePreferences(context: Context) : AutoCloseable {
    private val preferences = context.applicationContext.getSharedPreferences("persona-capsule", Context.MODE_PRIVATE)
    private val current = MutableStateFlow(CapsuleMode.fromKey(preferences.getString("mode", null)))
    val mode = current.asStateFlow()
    private val listener = SharedPreferences.OnSharedPreferenceChangeListener { _, key ->
        if (key == "mode") current.value = CapsuleMode.fromKey(preferences.getString("mode", null))
    }
    init { preferences.registerOnSharedPreferenceChangeListener(listener) }
    fun setMode(mode: CapsuleMode) {
        current.value = mode
        preferences.edit().putString("mode", mode.key).apply()
    }
    internal var dock: CapsuleDock
        get() = CapsuleDock(preferences.getBoolean("right", true), preferences.getFloat("fraction", .15f))
        set(value) { preferences.edit().putBoolean("right", value.right).putFloat("fraction", value.fraction).apply() }
    override fun close() { preferences.unregisterOnSharedPreferenceChangeListener(listener) }
}

object CapsulePermission {
    fun granted(context: Context): Boolean = Settings.canDrawOverlays(context)
    fun intent(context: Context) = Intent(Settings.ACTION_MANAGE_OVERLAY_PERMISSION, Uri.parse("package:${context.packageName}"))
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
}
