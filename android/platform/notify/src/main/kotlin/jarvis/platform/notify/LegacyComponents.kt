package jarvis.platform.notify

import android.content.Context
import androidx.work.WorkerParameters

// Keeps the ComponentName and WorkManager class names the system has persisted loadable; the logic lives only in the new namespace.
class HubService : persona.platform.notify.HubService()
class ActionReceiver : persona.platform.notify.ActionReceiver()
class BootReceiver : persona.platform.notify.BootReceiver()
class ConnectionWatchdog(context: Context, parameters: WorkerParameters) :
    persona.platform.notify.ConnectionWatchdog(context, parameters)
