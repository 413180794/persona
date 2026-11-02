package jarvis.android.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import jarvis.core.contracts.api.HubConnection
import jarvis.core.contracts.api.MessageSender
import jarvis.core.data.HubDataStore
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object ConnectionModule {
    @Provides @Singleton fun listPreferences(runtime: ConnectionRuntime): androidx.datastore.core.DataStore<androidx.datastore.preferences.core.Preferences> =
        jarvis.android.data.ScopedListPreferences(runtime)
    @Provides @Singleton fun connection(runtime: ConnectionRuntime): HubConnection = runtime
    @Provides @Singleton fun sender(runtime: ConnectionRuntime): MessageSender = runtime.messages
    @Provides @Singleton fun data(runtime: ConnectionRuntime): HubDataStore = runtime.dataStore
    @Provides @Singleton fun groups(runtime: ConnectionRuntime): jarvis.core.contracts.api.HubGroups = runtime.hubGroups
}
