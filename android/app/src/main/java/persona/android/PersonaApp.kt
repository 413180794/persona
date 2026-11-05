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
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map

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
    @Inject lateinit var capsule: persona.android.capsule.CapsuleRuntime
    /** The widget, the Quick Settings tile and the launcher shortcuts (platform/shortcuts). */
    @Inject lateinit var surfaces: persona.platform.shortcuts.SurfacesRuntime
    val pairingReady = MutableStateFlow(false)
    val pairingRestoreError = MutableStateFlow(false)

    /** 设置 › 界面风格; device-wide, kept across pairings. */
    val appearance: persona.core.designsystem.AppStyleStore by lazy { persona.core.designsystem.AppStyleStore(this) }

    /** 设置 › 外观 › 形象's hub packages; device-wide, listed again whenever the hub comes online. */
    val personas: persona.core.designsystem.PersonaStore by lazy {
        persona.core.designsystem.PersonaStore(this, appearance) {
            runtime.scopedData.value?.let { persona.core.hub.HttpPersonaPacks(runtime.connectionFor(it)) }
        }
    }

    private val _hub = MutableStateFlow<Hub?>(null)
    val hub: StateFlow<Hub?> = _hub.asStateFlow()

    /** Notifications, the widget and the tile are built from this context: their text follows 设置 › 语言 too. */
    override fun attachBaseContext(base: android.content.Context) {
        persona.core.designsystem.AppLanguages.init(base)
        super.attachBaseContext(persona.core.designsystem.AppLanguages.wrap(base))
    }

    override fun onCreate() {
        super.onCreate()
        pairings = PairingStore(this)
        Notify.install(this, connection.events)
        runtime.start()
        platform.start()
        surfaces.start()
        capsule.start()
        restorePairing()
        scope.launch {
            connection.state.map { it == persona.core.contracts.api.ConnectionState.Online }.distinctUntilChanged()
                .collect { online -> if (online) runCatching { personas.refresh() } }
        }
        // A tiered look moved to another tier: the new tier is downloaded whole before the face changes.
        scope.launch {
            connection.events.collect { event ->
                if (event is persona.core.contracts.PersonasEvent) runCatching { personas.refresh() }
            }
        }
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
