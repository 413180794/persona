package jarvis.feature.settings

import jarvis.core.contracts.Machine
import jarvis.core.contracts.Session
import jarvis.core.contracts.api.*
import jarvis.core.contracts.ServerEvent
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject

/** Explicitly opt-in fixture for hosts and previews while transport implementations are pending. */
fun demoSettingsServices(): SettingsServices {
    val connection = object : HubConnection {
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = false
    }
    val pairing = object : PairingService {
        override val isPaired = MutableStateFlow(false)
        override suspend fun pair(request: PairingRequest) { isPaired.value = true }
        override suspend fun pair(request: PairingRequest, progress: (PairingProgress) -> Unit) {
            PairingStage.entries.filter { it != PairingStage.Complete }.forEach {
                progress(PairingProgress(it, candidates = if (it == PairingStage.Probe) listOf(
                    ConnectionCandidate("https://example.com", "域名", "示例：身份校验通过"),
                ) else emptyList()))
            }
            isPaired.value = true
        }
        override suspend fun unpair() { isPaired.value = false }
    }
    val sessions = object : SessionsRepository {
        override val machines = MutableStateFlow(listOf(Machine("example-machine", "示例电脑", online = true)))
        override val sessions = MutableStateFlow(emptyList<Session>())
        override suspend fun refresh() = Unit
    }
    val diagnostics = object : SettingsDiagnostics {
        override suspend fun read() = ConnectionDiagnostics(
            currentPath = "域名 · TLS（示例）",
            candidates = listOf(
                ConnectionCandidate("https://example.com", "域名", "示例：通过身份校验"),
                ConnectionCandidate("https://[2001:db8::1]", "公网 IPv6", "示例：无法连接"),
            ),
            lastDisconnectReason = "示例：手机后台连接被系统关闭",
            sessions = listOf(DeviceConnectionSession("示例连接时刻", "示例断开时刻", "示例：对端关闭连接")),
        )
        override suspend fun reprobe() = read()
        override suspend fun scheduleLockScreenTest(delaySeconds: Int) { throw UnsupportedOperationException("Demo cannot schedule notifications") }
    }
    val updates = object : AppUpdates {
        override suspend fun check() = AppUpdate(2_100_000_000, "示例版本", "https://example.com/app.apk", releaseNotes = "示例更新说明。体验数据不提供安装包。")
    }
    return SettingsServices(pairing, connection, sessions, updates, diagnostics, demo = true)
}
