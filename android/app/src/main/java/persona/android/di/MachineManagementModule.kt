package persona.android.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import persona.core.contracts.GroupKeys
import persona.core.contracts.api.*
import persona.core.hub.HttpMachineManagement
import persona.core.hub.HttpModelConfiguration
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import persona.core.hub.OkHttpHubConnection
import javax.inject.Singleton

/**
 * Machine management (机器页, 引擎, Claude Code 设置) and 模型配置. Machine management goes to the hub group of the
 * machine it names; 模型配置 to the selected group. Either way the connection waits for its route, adds the device token
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
}
