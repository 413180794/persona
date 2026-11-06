package jarvis.platform.phonetools

import android.content.Context
import androidx.work.WorkerParameters

// Delayed notifications already enqueued still use the old class name; it extends the new implementation to keep the work and its input data.
class PhoneNotificationWorker(context: Context, parameters: WorkerParameters) :
    persona.platform.phonetools.PhoneNotificationWorker(context, parameters)
