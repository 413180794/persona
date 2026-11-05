package persona.feature.settings

import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/**
 * Uses only observations from the running transport, never pairing snapshots; apart from them, the last pairing
 * attempt ([pairing]), which is all there is to show while pairing keeps failing.
 */
internal class DiagnosticsState(
    private val connection: HubConnection,
    private val source: ConnectionDiagnosticsSource?,
    private val history: SettingsDiagnostics?,
    attempts: PairingAttempts? = null,
) {
    val data: StateFlow<ConnectionDiagnostics> = source?.diagnostics ?: MutableStateFlow(ConnectionDiagnostics())
    val pairing: StateFlow<PairingAttempt?> = attempts?.last ?: MutableStateFlow(null)
    val busy = MutableStateFlow(false)
    val message = MutableStateFlow<String?>(null)
    val summary = MutableStateFlow<DeviceDisconnectSummary?>(null)

    suspend fun refresh(reprobe: Boolean = false) {
        if (!busy.compareAndSet(false, true)) return
        message.value = null
        try {
            if (reprobe) connection.reconnect()
            // Reconnection publishes probe results asynchronously through the source.
            if (!reprobe) source?.readDiagnostics()
            if (source == null) message.value = "运行连接尚未提供诊断数据，以下项目未实测。"
        } catch (error: CancellationException) { throw error }
        catch (_: Exception) { message.value = "连接记录暂时读不到，已保留运行连接的观测数据。" }
        finally { busy.value = false }
    }

    suspend fun refreshSummary() {
        try { summary.value = history?.disconnectSummary() }
        catch (error: CancellationException) { throw error }
        catch (_: Exception) { summary.value = null }
    }
}

internal fun ConnectionCandidate.probeMeasurement(): String {
    val duration = probeDurationMillis?.takeIf { it >= 0 && sampleCount > 0 } ?: return "未实测"
    return "$duration 毫秒（样本数 $sampleCount）" + if (sampleCount < 10) "，不作为性能基线" else ""
}

internal fun routeKind(kind: String): String = when (kind) {
    "ipv6" -> "公网 IPv6"
    "name" -> "域名"
    "lan" -> "内网"
    "ipv4" -> "公网 IPv4"
    "unknown" -> "未知类型"
    else -> kind
}

/** The rows 连接诊断 shows for the last pairing attempt. */
internal fun PairingAttempt.rows(): List<Pair<String, String?>> = listOf(
    "地址" to address,
    "结果" to (step?.let { "失败，停在「${it.label()}」" } ?: "成功"),
) + listOfNotNull(error?.let { "错误" to it }) + listOf("时间" to at)
