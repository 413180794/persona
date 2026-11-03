package persona.feature.settings

import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow

data class DeviceSettingsUiState(
    val devices: PairedDevices? = null,
    val preferences: DeviceNotificationPreferences? = null,
    val busy: Boolean = false,
    val message: String? = null,
    val needsCleanup: Boolean = false,
)

/** All mutations are serialized on the UI dispatcher; the host owns scoped unpair cleanup. */
class DeviceSettingsState(private val api: DeviceSettings?, private val unpair: suspend () -> Unit) {
    private val mutable = MutableStateFlow(DeviceSettingsUiState())
    val state = mutable.asStateFlow()

    suspend fun loadDevices() = perform("设备列表读取失败，请重试。") {
        try {
            mutable.value = mutable.value.copy(devices = requireNotNull(api).devices())
        } catch (unavailable: DeviceCredentialsUnavailable) {
            // Not a passing failure: say why this hub has no list (an older version, no database).
            mutable.value = mutable.value.copy(message = unavailable.message)
        }
    }

    suspend fun loadPreferences() = perform("通知偏好读取失败，请重试。") {
        mutable.value = mutable.value.copy(preferences = requireNotNull(api).preferences())
    }

    suspend fun savePreferences(next: DeviceNotificationPreferences) {
        if (mutable.value.busy || mutable.value.preferences == null) return
        val previous = mutable.value.preferences
        perform("保存失败，已恢复原来的通知偏好。") {
            mutable.value = mutable.value.copy(preferences = next)
            try {
                mutable.value = mutable.value.copy(preferences = requireNotNull(api).savePreferences(next))
            } catch (error: Exception) {
                mutable.value = mutable.value.copy(preferences = previous)
                throw error
            }
        }
    }

    suspend fun revoke(device: PairedDevice) = perform("撤销未完成，请刷新设备列表后重试。") {
        val devices = requireNotNull(mutable.value.devices)
        requireNotNull(api).revoke(device.id)
        mutable.value = mutable.value.copy(devices = devices.copy(devices = devices.devices.filterNot { it.id == device.id },
            revoked = listOf(device.copy(revokedAt = java.time.Instant.now().toString())) + devices.revoked))
        if (device.id == devices.currentId) cleanup()
    }

    suspend fun disconnect() = perform("断开未完成，请重试。") { cleanup() }

    private suspend fun cleanup() {
        mutable.value = mutable.value.copy(needsCleanup = true)
        unpair()
        mutable.value = DeviceSettingsUiState(message = "已断开配对。")
    }

    private suspend fun perform(failure: String, block: suspend () -> Unit) {
        if (mutable.value.busy) return
        mutable.value = mutable.value.copy(busy = true, message = null)
        try { block() }
        catch (error: CancellationException) { throw error }
        catch (_: Exception) {
            mutable.value = mutable.value.copy(message = if (mutable.value.needsCleanup)
                "本机清理未完成，请重试清理。" else failure)
        } finally { mutable.value = mutable.value.copy(busy = false) }
    }
}
