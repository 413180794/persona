package persona.feature.settings

import android.content.res.Resources
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.*
import persona.core.contracts.ServerEvent
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject

/** Explicitly opt-in fixture for hosts and previews while transport implementations are pending, in the language of [resources]. */
fun demoSettingsServices(resources: Resources): SettingsServices {
    fun t(id: Int) = resources.getString(id)
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
                    ConnectionCandidate("https://example.com", "name", persona.core.contracts.CoreText.plain(t(R.string.settings_demo_identity_ok))),
                ) else emptyList()))
            }
            isPaired.value = true
        }
        override suspend fun unpair() { isPaired.value = false }
    }
    val sessions = object : SessionsRepository {
        override val machines = MutableStateFlow(listOf(Machine("example-machine", t(R.string.settings_demo_computer), online = true)))
        override val sessions = MutableStateFlow(emptyList<Session>())
        override suspend fun refresh() = Unit
    }
    val diagnostics = object : SettingsDiagnostics {
        override suspend fun read() = ConnectionDiagnostics(
            currentPath = "name · " + t(R.string.settings_demo_path),
            candidates = listOf(
                ConnectionCandidate("https://example.com", "name", persona.core.contracts.CoreText.plain(t(R.string.settings_demo_identity_passed))),
                ConnectionCandidate("https://[2001:db8::1]", "ipv6", persona.core.contracts.CoreText.plain(t(R.string.settings_demo_unreachable))),
            ),
            lastDisconnectReason = t(R.string.settings_demo_disconnect_reason),
            sessions = listOf(DeviceConnectionSession(t(R.string.settings_demo_connected_at), t(R.string.settings_demo_disconnected_at), t(R.string.settings_demo_peer_closed))),
        )
        override suspend fun reprobe() = read()
        override suspend fun scheduleLockScreenTest(delaySeconds: Int) { throw UnsupportedOperationException("Demo cannot schedule notifications") }
    }
    val updates = object : AppUpdates {
        override suspend fun check() = AppUpdate(2_100_000_000, t(R.string.settings_demo_version), "https://example.com/app.apk", releaseNotes = t(R.string.settings_demo_release_notes))
    }
    return SettingsServices(pairing, connection, sessions, updates, diagnostics, demo = true)
}
