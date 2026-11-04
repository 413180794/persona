package persona.platform.phonetools

import android.Manifest.permission.*
import android.app.NotificationManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat
import androidx.datastore.preferences.core.*
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import persona.core.contracts.HubScope
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import java.io.File

/** One instance per active HubScope; cancel the supplied scope when unpairing. */
class PhoneToolSettings(context: Context, scope: HubScope, coroutineScope: CoroutineScope) {
    private val store = PreferenceDataStoreFactory.create(scope = coroutineScope) {
        File(context.filesDir, "hub-data/${scope.path}/phone-tools.preferences_pb")
    }
    val enabled: StateFlow<Set<PhoneSource>> = store.data.map { values ->
        PhoneSource.entries.filter { values[booleanPreferencesKey(it.name)] == true }.toSet()
    }.stateIn(coroutineScope, SharingStarted.Eagerly, emptySet())
    suspend fun setEnabled(source: PhoneSource, enabled: Boolean) {
        store.edit { it[booleanPreferencesKey(source.name)] = enabled }
    }
}

class PhonePermissions(private val context: Context) {
    fun granted(permission: String) = ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED
    fun permissions(source: PhoneSource): Array<String> = when (source) {
        PhoneSource.Contacts -> arrayOf(READ_CONTACTS)
        PhoneSource.Calendar -> arrayOf(READ_CALENDAR, WRITE_CALENDAR)
        PhoneSource.Location -> arrayOf(ACCESS_COARSE_LOCATION, ACCESS_FINE_LOCATION)
        PhoneSource.Photos -> when {
            Build.VERSION.SDK_INT >= 34 -> arrayOf(READ_MEDIA_IMAGES, READ_MEDIA_VIDEO, READ_MEDIA_VISUAL_USER_SELECTED)
            Build.VERSION.SDK_INT >= 33 -> arrayOf(READ_MEDIA_IMAGES, READ_MEDIA_VIDEO)
            else -> arrayOf(READ_EXTERNAL_STORAGE)
        }
        PhoneSource.Notifications -> if (Build.VERSION.SDK_INT >= 33) arrayOf(POST_NOTIFICATIONS) else emptyArray()
        else -> emptyArray()
    }
    fun allowed(name: String, source: PhoneSource): Boolean = when (source) {
        PhoneSource.Calendar -> granted(READ_CALENDAR) && (name != "phone_calendar_create_event" || granted(WRITE_CALENDAR))
        PhoneSource.Location -> granted(ACCESS_COARSE_LOCATION) || granted(ACCESS_FINE_LOCATION)
        PhoneSource.Photos -> permissions(source).any(::granted)
        PhoneSource.Notifications -> name == "phone_notifications_list" ||
            (permissions(source).all(::granted) && context.getSystemService(NotificationManager::class.java).areNotificationsEnabled() &&
                context.getSystemService(NotificationManager::class.java).getNotificationChannel(PhoneNotifications.CHANNEL)?.importance != NotificationManager.IMPORTANCE_NONE)
        PhoneSource.Health -> true
        else -> permissions(source).all(::granted)
    }
    fun status(source: PhoneSource, healthGranted: Set<String>? = null): String = when {
        source == PhoneSource.Health -> when {
            !Health.available(context) -> Health.unavailableReason(context) ?: "不可用"
            healthGranted == null -> "已授权"
            healthGranted.containsAll(Health.permissions) -> "已授权"
            healthGranted.any { it in Health.permissions } -> "部分授权"
            else -> "未授权或已拒绝"
        }
        source == PhoneSource.Photos && Build.VERSION.SDK_INT >= 33 &&
            !(granted(READ_MEDIA_IMAGES) && granted(READ_MEDIA_VIDEO)) && allowed("", source) -> "部分授权"
        source == PhoneSource.Location && granted(ACCESS_COARSE_LOCATION) && !granted(ACCESS_FINE_LOCATION) -> "仅大致位置"
        source == PhoneSource.Calendar && granted(READ_CALENDAR) && !granted(WRITE_CALENDAR) -> "只读授权"
        permissions(source).isEmpty() && source != PhoneSource.Notifications -> "无需授权"
        allowed(if (source == PhoneSource.Calendar) "phone_calendar_create_event" else "", source) -> "已授权"
        else -> "未授权或已拒绝"
    }
}
