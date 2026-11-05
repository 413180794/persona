package persona.android.platform

import persona.android.AppText
import android.Manifest
import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import persona.android.mainActivityIntent
import persona.android.R
import persona.android.di.WiredPairingService
import persona.core.contracts.JsonCodec
import persona.core.contracts.LockTestEvent
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.*
import java.io.File
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class LockScreenTests @Inject constructor(
    private val application: Application,
    private val sessions: PlatformSessions,
    private val pairing: WiredPairingService,
) : SettingsDiagnostics {
    private val lock = Mutex()
    override suspend fun read() = pairing.read()
    override suspend fun reprobe() = pairing.reprobe()

    override suspend fun scheduleLockScreenTest(delaySeconds: Int) {
        require(delaySeconds in 1..3600)
        val session = sessions.requireSession()
        check("lock_test" in session.hub.connection.serverFeatures.value) { application.getString(R.string.app_error_update_hub) }
        lock.withLock {
            session.io {
                val record = session.json("POST", session.devicePath() + "/lock-tests",
                    buildJsonObject { put("delay_seconds", delaySeconds) })
                val test = decodeLockTest(record)
                writePlatformFile(File(session.directory, "lock-test.json"), record.toString())
                check(test.id.isNotBlank())
            }
        }
    }

    override suspend fun lockScreenTest(): LockScreenTest? {
        val session = sessions.requireSession()
        return lock.withLock { session.io {
            val file = File(session.directory, "lock-test.json")
            if (!file.exists()) return@io null
            val saved = decodeLockTest(JsonCodec.parseToJsonElement(file.readText()).jsonObject)
            val record = session.json("GET", session.devicePath() + "/lock-tests/" + safeId(saved.id))
            writePlatformFile(file, record.toString())
            decodeLockTest(record)
        } }
    }

    override suspend fun disconnectSummary(): DeviceDisconnectSummary? {
        val session = sessions.requireSession()
        if (session.data.credentials.deviceId == null) return null
        val summary = session.json("GET", session.devicePath() + "/sessions")["summary"] as? JsonObject ?: return null
        return DeviceDisconnectSummary(summary.getValue("disconnects").jsonPrimitive.int,
            summary.getValue("longest_gap_seconds").jsonPrimitive.double)
    }

    internal suspend fun observe(session: PlatformSession): Unit = coroutineScope {
        launch {
            session.hub.connection.events.filterIsInstance<LockTestEvent>().collect { event ->
                try {
                    lock.withLock { session.io {
                        val receipt = File(session.directory, "receipts/${safeId(event.testId)}.json")
                        // Replayed events reuse the first display result and never alert twice.
                        if (!receipt.exists()) {
                            val shown = show(event)
                            writePlatformFile(receipt, buildJsonObject { put("notification_shown", shown) }.toString())
                        }
                        report(session, receipt)
                    } }
                } catch (error: CancellationException) { throw error }
                catch (_: Exception) { /* Keep the receipt for the next connection. */ }
            }
        }
        session.hub.connection.state.collectLatest { state ->
            if (state != ConnectionState.Online) return@collectLatest
            while (currentCoroutineContext().isActive) {
                try {
                    lock.withLock { session.io {
                        File(session.directory, "receipts").listFiles()?.filter { it.extension == "json" }
                            ?.forEach { report(session, it) }
                    } }
                } catch (error: CancellationException) { throw error }
                catch (_: Exception) { }
                delay(15_000)
            }
        }
    }

    private suspend fun report(session: PlatformSession, file: File) {
        val record = session.json("POST", session.devicePath() + "/lock-tests/${safeId(file.nameWithoutExtension)}/receipt",
            JsonCodec.parseToJsonElement(file.readText()).jsonObject)
        val latest = File(session.directory, "lock-test.json")
        val latestId = if (latest.exists()) decodeLockTest(JsonCodec.parseToJsonElement(latest.readText()).jsonObject).id else null
        if (latestId == null || latestId == file.nameWithoutExtension) writePlatformFile(latest, record.toString())
        check(file.delete())
    }

    private fun show(event: LockTestEvent): Boolean {
        val manager = application.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(CHANNEL, application.getString(R.string.app_lock_test_channel), NotificationManager.IMPORTANCE_HIGH))
        if (!manager.areNotificationsEnabled() ||
            manager.getNotificationChannel(CHANNEL)?.importance == NotificationManager.IMPORTANCE_NONE ||
            Build.VERSION.SDK_INT >= 33 && application.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) return false
        val intent = mainActivityIntent(application).putExtra(EXTRA_LOCK_TEST, true)
            .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        val content = PendingIntent.getActivity(application, 0, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        return try {
            manager.notify(event.testId, 0, NotificationCompat.Builder(application, CHANNEL)
                .setSmallIcon(persona.core.designsystem.R.drawable.ic_stat).setContentTitle(event.title).setContentText(event.body)
                .setContentIntent(content).setAutoCancel(true).setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
                .setPriority(NotificationCompat.PRIORITY_HIGH).build())
            true
        } catch (_: SecurityException) { false }
    }

    companion object {
        const val EXTRA_LOCK_TEST = "platform.lock-test"
        private const val CHANNEL = "lock-test"
    }
}

internal fun decodeLockTest(record: JsonObject): LockScreenTest {
    fun text(key: String) = record[key]?.jsonPrimitive?.contentOrNull
    return LockScreenTest(requireNotNull(text("test_id")), requireNotNull(text("status")), requireNotNull(text("due_at")),
        text("sent_at"), text("received_at"), record["notification_shown"]?.jsonPrimitive?.booleanOrNull)
}

private fun safeId(value: String): String {
    require(value.matches(Regex("[A-Za-z0-9_-]+")))
    return value
}
private fun PlatformSession.devicePath() = "/api/v1/devices/" + safeId(requireNotNull(data.credentials.deviceId) { AppText.get(R.string.app_error_pair_again) })
private suspend fun PlatformSession.json(method: String, path: String, body: JsonObject? = null): JsonObject {
    active()
    val response = hub.api.raw(method, path, body?.toString())
    active()
    return JsonCodec.parseToJsonElement(response).jsonObject
}
