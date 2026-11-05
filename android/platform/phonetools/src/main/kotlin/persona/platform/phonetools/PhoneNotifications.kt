package persona.platform.phonetools

import android.app.*
import android.content.Context
import androidx.work.*
import persona.core.contracts.api.PhoneToolResult
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.json.*
import java.util.UUID
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

internal class PhoneNotifications(private val context: Context) {
    private val manager = context.getSystemService(NotificationManager::class.java)

    /** phone_notify, answering as the iOS app's NotificationsTool does ({scheduled, delay_seconds}). */
    suspend fun schedule(args: JsonObject): PhoneToolResult {
        val title = args.text("title") ?: invalid("title is required")
        val body = args.text("body") ?: invalid("body is required")
        val delay = args.int("delay_seconds", 0, 0, 86_400)
        channel()
        if (!manager.areNotificationsEnabled() || manager.getNotificationChannel(CHANNEL).importance == NotificationManager.IMPORTANCE_NONE) {
            throw PhoneToolFailure(PhoneToolFailure.Kind.Denied, "通知 / Notifications. The user has turned notifications off " +  // i18n-ignore: tool result for the model
                "for Persona in Android Settings, so nothing was shown.")
        }
        val id = UUID.randomUUID().toString()
        if (delay == 0) post(id, title, body) else {
            val request = OneTimeWorkRequestBuilder<PhoneNotificationWorker>()
                .setInitialDelay(delay.toLong(), TimeUnit.SECONDS)
                .setInputData(workDataOf("id" to id, "title" to title, "body" to body))
                .addTag("phone-tools-notification").build()
            val future = WorkManager.getInstance(context).enqueue(request).result
            suspendCancellableCoroutine<Unit> { continuation ->
                future.addListener({
                    try { future.get(); if (continuation.isActive) continuation.resume(Unit) }
                    catch (error: Exception) { if (continuation.isActive) continuation.resumeWithException(error) }
                }, context.mainExecutor)
            }
        }
        return PhoneToolResult.text(buildJsonObject {
            put("scheduled", true); put("delay_seconds", delay)
            if (delay > 0) put("note", "Android schedules delayed notifications itself; it may arrive a little late.")
        }.toString())
    }

    fun post(id: String, title: String, body: String) {
        channel()
        check(manager.areNotificationsEnabled() && manager.getNotificationChannel(CHANNEL).importance != NotificationManager.IMPORTANCE_NONE)
        val builder = Notification.Builder(context, CHANNEL).setSmallIcon(persona.core.designsystem.R.drawable.ic_stat)
            .setContentTitle(title).setContentText(body).setStyle(Notification.BigTextStyle().bigText(body))
            .setVisibility(Notification.VISIBILITY_PRIVATE).setAutoCancel(true)
        context.packageManager.getLaunchIntentForPackage(context.packageName)?.let { intent ->
            builder.setContentIntent(PendingIntent.getActivity(context, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        }
        manager.notify("phone-tool:$id", 0, builder.build())
    }

    /** phone_notifications_list (Android only): what Persona shows now, newest first, never another app's. */
    fun list(limit: Int): PhoneToolResult {
        val shown = manager.activeNotifications.filter { it.notification.channelId != PhoneConfirmations.CHANNEL }
            .sortedByDescending { it.postTime }.take(limit)
        return PhoneToolResult.text(buildJsonObject {
            put("count", shown.size)
            put("notifications", buildJsonArray {
                shown.forEach { notification -> add(buildJsonObject {
                    put("id", notification.key)
                    put("title", notification.notification.extras.getCharSequence(Notification.EXTRA_TITLE)?.toString().orEmpty())
                    put("body", notification.notification.extras.getCharSequence(Notification.EXTRA_TEXT)?.toString().orEmpty())
                    put("posted", iso(notification.postTime))
                }) }
            })
        }.toString())
    }

    // The channel the user sees in system settings; the name says who sends these (the old app's wording).
    private fun channel() { manager.createNotificationChannel(NotificationChannel(CHANNEL, context.getString(R.string.phonetools_messages_channel), NotificationManager.IMPORTANCE_DEFAULT)) }
    companion object { const val CHANNEL = "phone_tools_messages" }
}

open class PhoneNotificationWorker(context: Context, parameters: WorkerParameters) : CoroutineWorker(context, parameters) {
    override suspend fun doWork(): Result {
        val id = inputData.getString("id") ?: return Result.failure()
        val title = inputData.getString("title") ?: return Result.failure()
        val body = inputData.getString("body") ?: return Result.failure()
        return try { PhoneNotifications(applicationContext).post(id, title, body); Result.success() }
        catch (_: Exception) { Result.failure() }
    }
}
