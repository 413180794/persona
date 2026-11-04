package persona.android.di

import android.app.Application
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import persona.android.PersonaApp
import persona.core.contracts.api.*
import persona.core.hub.*
import persona.feature.settings.SettingsBinding
import persona.platform.notify.NotificationPreferencesSource
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
class WiredDeviceSettings @Inject constructor(
    application: Application,
    private val runtime: ConnectionRuntime,
    private val translation: TranslationSettings,
) :
    NotificationPreferencesSource {
    private val app = application as PersonaApp
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
                        object : TranslationSyncedSettings {
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
                            override suspend fun saveTranslation(prefs: HubTranslatePrefs) = checked { http.saveTranslatePrefs(prefs) }
                        }
                    }
                    mutableBinding.value = SettingsBinding(data.scope.path, adapter, address)
                    if (adapter != null) connection.state.collectLatest { state ->
                        if (state != ConnectionState.Online) return@collectLatest
                        // Unread (the notification prefs retry at the next ready state): send ours as if the hub had none.
                        val hubHas = try { adapter.preferences().translate }
                        catch (error: CancellationException) { throw error }
                        catch (_: Exception) { null }
                        // 设置 › 翻译 goes to the hub (on connect and when it changes), so it translates new mail as it
                        // comes in for this phone (docs/android.md「翻译」).
                        syncTranslation(translation.prefs, hubHas, adapter::saveTranslation)
                    }
                }
            }
        }
    }

    override suspend fun refreshPreferences(): DeviceNotificationPreferences? = binding.value?.deviceSettings?.preferences()
}

/** The paired hub's device settings, which also take this phone's 设置 › 翻译. */
private interface TranslationSyncedSettings : DeviceSettings {
    suspend fun saveTranslation(prefs: HubTranslatePrefs): HubTranslatePrefs?
}

/**
 * Sends this phone's 设置 › 翻译 to the hub whenever it differs from what the hub was last known to have ([hubHas] at
 * connect, then each answer). A failed save is tried again at the next change or connection. Runs until cancelled.
 */
internal suspend fun syncTranslation(
    prefs: Flow<TranslationPrefs>,
    hubHas: HubTranslatePrefs?,
    save: suspend (HubTranslatePrefs) -> HubTranslatePrefs?,
) {
    var known = hubHas
    prefs.map { it.hubCopy }.distinctUntilChanged().collect { mine ->
        if (mine == known) return@collect
        known = try { save(mine) ?: mine }
        catch (error: CancellationException) { throw error }
        catch (_: Exception) { known }
    }
}
