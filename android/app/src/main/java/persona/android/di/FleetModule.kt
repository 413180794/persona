package persona.android.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import persona.core.contracts.FleetResponse
import persona.core.contracts.FleetState
import persona.core.contracts.JsonCodec
import persona.platform.notify.FleetSource
import kotlinx.coroutines.CancellationException
import javax.inject.Singleton

/** The fleet summary for the ongoing notification, the widget and the tile, over the current pairing's connection. */
@Module
@InstallIn(SingletonComponent::class)
object FleetModule {
    @Provides @Singleton
    fun fleetSource(runtime: ConnectionRuntime): FleetSource = object : FleetSource {
        override fun pairing(): String? = runtime.scopedData.value?.scope?.path

        override suspend fun fetch(): FleetState {
            val data = runtime.scopedData.value ?: throw CancellationException("Not paired")
            return runtime.connectionFor(data).request("/api/v1/fleet").use { response ->
                JsonCodec.decodeFromString(FleetResponse.serializer(), response.body!!.string()).fleet
            }
        }
    }
}
