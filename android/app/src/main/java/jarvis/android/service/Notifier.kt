package jarvis.android.service

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.RemoteInput
import androidx.core.content.ContextCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner
import jarvis.android.MainActivity
import jarvis.android.R
import jarvis.android.data.AgentState
import jarvis.android.data.ConnState
import jarvis.android.data.Hub
import jarvis.android.data.HubEvent
import jarvis.android.data.Interaction
import jarvis.android.data.Session
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * Local notifications for what the hub reports over the WebSocket (docs/android.md「通知」), mirroring the hub's own
 * push rules for iPhone (docs/server.md「推送策略」): anything that needs you right away, a finished turn only if
 * nobody looked at it in the app.
 */
class Notifier(private val context: Context) {
    private val nm = NotificationManagerCompat.from(context)
    private var job: Job? = null

    init {
        val sys = context.getSystemService(NotificationManager::class.java)
        sys.createNotificationChannel(NotificationChannel(CH_INPUT, "需要你处理", NotificationManager.IMPORTANCE_HIGH).apply {
            description = "Agent 提问、计划、授权请求"
        })
        sys.createNotificationChannel(NotificationChannel(CH_DONE, "完成", NotificationManager.IMPORTANCE_DEFAULT).apply {
            description = "Agent 做完了一轮"
        })
        sys.createNotificationChannel(NotificationChannel(CH_ONGOING, "后台连接", NotificationManager.IMPORTANCE_MIN).apply {
            description = "保持和 hub 的连接，用来及时收到通知"
            setShowBadge(false)
        })
    }

    fun attach(hub: Hub) {
        job?.cancel()
        job = jarvis.android.JarvisApp.instance.scope.launch {
            hub.events.collect { e ->
                when (e) {
                    is HubEvent.NewInteraction -> postInteraction(e.interaction)
                    is HubEvent.Resolved -> nm.cancel(e.id.hashCode())
                    is HubEvent.SessionChanged -> onSessionChanged(e.before, e.after)
                    is HubEvent.ToolCall -> Unit
                }
            }
        }
    }

    private val foreground: Boolean
        get() = ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)

    private fun allowed() = android.os.Build.VERSION.SDK_INT < 33 ||
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    private fun openIntent(key: String?, requestCode: Int): PendingIntent {
        val i = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
            key?.let { putExtra(MainActivity.EXTRA_SESSION, it) }
        }
        return PendingIntent.getActivity(context, requestCode, i, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    private fun action(ix: Interaction, act: String, label: String, reply: Boolean = false): NotificationCompat.Action {
        val i = Intent(context, ActionReceiver::class.java).apply {
            action = act
            putExtra(ActionReceiver.EXTRA_ID, ix.id)
        }
        val mutable = if (reply) PendingIntent.FLAG_MUTABLE else PendingIntent.FLAG_IMMUTABLE
        val pi = PendingIntent.getBroadcast(context, (ix.id + act).hashCode(), i, PendingIntent.FLAG_UPDATE_CURRENT or mutable)
        return NotificationCompat.Action.Builder(0, label, pi).apply {
            if (reply) addRemoteInput(RemoteInput.Builder(ActionReceiver.EXTRA_TEXT).setLabel("改成……").build())
        }.build()
    }

    fun postInteraction(ix: Interaction) {
        if (!allowed()) return
        val title = ix.sessionTitle?.takeIf { it.isNotBlank() } ?: ix.cwd?.substringAfterLast('/') ?: "Agent"
        val b = NotificationCompat.Builder(context, CH_INPUT)
            .setSmallIcon(R.drawable.ic_stat)
            .setContentTitle(title)
            .setContentText(ix.headline)
            .setStyle(NotificationCompat.BigTextStyle().bigText(detail(ix)))
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setAutoCancel(true)
            .setContentIntent(openIntent("${ix.machineId}/${ix.sessionId}", ix.id.hashCode()))
        when (ix.kind) {
            "permission" -> {
                b.addAction(action(ix, ActionReceiver.ALLOW, "允许"))
                b.addAction(action(ix, ActionReceiver.DENY, "拒绝"))
            }
            "plan" -> {
                b.addAction(action(ix, ActionReceiver.APPROVE_PLAN, "批准"))
                b.addAction(action(ix, ActionReceiver.REJECT_PLAN, "打回", reply = true))
            }
            "screen" -> ix.dialog?.options?.filter { it.kind != "type_something" && it.kind != "chat" }?.take(3)?.forEach { o ->
                b.addAction(action(ix, ActionReceiver.OPTION + o.key, o.label))
            }
            "question" -> {
                val q = ix.questions?.singleOrNull()
                if (q != null && !q.multiSelect) q.options.take(3).forEachIndexed { n, o ->
                    b.addAction(action(ix, ActionReceiver.ANSWER + n, o.label))
                }
            }
        }
        runCatching { nm.notify(ix.id.hashCode(), b.build()) }
    }

    private fun detail(ix: Interaction): String = when (ix.kind) {
        "question" -> ix.questions.orEmpty().joinToString("\n") { q -> q.question + q.options.joinToString("") { "\n· ${it.label}" } }
        "plan" -> ix.plan?.take(600) ?: ix.headline
        "permission" -> ix.summary?.takeIf { it.isNotBlank() } ?: ix.headline
        "screen" -> ix.dialog?.let { d -> d.question + d.options.joinToString("") { "\n${it.key}. ${it.label}" } } ?: ix.screen?.takeLast(600).orEmpty()
        else -> ix.headline
    }

    /** A turn just ended unseen: notify unless the app is open (the user sees it there). */
    private fun onSessionChanged(before: Session?, after: Session) {
        if (before == null || after.assistant) return
        if (before.agentState == AgentState.Working && after.agentState == AgentState.Unread && !foreground && allowed()) {
            val n = NotificationCompat.Builder(context, CH_DONE)
                .setSmallIcon(R.drawable.ic_stat)
                .setContentTitle("${after.displayTitle} 做完了")
                .setContentText(after.lastText?.lineSequence()?.firstOrNull { it.isNotBlank() } ?: "")
                .setStyle(NotificationCompat.BigTextStyle().bigText(after.lastText?.take(600) ?: ""))
                .setAutoCancel(true)
                .setContentIntent(openIntent(after.key.path, after.key.path.hashCode()))
                .build()
            runCatching { nm.notify(after.key.path.hashCode(), n) }
        }
        if (after.agentState != AgentState.Unread && before.agentState == AgentState.Unread) nm.cancel(after.key.path.hashCode())
    }

    fun ongoing(conn: ConnState, pending: Int) = NotificationCompat.Builder(context, CH_ONGOING)
        .setSmallIcon(R.drawable.ic_stat)
        .setContentTitle(
            when (conn) {
                ConnState.Online -> if (pending > 0) "$pending 件事需要你" else "已连接 hub"
                ConnState.Connecting -> "正在连接 hub"
                ConnState.Offline -> "hub 连不上，稍后重试"
            },
        )
        .setOngoing(true)
        .setSilent(true)
        .setShowWhen(false)
        .setContentIntent(openIntent(null, 0))
        .build()

    fun updateOngoing(conn: ConnState, pending: Int) {
        if (allowed()) runCatching { nm.notify(ONGOING_ID, ongoing(conn, pending)) }
    }

    companion object {
        const val CH_INPUT = "input"
        const val CH_DONE = "done"
        const val CH_ONGOING = "ongoing"
        const val ONGOING_ID = 1
    }
}
