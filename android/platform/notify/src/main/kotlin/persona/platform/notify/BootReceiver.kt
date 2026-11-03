package persona.platform.notify

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

open class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED && intent.action != Intent.ACTION_MY_PACKAGE_REPLACED) return
        val dependencies = Notify.dependencies(context)
        dependencies.journal().capture()
        if (!dependencies.pairing().isPaired.value) return
        ConnectionWatchdog.schedule(context)
        if (!dependencies.journal().userStopped) HubService.start(context)
    }
}
