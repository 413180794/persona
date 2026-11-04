package persona.feature.settings

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
import persona.core.contracts.api.*
import persona.core.navigation.Navigator
import persona.core.navigation.SettingsRoute

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
    /** Engine choice and Claude Code settings on the paired hub's machines; null in 体验模式. */
    val machines: MachineManagement? = null,
    /** 设置 › 模型配置 on the paired hub; null in 体验模式. */
    val models: ModelConfiguration? = null,
    /** 新增模型供应商 (from 模型配置) on the paired hub; null in 体验模式. */
    val providers: ProviderSetup? = null,
    /** 设置 › 插件 on the paired hub; null in 体验模式. */
    val plugins: PluginManagement? = null,
    /** Where the selected hub's plugins draw (nav pages, cards, machine sections), kept by the shell; null without. */
    val pluginSlots: PluginSlots? = null,
    /** 设置 › 插件 › 渠道 on the paired hub; null in 体验模式. */
    val channels: ChannelManagement? = null,
    /** The phone's hub groups (settings › hub); null where there is no real pairing (体验模式). */
    val hubs: HubGroups? = null,
    /** Pairs one more hub instead of replacing the pairing (settings › hub › 添加 hub). */
    val addHub: PairingService? = null,
    /** 设置 › 连接器 on the paired hub; null in 体验模式. */
    val connectors: ConnectorsManagement? = null,
    /** 设置 › hub 版本 (the hub's version and its updating itself) on the paired hub; null in 体验模式. */
    val hubUpdates: HubUpdates? = null,
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
