package persona.android.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import persona.core.contracts.FleetState
import persona.platform.notify.FleetSource
import javax.inject.Singleton

/** The fleet summary for the ongoing notification, the widget and the tile, over the current pairing's connection. */
@Module
@InstallIn(SingletonComponent::class)
object FleetModule {
    @Provides @Singleton
    fun fleetSource(runtime: ConnectionRuntime): FleetSource = object : FleetSource {
        override fun pairing(): String? = runtime.scopedData.value?.scope?.path

        // Every group's, merged in the ids the pushed `fleet` carries: the selected group's alone would hide the others.
        override suspend fun fetch(): FleetState = runtime.fetchFleet()
    }
}
