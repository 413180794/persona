package jarvis.core.contracts.api

data class PairedDevice(
    val id: String,
    val name: String,
    val kind: String,
    val role: String,
    val lastSeen: String? = null,
    val platform: String? = null,
)

data class PairedDevices(val devices: List<PairedDevice>, val currentId: String?)

enum class DoneNotification(val wire: String) { Off("off"), Long("long"), Always("always") }

data class DeviceNotificationPreferences(
    val input: Boolean = true,
    val done: DoneNotification = DoneNotification.Long,
    val lockDetails: Boolean = false,
    val longTurnSeconds: Int? = null,
    val doneDelaySeconds: Int? = null,
    val liveActivity: Boolean = true,
)

/** Bound to one pairing; preferenceDeviceId is the ID used by notification registration. */
interface DeviceSettings {
    suspend fun devices(): PairedDevices
    suspend fun revoke(credentialId: String)
    suspend fun preferences(): DeviceNotificationPreferences
    suspend fun savePreferences(preferences: DeviceNotificationPreferences): DeviceNotificationPreferences
}
