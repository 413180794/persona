package persona.platform.notify

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.RemoteInput
import androidx.core.content.ContextCompat
import dagger.hilt.android.qualifiers.ApplicationContext
import persona.core.designsystem.PendingStatus
import persona.core.designsystem.Status
import persona.core.contracts.FleetState
import persona.core.contracts.Interaction
import persona.core.contracts.NotifyEvent
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.DoneNotification
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.collect
import persona.core.contracts.api.SessionsRepository
import persona.core.navigation.AppTab
import persona.core.navigation.NotificationRoute
import persona.core.navigation.TerminalRoute
import persona.core.domain.answer.RequestExpiry
import java.time.Instant
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class Notifier @Inject constructor(
    @ApplicationContext private val context: Context,
    private val sessions: SessionsRepository,
    private val preferences: NotificationPreferencesSource,
) {
    private val manager = NotificationManagerCompat.from(context)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val showLockDetails get() = preferences.preferences.value?.lockDetails == true

    init {
        scope.launch {
            var previous: persona.core.contracts.api.DeviceNotificationPreferences? = null
            preferences.preferences.collect { current ->
                if (current == null || previous?.lockDetails == true && !current.lockDetails ||
                    previous?.input == true && !current.input ||
                    previous?.done != DoneNotification.Off && current.done == DoneNotification.Off) clear()
                previous = current
            }
        }
        context.getSystemService(NotificationManager::class.java).apply {
            deleteNotificationChannel("ongoing")
            createNotificationChannels(listOf(
                NotificationChannel(CH_INPUT, "需要你", NotificationManager.IMPORTANCE_HIGH),
                NotificationChannel(CH_DONE, "做完了", NotificationManager.IMPORTANCE_DEFAULT),
                NotificationChannel(CH_ASSISTANT, "Jarvis", NotificationManager.IMPORTANCE_DEFAULT),
                NotificationChannel(CH_STATUS, "实时状态", NotificationManager.IMPORTANCE_LOW).apply {
                    setSound(null, null)
                    enableVibration(false)
                    setShowBadge(false)
                },
            ))
        }
    }

    private fun isAssistant(session: SessionKey) = sessions.sessions.value.firstOrNull { it.key == session }?.assistant

    private fun open(session: SessionKey?, assistant: Boolean? = null, terminal: Boolean = false): PendingIntent {
        val uri = if (terminal && session != null) Uri.parse(terminalLink(TerminalRoute(session.machineId, session.sessionId)))
        else session?.let {
            NotificationRoute.conversation(it.machineId, it.sessionId, assistant ?: isAssistant(it))
        }?.uri() ?: NotificationRoute(AppTab.Agents).uri()
        val intent = Intent(Intent.ACTION_VIEW, uri).setPackage(context.packageName)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        return PendingIntent.getActivity(context, 0, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    fun ongoing(state: ConnectionState, fleet: FleetState = FleetState()): Notification =
        ongoing(OngoingStatus.from(state, fleet))

    internal fun ongoing(status: OngoingStatus): Notification {
        val public = NotificationCompat.Builder(context, CH_STATUS).setSmallIcon(R.drawable.notify_status)
            .setContentTitle(status.title).setContentText(status.lockScreenText(false))
            .setShowWhen(false)
            .setVisibility(NotificationCompat.VISIBILITY_PUBLIC).build()
        val notification = base(CH_STATUS)
            .setContentTitle(status.title)
            .setContentText(status.counts)
            .setStyle(NotificationCompat.BigTextStyle().bigText(status.lockScreenText(showLockDetails)))
            .apply { if (!showLockDetails) setPublicVersion(public) }
            .setOngoing(true)
            .setSilent(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(false)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .setContentIntent(open(null))
            .build()
        val enabled = preferences.preferences.value?.liveActivity == true
        if (Build.VERSION.SDK_INT < 36 || !enabled) return notification
        // Promotion is optional: OEM policy or user settings must never break the foreground service.
        return try {
            val allowed = context.getSystemService(NotificationManager::class.java).canPostPromotedNotifications()
            if (!status.shouldPromote(Build.VERSION.SDK_INT, allowed, enabled)) notification
            else Notification.Builder.recoverBuilder(context, notification)
                .setRequestPromotedOngoing(true)
                .setShortCriticalText(status.criticalText)
                .apply {
                    if (Build.VERSION.SDK_INT >= 37) {
                        setStyle(Notification.MetricStyle()
                            .addMetric(Notification.Metric(Notification.Metric.FixedInt(status.needsYou), Status.NeedsYou.label))
                            .addMetric(Notification.Metric(Notification.Metric.FixedInt(status.working), Status.Working.label))
                            .addMetric(Notification.Metric(Notification.Metric.FixedInt(status.unread), Status.Unread.label))
                            .setCriticalMetric(if (status.needsYou > 0) 0 else if (status.working > 0) 1 else 2))
                        setContentText(status.lockScreenText(showLockDetails))
                    }
                }
                .build()
        } catch (_: RuntimeException) {
            notification
        }
    }

    fun updateOngoing(state: ConnectionState, fleet: FleetState) = post(null, ONGOING_ID, ongoing(state, fleet))

    internal fun updateOngoing(status: OngoingStatus) = post(null, ONGOING_ID, ongoing(status))

    fun postInteraction(ix: Interaction) {
        if (preferences.preferences.value?.input != true) return
        if (NotificationPolicy.expired(ix)) { retract(ix.id); return }
        val assistant = isAssistant(ix.sessionKey)
        val title = if (assistant == true && ix.kind == "question") "Jarvis 在问你"
            else ix.sessionTitle?.takeIf { it.isNotBlank() } ?: "需要你"
        val detail = when (ix.kind) {
            "question" -> ix.questions.orEmpty().joinToString("\n") { it.question }
            "plan" -> ix.plan ?: ix.headline
            "permission" -> ix.summary ?: ix.headline
            "screen" -> ix.dialog?.let { "${it.question}\n${it.detail.orEmpty()}" } ?: ix.screen.orEmpty()
            else -> ix.headline
        }
        val builder = base(CH_INPUT).forSession(ix.sessionKey).setContentTitle(title).setContentText(ix.headline)
            .setStyle(NotificationCompat.BigTextStyle().bigText(detail.take(2000)))
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setOnlyAlertOnce(true)
            .setAutoCancel(true)
            .setContentIntent(open(ix.sessionKey))
        ix.expiresAt?.let { deadline ->
            builder.setTimeoutAfter((Instant.parse(deadline).toEpochMilli() - System.currentTimeMillis()).coerceAtLeast(1))
        }
        NotificationPolicy.actions(ix).forEach { action -> builder.addAction(action(ix, action)) }
        post(interactionTag(ix.id), EVENT_ID, builder.build())
    }

    private fun action(ix: Interaction, action: NotificationAction): NotificationCompat.Action {
        val intent = Intent(context, ActionReceiver::class.java).apply {
            this.action = ActionReceiver.RESOLVE
            data = Uri.Builder().scheme("jarvis-notify").authority("resolve")
                .appendPath(ix.id).appendPath(action.key).build()
            putExtra(ActionReceiver.EXTRA_MACHINE, ix.machineId)
            putExtra(ActionReceiver.EXTRA_SESSION, ix.sessionId)
            isAssistant(ix.sessionKey)?.let { putExtra(ActionReceiver.EXTRA_ASSISTANT, it) }
            putExtra(ActionReceiver.EXTRA_ID, ix.id)
            putExtra(ActionReceiver.EXTRA_EXPIRES_AT, ix.expiresAt)
            putExtra(ActionReceiver.EXTRA_ACTION, action.key)
        }
        val mutable = if (action.replyLabel != null) PendingIntent.FLAG_MUTABLE else PendingIntent.FLAG_IMMUTABLE
        val pending = PendingIntent.getBroadcast(context, 0, intent, PendingIntent.FLAG_UPDATE_CURRENT or mutable)
        return NotificationCompat.Action.Builder(0, action.label.replace(Regex("\\s+"), " ").trim(), pending)
            .setAuthenticationRequired(action.authenticationRequired)
            .apply {
                action.replyLabel?.let { label ->
                    addRemoteInput(RemoteInput.Builder(ActionReceiver.EXTRA_REPLY).setLabel(label).build())
                }
            }.build()
    }

    fun postNotify(event: NotifyEvent) {
        val channel = NotificationPolicy.channel(event) ?: return
        val settings = preferences.preferences.value ?: return
        if (channel == CH_DONE && settings.done == DoneNotification.Off) return
        val session = SessionKey(event.machineId, event.sessionId)
        post("${event.kind}:${session.path}", EVENT_ID, base(channel).forSession(session)
            .setContentTitle(event.title.ifBlank { if (channel == CH_DONE) "做完了" else "Jarvis" })
            .setContentText(event.body.ifBlank { event.subtitle })
            .setSubText(event.subtitle.takeIf { it.isNotBlank() })
            .setStyle(NotificationCompat.BigTextStyle().bigText(event.body.take(2000)))
            .setAutoCancel(true).setContentIntent(open(session, if (event.kind == "assistant") true else isAssistant(session))).build())
    }

    fun retract(id: String) = manager.cancel(interactionTag(id), EVENT_ID)

    internal fun pending(id: String, session: SessionKey?, status: PendingStatus, assistant: Boolean?) =
        resultNotice(id, session, status.label, "请打开会话查看。", assistant)

    fun retractSession(session: SessionKey) {
        context.getSystemService(NotificationManager::class.java).activeNotifications
            .filter { it.id == EVENT_ID && it.notification.extras.getString("jarvis.session") == session.path }
            .forEach { manager.cancel(it.tag, it.id) }
    }

    private fun NotificationCompat.Builder.forSession(session: SessionKey?) = apply {
        session?.let { addExtras(android.os.Bundle().apply { putString("jarvis.session", it.path) }) }
    }

    fun failure(id: String, session: SessionKey?, assistant: Boolean? = null) = resultNotice(
        id, session, PendingStatus.Unconfirmed.label, "请打开会话核对结果，不要重复提交。", assistant,
    )

    internal fun actionResult(id: String, session: SessionKey?, result: NotificationActionResult, assistant: Boolean?) {
        val (title, detail) = actionResultCopy(result) ?: return
        resultNotice(id, session, title, detail, assistant, terminal = result == NotificationActionResult.Expired)
    }

    private fun resultNotice(
        id: String, session: SessionKey?, title: String, detail: String, assistant: Boolean?, terminal: Boolean = false,
    ) {
        val destination = open(session, assistant, terminal)
        post(interactionTag(id), EVENT_ID, base(CH_INPUT).forSession(session)
            .setContentTitle(title).setContentText(detail)
            .setOnlyAlertOnce(true)
            .apply {
                if (terminal && session != null) addAction(0, RequestExpiry.terminalLabel, destination)
            }
            .setAutoCancel(true).setContentIntent(destination).build())
    }

    fun userStopped() {
        val intent = Intent(context, ActionReceiver::class.java).setAction(ActionReceiver.RESUME)
        val pending = PendingIntent.getBroadcast(context, 0, intent, PendingIntent.FLAG_IMMUTABLE)
        post("stopped", EVENT_ID, base(CH_STATUS)
            .setContentTitle("后台连接被你停止了").setContentText("点「恢复」重新接收通知")
            .addAction(NotificationCompat.Action.Builder(0, "恢复", pending).setAuthenticationRequired(true).build())
            .setAutoCancel(true).setContentIntent(open(null)).build())
    }

    fun clear() {
        context.getSystemService(NotificationManager::class.java).activeNotifications
            .filter { it.id == EVENT_ID && it.tag?.let { tag ->
                tag == "stopped" || listOf("interaction:", "failure:", "done:", "assistant:").any(tag::startsWith)
            } == true }.forEach { manager.cancel(it.tag, it.id) }
    }

    fun dismissStopped() = manager.cancel("stopped", EVENT_ID)

    private fun base(channel: String) = NotificationCompat.Builder(context, channel)
        .setSmallIcon(R.drawable.notify_status)
        .setVisibility(if (showLockDetails) NotificationCompat.VISIBILITY_PUBLIC else NotificationCompat.VISIBILITY_PRIVATE)
        .apply {
            if (!showLockDetails) setPublicVersion(NotificationCompat.Builder(context, channel)
                .setSmallIcon(R.drawable.notify_status).setContentTitle("Jarvis 有新消息")
                .setVisibility(NotificationCompat.VISIBILITY_PUBLIC).build())
        }

    private fun post(tag: String?, id: Int, notification: Notification) {
        if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS)
            != PackageManager.PERMISSION_GRANTED) return
        runCatching { manager.notify(tag, id, notification) }
    }

    companion object {
        const val CH_INPUT = "input"
        const val CH_DONE = "done"
        const val CH_ASSISTANT = "assistant"
        const val CH_STATUS = "realtime_status"
        const val ONGOING_ID = 1
        private const val EVENT_ID = 2
        private fun interactionTag(id: String) = "interaction:$id"
    }
}
