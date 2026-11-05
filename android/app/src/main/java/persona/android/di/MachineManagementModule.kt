package persona.android.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import persona.core.contracts.GroupKeys
import persona.core.contracts.HubUpdateEvent
import persona.core.contracts.ExtensionUiEvent
import persona.core.contracts.api.*
import persona.core.hub.HttpConnectorsManagement
import persona.core.hub.HttpHubUpdates
import persona.core.hub.HttpMachineManagement
import persona.core.hub.HttpAgentTriage
import persona.core.hub.HttpModelConfiguration
import persona.core.hub.HttpExtensionManagement
import persona.core.hub.HttpProviderSetup
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import persona.core.hub.OkHttpHubConnection
import android.app.Application
import persona.android.PersonaApp
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.filterIsInstance
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.JsonObject
import javax.inject.Singleton

/**
 * Machine management (机器页, 引擎, Claude Code 设置), 模型配置, 扩展 and hub 版本. Machine management goes to the hub group of the
 * machine it names; 模型配置 and 扩展 to the selected group. Either way the connection waits for its route, adds the device token
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
    // Bound to the pairing at each call, like 模型配置: switching hubs reads the new hub's switches.
    @Provides @Singleton fun triage(runtime: ConnectionRuntime): AgentTriage = object : AgentTriage {
        override suspend fun switches() = HttpAgentTriage(runtime.connection()).switches()
        override suspend fun put(value: TriageSwitches) = HttpAgentTriage(runtime.connection()).put(value)
    }

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

    // A hub-level page too: the selected group's extensions.
    @OptIn(ExperimentalCoroutinesApi::class)
    @Provides @Singleton fun extensions(runtime: ConnectionRuntime, application: Application): ExtensionManagement = object : ExtensionManagement {
        // The selected hub's features, from its hello: the settings row shows while it says extension_manager
        // (plugin_manager before #1844).
        private val features = (application as PersonaApp).hub
            .flatMapLatest { hub -> hub?.connection?.serverFeatures ?: flowOf(emptySet()) }
        override val available: Flow<Boolean> = features.map { "extension_manager" in it || "plugin_manager" in it }.distinctUntilChanged()
        // Extensions' UI slots (docs/extensions.md「UI 插槽」) and the badge events of the selected hub.
        override val slotsAvailable: Flow<Boolean> = features.map { "extension_ui_slots" in it || "plugin_ui_slots" in it }.distinctUntilChanged()
        override val uiEvents: Flow<ExtensionUiEvent> = (application as PersonaApp).hub
            .flatMapLatest { hub -> hub?.connection?.events ?: emptyFlow() }.filterIsInstance<ExtensionUiEvent>()
        // A hub without the `extensions` feature (before #1844, or no hello yet) answers under /api/v1/plugins only;
        // a newer one under both.
        private fun api() = runtime.connection().let { HttpExtensionManagement(it, available, legacy = "extensions" !in it.serverFeatures.value) }
        private val updates = kotlinx.coroutines.flow.MutableSharedFlow<Unit>(extraBufferCapacity = 1)
        override val changes: Flow<Unit> = updates
        private suspend fun <T> changed(block: suspend () -> T): T = block().also { updates.tryEmit(Unit) }
        override suspend fun quickPrompts() = api().quickPrompts()
        override suspend fun themes() = api().themes()
        override suspend fun triageTemplates() = api().triageTemplates()
        override suspend fun setContribution(id: String, point: String, contribution: String, enabled: Boolean) =
            changed { api().setContribution(id, point, contribution, enabled) }
        override suspend fun extensions() = api().extensions()
        override suspend fun extension(id: String) = api().extension(id)
        override suspend fun setEnabled(id: String, enabled: Boolean) = changed { api().setEnabled(id, enabled) }
        override suspend fun setConfig(id: String, config: JsonObject) = api().setConfig(id, config)
        override suspend fun setSecret(id: String, name: String, value: String) = api().setSecret(id, name, value)
        override suspend fun clearSecret(id: String, name: String) = api().clearSecret(id, name)
        override suspend fun listing() = api().listing()
        override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?) = api().uiPlaces(placement, launcher, machine)
        override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?) =
            api().renderUi(extension, ui, context, state)
        override suspend fun uiAction(
            extension: String, ui: String, action: String, context: ExtensionUiContext?, state: String?, values: Map<String, String>?,
        ) = api().uiAction(extension, ui, action, context, state, values)
        override suspend fun markets() = api().markets()
        override suspend fun addMarket(spec: String, token: String?) = api().addMarket(spec, token)
        override suspend fun removeMarket(id: String) = api().removeMarket(id)
        override suspend fun refreshMarket(id: String) = api().refreshMarket(id)
        override suspend fun setMarketToken(id: String, token: String) = api().setMarketToken(id, token)
        override suspend fun catalog(query: String, market: String?, refresh: Boolean) = api().catalog(query, market, refresh)
        override suspend fun previewFromMarket(market: String, extension: String) = api().previewFromMarket(market, extension)
        override suspend fun install(preview: ExtensionPreview) = changed { api().install(preview) }
        override suspend fun upgrade(id: String, preview: ExtensionPreview) = changed { api().upgrade(id, preview) }
        override suspend fun uninstall(id: String, keepData: Boolean) = changed { api().uninstall(id, keepData) }
    }

    // 设置 › hub 版本 (docs/hub-update.md「App 里」): the selected group's hub, following its `hub_update` pushes.
    @OptIn(ExperimentalCoroutinesApi::class)
    @Provides @Singleton fun hubUpdates(runtime: ConnectionRuntime, application: Application): HubUpdates = object : HubUpdates {
        override val events: Flow<HubUpdateStatus> = (application as PersonaApp).hub
            .flatMapLatest { hub -> hub?.connection?.events ?: emptyFlow() }.filterIsInstance<HubUpdateEvent>().map { it.update }
        private fun api() = HttpHubUpdates(runtime.connection())
        override suspend fun status() = api().status()
        override suspend fun check() = api().check()
        override suspend fun start(now: Boolean) = api().start(now)
        override suspend fun settings(auto: Boolean?, noticeSeen: Boolean?) = api().settings(auto, noticeSeen)
        override suspend fun history() = api().history()
    }

    // A hub-level page: the selected group's connectors (docs/connectors.md).
    @Provides @Singleton fun connectors(runtime: ConnectionRuntime): ConnectorsManagement = object : ConnectorsManagement {
        private fun api() = HttpConnectorsManagement(runtime.connection())
        override suspend fun connectors(all: Boolean) = api().connectors(all)
        override suspend fun setConnectorEnabled(id: String, enabled: Boolean) = api().setConnectorEnabled(id, enabled)
        override suspend fun setConnectorFeed(id: String, feed: String, on: Boolean) = api().setConnectorFeed(id, feed, on)
        override suspend fun deleteConnectorData(id: String) = api().deleteConnectorData(id)
        override suspend fun deleteConnectorArchive(id: String) = api().deleteConnectorArchive(id)
        override suspend fun mailAccounts() = api().mailAccounts()
        override suspend fun addMailAccount(req: AddMailAccountRequest) = api().addMailAccount(req)
        override suspend fun deleteMailAccount(id: String) = api().deleteMailAccount(id)
        override suspend fun testMailAccount(id: String) = api().testMailAccount(id)
        override suspend fun googleSignInStart(loginHint: String?) = api().googleSignInStart(loginHint)
        override suspend fun googleSignInFinish(finish: GoogleFinish) = api().googleSignInFinish(finish)
        override suspend fun larkStatus() = api().larkStatus()
        override suspend fun larkAccounts() = api().larkAccounts()
        override suspend fun larkChatChoices() = api().larkChatChoices()
    }
}
