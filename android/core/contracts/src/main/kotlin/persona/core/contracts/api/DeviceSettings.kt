package persona.core.contracts.api

data class PairedDevice(
    val id: String,
    val name: String,
    val kind: String,
    val role: String,
    val lastSeen: String? = null,
    val platform: String? = null,
    val createdAt: String? = null,
    /** Set on credentials that were revoked; they can no longer connect or be revoked again. */
    val revokedAt: String? = null,
)

/** [devices] are the credentials in use; [revoked] the ones already revoked, kept by the hub for reference. */
data class PairedDevices(val devices: List<PairedDevice>, val currentId: String?, val revoked: List<PairedDevice> = emptyList())

/** The hub cannot list device credentials: too old a version (404), or no database (503). [message] says which. */
class DeviceCredentialsUnavailable(message: String) : Exception(message)

enum class DoneNotification(val wire: String) { Off("off"), Long("long"), Always("always") }

data class DeviceNotificationPreferences(
    val input: Boolean = true,
    val done: DoneNotification = DoneNotification.Long,
    val lockDetails: Boolean = false,
    val longTurnSeconds: Int? = null,
    val doneDelaySeconds: Int? = null,
    val liveActivity: Boolean = true,
    /** What the hub has of this device's 设置 › 翻译; null from a hub without it. */
    val translate: HubTranslatePrefs? = null,
)

/** Bound to one pairing; preferenceDeviceId is the ID used by notification registration. */
interface DeviceSettings {
    suspend fun devices(): PairedDevices
    suspend fun revoke(credentialId: String)
    suspend fun preferences(): DeviceNotificationPreferences
    suspend fun savePreferences(preferences: DeviceNotificationPreferences): DeviceNotificationPreferences
}
