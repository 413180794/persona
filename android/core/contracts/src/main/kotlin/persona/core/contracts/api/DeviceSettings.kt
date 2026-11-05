package persona.core.contracts.api

import persona.core.contracts.CoreTextException
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText

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
class DeviceCredentialsUnavailable(text: CoreText) : CoreTextException(text)

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
    /** 手机静音: no 需要你 / 做完 pushes while a desktop app is connected; null from a hub without it. */
    val muteWhileDesktop: Boolean? = null,
)

/** Bound to one pairing; preferenceDeviceId is the ID used by notification registration. */
interface DeviceSettings {
    suspend fun devices(): PairedDevices
    suspend fun revoke(credentialId: String)
    suspend fun preferences(): DeviceNotificationPreferences
    suspend fun savePreferences(preferences: DeviceNotificationPreferences): DeviceNotificationPreferences

    /**
     * 设置 › 设备 › 连接新设备: `POST /api/v1/auth/pair-codes` with the page address of this hub (docs/protocol.md「临时配对」),
     * a one-time code for another app (its QR rows) and the browser sign-in link.
     */
    suspend fun pairCode(): AppPairCode = throw UnsupportedOperationException("pair-codes")
}

/** A QR code the hub drew: [rows] of `0` / `1`, no quiet zone. */
data class QrImage(val size: Int, val rows: List<String>) {
    /** Whether it can be drawn: square, only 0 and 1. */
    val valid: Boolean get() = size > 0 && rows.size == size && rows.all { r -> r.length == size && r.all { it == '0' || it == '1' } }
}

/**
 * A one-time pairing code (ten minutes, traded once): [pairUrl] and its [qr] for an app, [webUrl] and [webQr] for a
 * browser (an older hub leaves the QR codes and the web link out). [loopbackOnly]: the hub's address this phone uses is
 * one only it can open (an SSH tunnel's 127.0.0.1), so the browser link does nothing elsewhere.
 */
data class AppPairCode(
    val token: String,
    /** Epoch milliseconds; 0 when the hub's time could not be read. */
    val expiresAt: Long,
    val pairUrl: String,
    val qr: QrImage? = null,
    val webUrl: String? = null,
    val webQr: QrImage? = null,
    val loopbackOnly: Boolean = false,
)
