package jarvis.android.service

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.os.Build
import android.os.IBinder
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import jarvis.android.JarvisApp
import jarvis.android.data.ConnState
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch

/**
 * Keeps the hub WebSocket open while the app is in the background, so questions, plans and permission requests
 * become notifications without a push service (no FCM on most phones sold in China, no vendor push account needed).
 * The cost is a persistent notification and some battery; the user has to allow autostart / no battery restriction
 * on aggressive ROMs (docs/android.md).
 */
class HubService : Service() {
    private var job: Job? = null
    private var netCallback: ConnectivityManager.NetworkCallback? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val app = application as JarvisApp
        val n = app.notifier.ongoing(ConnState.Connecting, 0)
        ServiceCompat.startForeground(
            this, Notifier.ONGOING_ID, n,
            if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0,
        )
        if (job == null) {
            job = app.scope.launch {
                app.hub.flatMapLatest { hub ->
                    hub?.let { combine(it.conn, it.interactions) { c, ix -> c to ix.size } } ?: flowOf(ConnState.Offline to 0)
                }.collectLatest { (conn, pending) -> app.notifier.updateOngoing(conn, pending) }
            }
            val cm = getSystemService(ConnectivityManager::class.java)
            netCallback = object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) { app.hub.value?.kick() }
            }.also { cm.registerDefaultNetworkCallback(it) }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        job?.cancel()
        netCallback?.let { getSystemService(ConnectivityManager::class.java).unregisterNetworkCallback(it) }
        super.onDestroy()
    }

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, HubService::class.java))
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, HubService::class.java))
        }
    }
}
