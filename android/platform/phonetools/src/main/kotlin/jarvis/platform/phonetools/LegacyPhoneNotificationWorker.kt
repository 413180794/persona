package jarvis.platform.phonetools

import android.content.Context
import androidx.work.WorkerParameters

// 已入队的延时通知仍使用旧类名，继承新实现以保留任务与输入数据。
class PhoneNotificationWorker(context: Context, parameters: WorkerParameters) :
    persona.platform.phonetools.PhoneNotificationWorker(context, parameters)
