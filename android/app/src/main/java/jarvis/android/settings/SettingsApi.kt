package jarvis.android.settings

import jarvis.android.data.ApiClient
import jarvis.android.data.JsonCodec
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonElement

// REST of 设置 and 机器 (docs/protocol.md), as ApiClient extensions over its `raw` call (Dispatchers.IO there).

/** The whitelisted Claude Code options of [machineId] with their values. */
suspend fun ApiClient.claudeSettings(machineId: String): List<ClaudeSetting> =
    JsonCodec.decodeFromString<ClaudeSettingsBody>(raw("GET", "/api/v1/machines/$machineId/claude-settings")).settings

@Serializable
private data class ClaudeSettingUpdate(val key: String, val value: JsonElement)

/** Writes one option on [machineId] (every Claude config directory there); [value] JsonNull = back to the default. */
suspend fun ApiClient.setClaudeSetting(machineId: String, key: String, value: JsonElement): List<ClaudeSetting> =
    JsonCodec.decodeFromString<ClaudeSettingsBody>(
        raw("PUT", "/api/v1/machines/$machineId/claude-settings", JsonCodec.encodeToString(ClaudeSettingUpdate(key, value))),
    ).settings

suspend fun ApiClient.notifyPrefs(deviceId: String): NotifyPrefs =
    JsonCodec.decodeFromString(raw("GET", "/api/v1/devices/$deviceId/prefs"))

/** Sends only `input` / `done`: the hub decodes them over what it has (live activity, island stay as they are). */
suspend fun ApiClient.setNotifyPrefs(deviceId: String, input: Boolean, done: DoneMode): NotifyPrefs =
    JsonCodec.decodeFromString(
        raw("PUT", "/api/v1/devices/$deviceId/prefs", JsonCodec.encodeToString(NotifyPrefsUpdate(input, done.wire))),
    )

suspend fun ApiClient.deviceTokens(): DeviceTokens = JsonCodec.decodeFromString(raw("GET", "/api/v1/auth/tokens"))

suspend fun ApiClient.revokeDeviceToken(id: String) {
    raw("DELETE", "/api/v1/auth/tokens/$id")
}

suspend fun ApiClient.machineMetrics(machineId: String, range: MetricRange): MachineMetrics =
    JsonCodec.decodeFromString<MachineMetricsBody>(
        raw("GET", "/api/v1/machines/$machineId/metrics", query = mapOf("range" to range.wire)),
    ).metrics
