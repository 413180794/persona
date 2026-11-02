package jarvis.platform.notify

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import jarvis.core.contracts.FleetEvent
import jarvis.core.contracts.FleetState
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.HubConnection
import jarvis.core.contracts.api.PairingService
import jarvis.core.contracts.api.InteractionCenter
import jarvis.core.contracts.api.SessionsRepository
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

@AndroidEntryPoint
class HubService : Service() {
    @Inject lateinit var connection: HubConnection
    @Inject lateinit var pairing: PairingService
    @Inject lateinit var notifier: Notifier
    @Inject lateinit var journal: ExitJournal
    @Inject lateinit var interactions: InteractionCenter
    @Inject lateinit var sessions: SessionsRepository
    @Inject lateinit var notificationPreferences: NotificationPreferencesSource

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val fleet = MutableStateFlow(FleetState())
    private var observing = false
    private var networkCallback: ConnectivityManager.NetworkCallback? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        journal.capture()
        // Promote before deciding anything: stopping first (not paired, stopped by the user, or a stop that came while
        // this start was pending) is what crashed the app (ForegroundStarts). stopSelf(startId) leaves a newer start alone.
        try {
            ServiceCompat.startForeground(this, Notifier.ONGOING_ID, notifier.ongoing(connection.state.value, fleet.value),
                if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0)
        } catch (failure: RuntimeException) {
            Log.w(TAG, "Foreground promotion failed: ${failure.javaClass.simpleName}")
            starts.promoted()
            stopSelf(startId)
            return START_NOT_STICKY
        }
        if (starts.promoted() || journal.userStopped || !pairing.isPaired.value) {
            stopSelf(startId)
            return START_NOT_STICKY
        }
        running = true
        if (!observing) {
            observing = true
            scope.launch {
                launch {
                    notificationPreferences.preferences.collect {
                        notifier.updateOngoing(connection.state.value, fleet.value)
                    }
                }
                var displayed = OngoingStatus.from(connection.state.value, fleet.value)
                combine(connection.state, fleet) { state, snapshot ->
                    OngoingStatus.from(state, snapshot)
                }.throttleOngoing().collect { status ->
                    if (status != displayed) {
                        notifier.updateOngoing(status)
                        displayed = status
                    }
                }
            }
            scope.launch {
                val delivery = NotificationDelivery(interactions, sessions, ::suppressed,
                    notifier::postInteraction, notifier::postNotify, notifier::retract,
                    notificationPreferences::refreshPreferences)
                Notify.events.collect { event ->
                    if (event is FleetEvent) fleet.value = event.fleet
                    try {
                        // Bound refresh work without terminating observation on a transient HTTP failure.
                        withTimeout(8_000) { delivery.accept(event) }
                    } catch (failure: Exception) {
                        if (failure is CancellationException && failure !is kotlinx.coroutines.TimeoutCancellationException) throw failure
                        Log.w(TAG, "Notification refresh failed: ${failure.javaClass.simpleName}")
                    }
                }
            }
            scope.launch {
                pairing.isPaired.collect { if (!it && !starts.deferStop()) stopSelf() }
            }
            val manager = getSystemService(ConnectivityManager::class.java)
            val callback = object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) { connection.reconnect() }
                override fun onLost(network: Network) { connection.reconnect() }
            }
            runCatching { manager.registerDefaultNetworkCallback(callback) }
                .onSuccess { networkCallback = callback }
                .onFailure { Log.w(TAG, "Network observation failed: ${it.javaClass.simpleName}") }
            connection.reconnect()
        }
        return START_STICKY
    }

    private fun suppressed(session: SessionKey) =
        NotificationPolicy.suppressed(session, Notify.foreground.value, Notify.visibleSession.value)

    override fun onDestroy() {
        running = false
        scope.cancel()
        networkCallback?.let { callback ->
            runCatching { getSystemService(ConnectivityManager::class.java).unregisterNetworkCallback(callback) }
        }
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        super.onDestroy()
    }

    companion object {
        private const val TAG = "HubService"
        private val live = MutableStateFlow(false)
        private val starts = ForegroundStarts()
        val runningState = live.asStateFlow()
        @Volatile internal var running = false
            private set(value) { field = value; live.value = value }

        fun start(context: Context): Boolean {
            starts.requested()
            return try {
                ContextCompat.startForegroundService(context, Intent(context, HubService::class.java))
                true
            } catch (failure: RuntimeException) {
                starts.failed()
                Log.w(TAG, "Service launch failed: ${failure.javaClass.simpleName}")
                false
            }
        }

        /**
         * Stops the service only once it runs in the foreground. While one of our starts is pending the stop is left to
         * it (see [ForegroundStarts]); a start we did not send (the system restarting the service in a new process,
         * `am start-foreground-service`) is pending unseen while Application.onCreate runs this, and it checks pairing
         * and the user's stop itself after promoting.
         */
        fun stop(context: Context) {
            if (!starts.deferStop() && running) context.stopService(Intent(context, HubService::class.java))
        }
    }
}
