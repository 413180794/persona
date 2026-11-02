package jarvis.android.di

import android.app.Application
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import jarvis.android.JarvisApp
import jarvis.core.contracts.api.*
import jarvis.core.hub.*
import jarvis.feature.settings.SettingsBinding
import jarvis.platform.notify.NotificationPreferencesSource
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import javax.inject.Inject
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object DeviceSettingsModule {
    @Provides @Singleton fun notificationPreferences(devices: WiredDeviceSettings): NotificationPreferencesSource = devices
}

/** Rebuilds the adapter for each pairing and verified route; retains no former hub snapshot. */
@Singleton
class WiredDeviceSettings @Inject constructor(application: Application, private val runtime: ConnectionRuntime) :
    NotificationPreferencesSource {
    private val app = application as JarvisApp
    private val mutableBinding = MutableStateFlow<SettingsBinding?>(null)
    val binding = mutableBinding.asStateFlow()
    private val mutablePreferences = MutableStateFlow<DeviceNotificationPreferences?>(null)
    override val preferences = mutablePreferences.asStateFlow()

    init {
        app.scope.launch {
            runtime.scopedData.collectLatest { data ->
                mutableBinding.value = null
                mutablePreferences.value = null
                if (data == null) return@collectLatest
                val connection = runtime.connectionFor(data)
                connection.route.collectLatest { route ->
                    mutablePreferences.value = null
                    val address = route?.endpoint?.url
                    val id = data.credentials.deviceId
                    val adapter = if (address == null || id.isNullOrBlank()) null else {
                        fun current() = runtime.scopedData.value === data && connection.route.value === route
                        val http = HttpDeviceSettings(address, data.credentials.deviceToken, id,
                            connection.http.client, ::current)
                        object : DeviceSettings {
                            suspend fun <T> checked(block: suspend () -> T): T {
                                if (!current()) throw CancellationException("Pairing changed")
                                try {
                                    val result = block()
                                    if (!current()) throw CancellationException("Pairing changed")
                                    return result
                                } catch (error: HubFailure) {
                                    if (error.kind == HubError.PairingExpired && current()) connection.revoke()
                                    throw error
                                }
                            }
                            override suspend fun devices() = checked { http.devices() }
                            override suspend fun revoke(credentialId: String) = checked { http.revoke(credentialId) }
                            override suspend fun preferences() = checked { http.preferences() }.also { mutablePreferences.value = it }
                            override suspend fun savePreferences(preferences: DeviceNotificationPreferences) =
                                checked { http.savePreferences(preferences) }.also { mutablePreferences.value = it }
                        }
                    }
                    mutableBinding.value = SettingsBinding(data.scope.path, adapter, address)
                    if (adapter != null) connection.state.collectLatest { state ->
                        if (state == ConnectionState.Online) try { adapter.preferences() }
                        catch (error: CancellationException) { throw error }
                        catch (_: Exception) { /* Retry at the next ready state or notification. */ }
                    }
                }
            }
        }
    }

    override suspend fun refreshPreferences(): DeviceNotificationPreferences? = binding.value?.deviceSettings?.preferences()
}
