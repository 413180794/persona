package persona.android.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import persona.core.contracts.GroupKeys
import persona.core.contracts.api.*
import persona.core.hub.HttpMachineManagement
import persona.core.hub.HttpModelConfiguration
import persona.core.hub.HttpPluginManagement
import persona.core.hub.HttpProviderSetup
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import persona.core.hub.OkHttpHubConnection
import android.app.Application
import persona.android.PersonaApp
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.JsonObject
import javax.inject.Singleton

/**
 * Machine management (机器页, 引擎, Claude Code 设置), 模型配置 and 插件. Machine management goes to the hub group of the
 * machine it names; 模型配置 and 插件 to the selected group. Either way the connection waits for its route, adds the device token
 * and drops calls of a pairing that has gone.
 */
@Module
@InstallIn(SingletonComponent::class)
object MachineManagementModule {
    private fun ConnectionRuntime.connection(): OkHttpHubConnection {
        val data = scopedData.value ?: throw HubFailure(HubError.NoNetwork, "请先配对 hub", retryable = false)
        return connectionFor(data)
    }

    // The machine ids come from the merged lists: each call goes to the group the id names, on that group's connection.
    @Provides @Singleton fun machines(stacks: GroupStacks): MachineManagement = mergedMachineManagement(object : MachineRoutes {
        override suspend fun <T> on(machineId: String, call: suspend (MachineManagement) -> T): T =
            stacks.use(GroupKeys.namespaceOf(machineId)) { call(HttpMachineManagement(it.connection).inGroup(it.names)) }
    })

    // A hub-level page: the selected group's hub, whose own plain ids it reads and sends back (docs/android.md「多 hub」).
    @Provides @Singleton fun models(runtime: ConnectionRuntime): ModelConfiguration = object : ModelConfiguration {
        private fun api() = HttpModelConfiguration(runtime.connection())
        override suspend fun catalog() = api().catalog()
        override suspend fun purpose(id: String) = api().purpose(id)
        override suspend fun save(purpose: ModelPurpose, draft: ModelConfigurationDraft) = api().save(purpose, draft)
        override suspend fun providerModels(provider: String, refresh: Boolean, machine: String?) = api().providerModels(provider, refresh, machine)
    }

    // 新增模型供应商 from 模型配置: the selected group's hub keeps the provider.
    @Provides @Singleton fun providers(runtime: ConnectionRuntime): ProviderSetup = object : ProviderSetup {
        private fun api() = HttpProviderSetup(runtime.connection())
        override suspend fun presets() = api().presets()
        override suspend fun add(provider: NewProvider) = api().add(provider)
    }

    // A hub-level page too: the selected group's plugins.
    @OptIn(ExperimentalCoroutinesApi::class)
    @Provides @Singleton fun plugins(runtime: ConnectionRuntime, application: Application): PluginManagement = object : PluginManagement {
        // The selected hub's features, from its hello: the settings row shows while it says plugin_manager.
        override val available: Flow<Boolean> = (application as PersonaApp).hub
            .flatMapLatest { hub -> hub?.connection?.serverFeatures ?: flowOf(emptySet()) }
            .map { "plugin_manager" in it }.distinctUntilChanged()
        private fun api() = HttpPluginManagement(runtime.connection(), available)
        override suspend fun plugins() = api().plugins()
        override suspend fun plugin(id: String) = api().plugin(id)
        override suspend fun setEnabled(id: String, enabled: Boolean) = api().setEnabled(id, enabled)
        override suspend fun setConfig(id: String, config: JsonObject) = api().setConfig(id, config)
        override suspend fun setSecret(id: String, name: String, value: String) = api().setSecret(id, name, value)
        override suspend fun clearSecret(id: String, name: String) = api().clearSecret(id, name)
        override suspend fun listing() = api().listing()
        override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?) = api().uiPlaces(placement, launcher, machine)
        override suspend fun renderUi(plugin: String, ui: String, context: PluginUiContext?, state: String?) =
            api().renderUi(plugin, ui, context, state)
        override suspend fun uiAction(
            plugin: String, ui: String, action: String, context: PluginUiContext?, state: String?, values: Map<String, String>?,
        ) = api().uiAction(plugin, ui, action, context, state, values)
        override suspend fun markets() = api().markets()
        override suspend fun addMarket(spec: String, token: String?) = api().addMarket(spec, token)
        override suspend fun removeMarket(id: String) = api().removeMarket(id)
        override suspend fun refreshMarket(id: String) = api().refreshMarket(id)
        override suspend fun setMarketToken(id: String, token: String) = api().setMarketToken(id, token)
        override suspend fun catalog(query: String, market: String?, refresh: Boolean) = api().catalog(query, market, refresh)
        override suspend fun previewFromMarket(market: String, plugin: String) = api().previewFromMarket(market, plugin)
        override suspend fun install(preview: PluginPreview) = api().install(preview)
        override suspend fun upgrade(id: String, preview: PluginPreview) = api().upgrade(id, preview)
        override suspend fun uninstall(id: String, keepData: Boolean) = api().uninstall(id, keepData)
    }
}
