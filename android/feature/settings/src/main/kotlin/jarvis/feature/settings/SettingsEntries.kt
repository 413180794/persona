package jarvis.feature.settings

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.platform.LocalContext
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import jarvis.core.contracts.api.*
import jarvis.core.navigation.Navigator
import jarvis.core.navigation.SettingsRoute

@EntryPoint
@InstallIn(SingletonComponent::class)
interface SettingsDependencies {
    fun settingsServices(): SettingsServices
    fun pairing(): PairingService
    fun connection(): HubConnection
    fun sessions(): SessionsRepository
    fun updates(): AppUpdates
}

data class SettingsBinding(val identity: String, val deviceSettings: DeviceSettings?, val hubAddress: String?)

data class SettingsServices(
    val pairing: PairingService,
    val connection: HubConnection,
    val sessions: SessionsRepository,
    val updates: AppUpdates,
    val diagnostics: SettingsDiagnostics? = null,
    val demo: Boolean = false,
    val deviceSettings: DeviceSettings? = null,
    val hubAddress: String? = null,
    // Must stop background work and clear all data for the old hub before returning.
    val disconnect: suspend () -> Unit = { pairing.unpair() },
    val liveDiagnostics: ConnectionDiagnosticsSource? = connection as? ConnectionDiagnosticsSource,
    val binding: kotlinx.coroutines.flow.StateFlow<SettingsBinding?>? = null,
    /** Manual candidates and route choices; null where the connection cannot change them (体验模式). */
    val endpoints: ConnectionEndpoints? = connection as? ConnectionEndpoints,
    /** The phone's hub groups (settings › hub); null where there is no real pairing (体验模式). */
    val hubs: HubGroups? = null,
    /** Pairs one more hub instead of replacing the pairing (settings › hub › 添加 hub). */
    val addHub: PairingService? = null,
)

fun EntryProviderScope<NavKey>.settingsEntries(navigator: Navigator, services: SettingsServices? = null) {
    entry<SettingsRoute> { SettingsScreen(services ?: injectedSettingsServices()) }
}

@Composable
private fun injectedSettingsServices(): SettingsServices {
    val context = LocalContext.current.applicationContext
    return remember(context) {
        val entry = EntryPointAccessors.fromApplication(context, SettingsDependencies::class.java)
        entry.settingsServices()
    }
}

@Composable
internal fun SettingsServices.bound(): SettingsServices {
    val source = binding ?: return this
    val current = source.collectAsState().value
    return remember(this, current) {
        copy(deviceSettings = current?.deviceSettings, hubAddress = current?.hubAddress, binding = null)
    }
}
