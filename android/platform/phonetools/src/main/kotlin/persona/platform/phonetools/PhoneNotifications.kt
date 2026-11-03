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
    suspend fun schedule(args: JsonObject): PhoneToolResult {
        channel()
        check(manager.areNotificationsEnabled() && manager.getNotificationChannel(CHANNEL).importance != NotificationManager.IMPORTANCE_NONE) {
            "请在系统设置中允许 Persona 通知及手机工具通知渠道。"
        }
        val id = UUID.randomUUID().toString()
        val title = requireNotNull(args.string("title"))
        val body = requireNotNull(args.string("body"))
        val delay = args.int("delay_seconds", 0)
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
            put("id", id); put("scheduled", delay > 0)
            if (delay > 0) put("note", "已交给系统调度，发送时间可能延后。")
        }.toString())
    }
    fun post(id: String, title: String, body: String) {
        channel()
        check(manager.areNotificationsEnabled() && manager.getNotificationChannel(CHANNEL).importance != NotificationManager.IMPORTANCE_NONE)
        val builder = Notification.Builder(context, CHANNEL).setSmallIcon(R.drawable.phone_tool_notification)
            .setContentTitle(title).setContentText(body).setStyle(Notification.BigTextStyle().bigText(body))
            .setVisibility(Notification.VISIBILITY_PRIVATE).setAutoCancel(true)
        context.packageManager.getLaunchIntentForPackage(context.packageName)?.let { intent ->
            builder.setContentIntent(PendingIntent.getActivity(context, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        }
        manager.notify("phone-tool:$id", 0, builder.build())
    }
    fun list(limit: Int): PhoneToolResult = PhoneToolResult.text(buildJsonArray {
        manager.activeNotifications.filter { it.notification.channelId != PhoneConfirmations.CHANNEL }
            .sortedByDescending { it.postTime }.take(limit).forEach { notification -> add(buildJsonObject {
                put("id", notification.key); put("title", notification.notification.extras.getCharSequence(Notification.EXTRA_TITLE)?.toString().orEmpty())
                put("body", notification.notification.extras.getCharSequence(Notification.EXTRA_TEXT)?.toString().orEmpty())
                put("posted", iso(notification.postTime))
            }) }
    }.toString())
    private fun channel() { manager.createNotificationChannel(NotificationChannel(CHANNEL, "手机工具通知", NotificationManager.IMPORTANCE_DEFAULT)) }
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
