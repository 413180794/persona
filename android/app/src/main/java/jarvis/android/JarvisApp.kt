package jarvis.android

import android.app.Application
import android.os.Build
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.Pairing
import jarvis.android.data.PairingStore
import jarvis.android.service.HubService
import jarvis.android.service.Notifier
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/** Holds the one hub connection for the process: the activity and the foreground service share it. */
class JarvisApp : Application() {
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    lateinit var pairings: PairingStore
        private set
    lateinit var notifier: Notifier
        private set
    lateinit var phoneTools: jarvis.android.phone.PhoneToolRunner
        private set

    private val _hub = MutableStateFlow<Hub?>(null)
    val hub: StateFlow<Hub?> = _hub.asStateFlow()

    override fun onCreate() {
        super.onCreate()
        instance = this
        pairings = PairingStore(this)
        notifier = Notifier(this)
        phoneTools = jarvis.android.phone.PhoneToolRunner(this)
        jarvis.android.shortcuts.Shortcuts.publish(this)
        pairings.load()?.let { connect(it) }
    }

    val deviceName: String get() = "${Build.MANUFACTURER} ${Build.MODEL}".trim()

    fun connect(p: Pairing) {
        _hub.value?.stop()
        val hub = Hub(ApiClient(p.url, p.token), scope, deviceName)
        _hub.value = hub
        notifier.attach(hub)
        phoneTools.attach(hub) // before start: the first connect declares the tools
        hub.start()
        HubService.start(this)
        // The ongoing notification, widget and tile follow the same summary (fleet/).
        jarvis.android.fleet.FleetStore.follow(this, hub) { refreshFleetViews() }
    }

    fun unpair() {
        _hub.value?.stop()
        _hub.value = null
        pairings.clear()
        HubService.stop(this)
        jarvis.android.fleet.FleetStore.clear(this)
        scope.launch { refreshFleetViews() }
    }

    private suspend fun refreshFleetViews() {
        jarvis.android.widget.JarvisWidget.refresh(this)
        jarvis.android.shortcuts.JarvisTileService.refresh(this)
    }

    companion object {
        lateinit var instance: JarvisApp
            private set
    }
}
