package persona.core.contracts.api

import persona.core.contracts.CoreText
import kotlinx.coroutines.flow.StateFlow

data class ConnectionCandidate(
    val url: String, val kind: String, val result: CoreText, val checkedAt: String? = null,
    val probeDurationMillis: Long? = null,
    val sampleCount: Int = 0,
    val measurementNote: CoreText = CoreText.of(CoreText.Key.NotMeasured),
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
    /** When the connection tries the hub again (ISO instant), while it waits between attempts; null otherwise. */
    val nextRetryAt: String? = null,
    /**
     * Why the latest attempt failed, in the user's words; null once a connection got through. Unlike
     * [lastDisconnectReason] (the raw message settings keep for diagnosis), it never outlives a successful connection.
     */
    val lastAttemptFailure: CoreText? = null,
    val unmeasuredFields: Map<String, CoreText> = emptyMap(),
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
