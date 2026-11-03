package persona.android.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import persona.core.contracts.api.*
import persona.core.hub.HttpMachineManagement
import persona.core.hub.HttpModelConfiguration
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import persona.core.hub.OkHttpHubConnection
import kotlinx.serialization.json.JsonElement
import javax.inject.Singleton

/**
 * Machine management (机器页, 引擎, Claude Code 设置) and 模型配置, each call made on the pairing active when it is
 * made: the connection waits for its route, adds the device token and drops calls of a pairing that has gone.
 */
@Module
@InstallIn(SingletonComponent::class)
object MachineManagementModule {
    private fun ConnectionRuntime.connection(): OkHttpHubConnection {
        val data = scopedData.value ?: throw HubFailure(HubError.NoNetwork, "请先配对 hub", retryable = false)
        return connectionFor(data)
    }

    @Provides @Singleton fun machines(runtime: ConnectionRuntime): MachineManagement = object : MachineManagement {
        private fun api() = HttpMachineManagement(runtime.connection())
        override suspend fun launchers(machineId: String) = api().launchers(machineId)
        override suspend fun reconnect(machineId: String) = api().reconnect(machineId)
        override suspend fun deploy(machineId: String) = api().deploy(machineId)
        override suspend fun setEngine(machineId: String, engine: String) = api().setEngine(machineId, engine)
        override suspend fun claudeSettings(machineId: String) = api().claudeSettings(machineId)
        override suspend fun setClaudeSetting(machineId: String, key: String, value: JsonElement) =
            api().setClaudeSetting(machineId, key, value)
    }

    @Provides @Singleton fun models(runtime: ConnectionRuntime): ModelConfiguration = object : ModelConfiguration {
        private fun api() = HttpModelConfiguration(runtime.connection())
        override suspend fun catalog() = api().catalog()
        override suspend fun purpose(id: String) = api().purpose(id)
        override suspend fun save(purpose: ModelPurpose, draft: ModelConfigurationDraft) = api().save(purpose, draft)
        override suspend fun providerModels(provider: String, refresh: Boolean, machine: String?) = api().providerModels(provider, refresh, machine)
    }
}
