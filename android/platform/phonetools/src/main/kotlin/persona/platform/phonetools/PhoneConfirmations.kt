package persona.platform.phonetools

import android.app.*
import android.content.*
import android.net.Uri
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap

/** A token is valid only while its single local confirmation is pending. Nothing is persisted. */
class PhoneConfirmations(context: Context) : AutoCloseable {
    private val context = context.applicationContext
    private val manager = context.getSystemService(NotificationManager::class.java)
    private val keyguard = context.getSystemService(KeyguardManager::class.java)
    private val _pending = MutableStateFlow<PendingPhoneConfirmation?>(null)
    val pending = _pending.asStateFlow()
    private var answer: CompletableDeferred<Boolean>? = null
    @Volatile private var foreground = false

    suspend fun confirm(summary: String): Boolean {
        val token = UUID.randomUUID().toString()
        val deferred = CompletableDeferred<Boolean>()
        answer = deferred
        callbacks[token] = { approved -> resolve(token, approved) }
        _pending.value = PendingPhoneConfirmation(token, summary)
        return try {
            if (!foreground && !showNotification()) return false
            withTimeoutOrNull(45_000) { deferred.await() } ?: false
        } finally {
            callbacks.remove(token); manager.cancel(token, 0); _pending.value = null; answer = null
        }
    }
    fun resolve(token: String, allowed: Boolean) {
        if (_pending.value?.token != token) return
        if (allowed && keyguard.isDeviceLocked) return
        answer?.complete(allowed)
    }
    fun setForeground(value: Boolean) {
        foreground = value
        val current = _pending.value ?: return
        if (value) manager.cancel(current.token, 0)
        else if (!showNotification()) answer?.complete(false)
    }
    private fun showNotification(): Boolean {
        val current = _pending.value ?: return false
        return try {
            manager.createNotificationChannel(NotificationChannel(CHANNEL, "手机操作确认", NotificationManager.IMPORTANCE_HIGH))
            if (!manager.areNotificationsEnabled() || manager.getNotificationChannel(CHANNEL).importance == NotificationManager.IMPORTANCE_NONE) return false
            fun action(allowed: Boolean): Notification.Action {
                val intent = Intent(context, PhoneConfirmationReceiver::class.java)
                    .setAction("jarvis.phone.confirm").setData(Uri.parse("jarvis-phone://confirmation/${current.token}/$allowed"))
                    .putExtra("token", current.token).putExtra("allowed", allowed)
                val pendingIntent = PendingIntent.getBroadcast(context, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
                return Notification.Action.Builder(null, if (allowed) "允许" else "拒绝", pendingIntent)
                    .setAuthenticationRequired(allowed).build()
            }
            val builder = Notification.Builder(context, CHANNEL).setSmallIcon(R.drawable.phone_tool_notification)
                .setContentTitle("请确认手机操作").setContentText(current.summary)
                .setStyle(Notification.BigTextStyle().bigText(current.summary)).setVisibility(Notification.VISIBILITY_PRIVATE)
                .setCategory(Notification.CATEGORY_RECOMMENDATION).setTimeoutAfter(45_000)
                .addAction(action(false)).addAction(action(true))
            context.packageManager.getLaunchIntentForPackage(context.packageName)?.let { intent ->
                builder.setContentIntent(PendingIntent.getActivity(context, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            }
            manager.notify(current.token, 0, builder.build()); true
        } catch (_: Exception) { false }
    }
    override fun close() { answer?.complete(false); _pending.value?.let { callbacks.remove(it.token); manager.cancel(it.token, 0) } }
    companion object {
        internal const val CHANNEL = "phone_tools_confirmation"
        internal val callbacks = ConcurrentHashMap<String, (Boolean) -> Unit>()
    }
}

data class PendingPhoneConfirmation(val token: String, val summary: String)

class PhoneConfirmationReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != "jarvis.phone.confirm") return
        val token = intent.getStringExtra("token") ?: return
        val allowed = intent.getBooleanExtra("allowed", false)
        if (allowed && context.getSystemService(KeyguardManager::class.java).isDeviceLocked) return
        PhoneConfirmations.callbacks[token]?.invoke(allowed)
    }
}
