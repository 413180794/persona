package jarvis.android.platform

import android.content.Context
import androidx.work.WorkerParameters

// Share retries enqueued before the upgrade keep the old class name; existing work is not cancelled or rebuilt.
class ShareRetryWorker(context: Context, parameters: WorkerParameters) :
    persona.android.platform.ShareRetryWorker(context, parameters)
