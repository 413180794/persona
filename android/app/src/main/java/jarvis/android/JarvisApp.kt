package jarvis.android

import android.app.Application
import dagger.hilt.android.HiltAndroidApp
import android.os.Build
import jarvis.android.data.Hub
import jarvis.android.data.PairingStore
import jarvis.android.di.ConnectionRuntime
import jarvis.android.di.WiredPairingService
import jarvis.core.contracts.api.HubConnection
import jarvis.platform.notify.Notify
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
class JarvisApp : Application() {
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    lateinit var pairings: PairingStore
        private set
    @Inject lateinit var runtime: ConnectionRuntime
    @Inject lateinit var connection: HubConnection
    @Inject lateinit var pairingService: WiredPairingService
    @Inject lateinit var platform: jarvis.android.platform.PlatformRuntime
    val pairingReady = MutableStateFlow(false)
    val pairingRestoreError = MutableStateFlow(false)

    /** 设置 › 界面风格; device-wide, kept across pairings. */
    val appearance: jarvis.core.designsystem.AppStyleStore by lazy { jarvis.core.designsystem.AppStyleStore(this) }

    private val _hub = MutableStateFlow<Hub?>(null)
    val hub: StateFlow<Hub?> = _hub.asStateFlow()

    override fun onCreate() {
        super.onCreate()
        pairings = PairingStore(this)
        Notify.install(this, connection.events)
        runtime.start()
        platform.start()
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
