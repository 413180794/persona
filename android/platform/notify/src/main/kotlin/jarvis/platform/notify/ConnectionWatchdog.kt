package jarvis.platform.notify

import android.content.Context
import android.os.PowerManager
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import java.util.concurrent.TimeUnit

class ConnectionWatchdog(context: Context, parameters: WorkerParameters) : CoroutineWorker(context, parameters) {
    override suspend fun doWork(): Result {
        val dependencies = Notify.dependencies(applicationContext)
        dependencies.journal().capture()
        if (!dependencies.pairing().isPaired.value || dependencies.journal().userStopped || HubService.running) return Result.success()
        if (applicationContext.getSystemService(PowerManager::class.java)
                .isIgnoringBatteryOptimizations(applicationContext.packageName)) {
            HubService.start(applicationContext)
        }
        return Result.success()
    }

    companion object {
        private const val WORK_NAME = "jarvis-hub-watchdog"

        internal fun schedule(context: Context) {
            WorkManager.getInstance(context).enqueueUniquePeriodicWork(WORK_NAME, ExistingPeriodicWorkPolicy.KEEP,
                PeriodicWorkRequestBuilder<ConnectionWatchdog>(15, TimeUnit.MINUTES).build())
        }

        internal fun cancel(context: Context) { WorkManager.getInstance(context).cancelUniqueWork(WORK_NAME) }
    }
}
