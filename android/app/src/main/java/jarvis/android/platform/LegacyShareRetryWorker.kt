package jarvis.android.platform

import android.content.Context
import androidx.work.WorkerParameters

// 升级前入队的分享重试保留旧类名；不取消或重建已有任务。
class ShareRetryWorker(context: Context, parameters: WorkerParameters) :
    persona.android.platform.ShareRetryWorker(context, parameters)
