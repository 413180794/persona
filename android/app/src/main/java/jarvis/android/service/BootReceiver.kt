package jarvis.android.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import jarvis.android.JarvisApp

/** Reconnects after a reboot when a hub is paired (JarvisApp.onCreate does the connecting). */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        val app = context.applicationContext as JarvisApp
        if (app.hub.value != null) HubService.start(app)
    }
}
