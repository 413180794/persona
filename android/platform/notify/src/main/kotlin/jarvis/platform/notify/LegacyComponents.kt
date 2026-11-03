package jarvis.platform.notify

import android.content.Context
import androidx.work.WorkerParameters

// 系统已持久化的 ComponentName 与 WorkManager 类名保持可加载；逻辑只在新命名空间实现。
class HubService : persona.platform.notify.HubService()
class ActionReceiver : persona.platform.notify.ActionReceiver()
class BootReceiver : persona.platform.notify.BootReceiver()
class ConnectionWatchdog(context: Context, parameters: WorkerParameters) :
    persona.platform.notify.ConnectionWatchdog(context, parameters)
