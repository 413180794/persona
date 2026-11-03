package persona.android

import android.app.Application
import dagger.hilt.android.HiltAndroidApp
import android.os.Build
import persona.android.data.Hub
import persona.android.data.PairingStore
import persona.android.di.ConnectionRuntime
import persona.android.di.WiredPairingService
import persona.core.contracts.api.HubConnection
import persona.platform.notify.Notify
import javax.inject.Inject
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/** Holds the one hub connection for the process: the activity and the foreground service share it. */
@HiltAndroidApp
class PersonaApp : Application() {
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    lateinit var pairings: PairingStore
        private set
    @Inject lateinit var runtime: ConnectionRuntime
    @Inject lateinit var connection: HubConnection
    @Inject lateinit var pairingService: WiredPairingService
    @Inject lateinit var platform: persona.android.platform.PlatformRuntime
    /** The widget, the Quick Settings tile and the launcher shortcuts (platform/shortcuts). */
    @Inject lateinit var surfaces: persona.platform.shortcuts.SurfacesRuntime
    val pairingReady = MutableStateFlow(false)
    val pairingRestoreError = MutableStateFlow(false)

    /** 设置 › 界面风格; device-wide, kept across pairings. */
    val appearance: persona.core.designsystem.AppStyleStore by lazy { persona.core.designsystem.AppStyleStore(this) }

    private val _hub = MutableStateFlow<Hub?>(null)
    val hub: StateFlow<Hub?> = _hub.asStateFlow()

    override fun onCreate() {
        super.onCreate()
        pairings = PairingStore(this)
        Notify.install(this, connection.events)
        runtime.start()
        platform.start()
        surfaces.start()
        restorePairing()
    }

    val deviceName: String get() = "${Build.MANUFACTURER} ${Build.MODEL}".trim()

    /** Picks up what the runtime restored or migrated, for first-run routing and the settings screens. */
    fun restorePairing() {
        scope.launch {
            pairingRestoreError.value = false
            try {
                pairingService.restore()
                pairingReady.value = true
            } catch (error: kotlinx.coroutines.CancellationException) { throw error }
            catch (_: Exception) { pairingRestoreError.value = true }
        }
    }

    internal fun showHub(value: Hub?) { _hub.value = value }

}
