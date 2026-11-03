package persona.core.contracts.api

import kotlinx.coroutines.flow.StateFlow

data class ConnectionCandidate(
    val url: String, val kind: String, val result: String, val checkedAt: String? = null,
    val probeDurationMillis: Long? = null,
    val sampleCount: Int = 0,
    val measurementNote: String = "未实测",
)
data class DeviceConnectionSession(val connectedAt: String, val disconnectedAt: String? = null, val reason: String? = null)
data class ConnectionDiagnostics(
    val currentPath: String? = null,
    val candidates: List<ConnectionCandidate> = emptyList(),
    val fingerprint: String? = null,
    val foregroundHeartbeatSeconds: Int? = null,
    val backgroundHeartbeatSeconds: Int? = null,
    val lastDisconnectReason: String? = null,
    val sessions: List<DeviceConnectionSession> = emptyList(),
    val legacyHub: Boolean = false,
    val heartbeatSeconds: Int? = null,
    val pongTimeoutSeconds: Int? = null,
    val lastReceivedAt: String? = null,
    val unmeasuredFields: Map<String, String> = emptyMap(),
)

/** Live transport observations; null observations are reported in unmeasuredFields. */
interface ConnectionDiagnosticsSource {
    val diagnostics: StateFlow<ConnectionDiagnostics>
    suspend fun readDiagnostics(): ConnectionDiagnostics
}

/** The authenticated transport supplies diagnostics and schedules the notification on the hub. */
interface SettingsDiagnostics {
    suspend fun read(): ConnectionDiagnostics
    suspend fun reprobe(): ConnectionDiagnostics
    suspend fun scheduleLockScreenTest(delaySeconds: Int = 30)
    suspend fun lockScreenTest(): LockScreenTest? = null
    suspend fun disconnectSummary(): DeviceDisconnectSummary? = null
}

data class LockScreenTest(
    val id: String, val status: String, val dueAt: String,
    val sentAt: String? = null, val receivedAt: String? = null,
    val notificationShown: Boolean? = null,
)

data class DeviceDisconnectSummary(val disconnects: Int, val longestGapSeconds: Double)
