package jarvis.platform.notify

import jarvis.core.contracts.api.DeviceNotificationPreferences
import kotlinx.coroutines.flow.StateFlow

interface NotificationPreferencesSource {
    val preferences: StateFlow<DeviceNotificationPreferences?>
    suspend fun refreshPreferences(): DeviceNotificationPreferences?
}
