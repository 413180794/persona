package jarvis.android.fleet

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.Toast
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import jarvis.android.JarvisApp
import jarvis.android.MainActivity
import jarvis.android.R
import jarvis.android.data.ConnState
import jarvis.android.data.HubException
import jarvis.android.data.Resolution
import jarvis.android.data.SessionKey
import jarvis.android.service.Notifier
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout

/**
 * The Live Activity's Android counterpart (docs/live-activity.md, docs/android.md「常驻通知」): the foreground service's
 * ongoing notification sums up every agent that needs you, is working or finished unseen, per machine, and the
 * first request a button can settle gets 拒绝 / 允许 (授权) or 查看 / 批准 (计划). With nothing to show, or the hub not
 * reachable, it falls back to the connection line ([Notifier.ongoing], the minimum-priority 「后台连接」 channel).
 * On Android 16 it asks to be promoted (a Live Update in the status bar); older versions show it as a plain ongoing
 * notification. Muted agents never appear ([FleetState.build] uses the hub's mute rule), as with the other notifications.
 */
object FleetNotification {
    const val CHANNEL = "fleet"

    // Each PendingIntent here has its own request code and data URI, so none can be confused with another one
    // (Notifier's, the widget's) that differs only in extras.
    private const val REQ_OPEN = 0x7300
    private const val REQ_VIEW = 0x7301
    private const val REQ_ACTION = 0x7302

    /** Notification.EXTRA_REQUEST_PROMOTED_ONGOING (API 36, written out: this app compiles against 34). */
    private const val EXTRA_REQUEST_PROMOTED = "android.requestPromotedOngoing"

    internal const val EXTRA_IX = "jarvis.android.fleet.interaction"
    internal const val EXTRA_WHAT = "jarvis.android.fleet.what"

    fun ensureChannel(ctx: Context) {
        ctx.getSystemService(NotificationManager::class.java)?.createNotificationChannel(
            NotificationChannel(CHANNEL, "实时状态", NotificationManager.IMPORTANCE_LOW).apply {
                description = "常驻一条：各台电脑上几个 Agent 需要你、在工作、做完了（相当于 iPhone 的实时活动）"
                setShowBadge(false)
            },
        )
    }

    private fun allowed(ctx: Context) = Build.VERSION.SDK_INT < 33 ||
        ContextCompat.checkSelfPermission(ctx, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    /** The user turned 「实时状态」 off in the system settings: show only the connection line. */
    private fun channelOff(ctx: Context): Boolean =
        ctx.getSystemService(NotificationManager::class.java)?.getNotificationChannel(CHANNEL)?.importance == NotificationManager.IMPORTANCE_NONE

    /** Replaces the service's notification. [quiet]: only finished agents have been left for a while (the hub ends the activity then). */
    fun show(ctx: Context, conn: ConnState, st: FleetState, quiet: Boolean = false) {
        if (!allowed(ctx)) return
        val n = build(ctx, conn, st, quiet)
        runCatching { NotificationManagerCompat.from(ctx).notify(Notifier.ONGOING_ID, n) }
    }

    fun build(ctx: Context, conn: ConnState, st: FleetState, quiet: Boolean = false): Notification {
        ensureChannel(ctx)
        if (conn != ConnState.Online || st.isEmpty || quiet || channelOff(ctx)) {
            return JarvisApp.instance.notifier.ongoing(conn, st.needsYou)
        }
        val first = st.items.firstOrNull()
        val b = base(ctx, st)
            .setContentText(first?.let { "${it.title} · ${FleetText.line(it)}" })
            .setSubText(FleetText.rest(st).ifEmpty { null })
            .setStyle(NotificationCompat.BigTextStyle().bigText(bigText(st)))
        val since = st.workingSince
        if (st.phase == "working" && since != null) {
            // The clock runs on the phone from when the longest turn started (iOS Text(timerInterval:)).
            b.setWhen((since * 1000).toLong()).setShowWhen(true).setUsesChronometer(true)
        } else {
            b.setShowWhen(false)
        }
        st.firstActionable?.let { addActions(ctx, b, it) }
        // The lock screen gets counts and 「有个授权等你」 only (iOS default); the system's own 「敏感内容」 setting decides
        // whether the full version shows there.
        b.setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
        b.setPublicVersion(base(ctx, st).setContentText(first?.let(FleetText::anonymous)).setShowWhen(false).build())
        if (Build.VERSION.SDK_INT >= 36) b.addExtras(Bundle().apply { putBoolean(EXTRA_REQUEST_PROMOTED, true) })
        return b.build()
    }

    private fun base(ctx: Context, st: FleetState): NotificationCompat.Builder =
        NotificationCompat.Builder(ctx, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat)
            .setContentTitle(FleetText.headline(st))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setCategory(if (st.phase == "needsYou") NotificationCompat.CATEGORY_STATUS else NotificationCompat.CATEGORY_PROGRESS)
            .setContentIntent(PendingIntent.getActivity(ctx, REQ_OPEN, open(ctx, st), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))

    /** Up to three agents with their machine, then 「+N 个」. */
    private fun bigText(st: FleetState): String {
        val lines = st.items.map { "${it.title} · ${FleetText.line(it)}" + (if (it.place.isNotEmpty()) "（${it.place}）" else "") }.toMutableList()
        val hidden = st.hidden(st.items.size)
        if (hidden > 0) lines += FleetText.more(hidden)
        return lines.joinToString("\n")
    }

    /** One agent: open it. Several: the Agents tab. */
    fun open(ctx: Context, st: FleetState): Intent = st.single?.let { openSession(ctx, it) } ?: openTab(ctx, "agents")

    /** MainActivity on a chat; the data URI only keeps PendingIntents for different chats apart. */
    fun openSession(ctx: Context, key: SessionKey): Intent = Intent(ctx, MainActivity::class.java)
        .setData(Uri.Builder().scheme("jarvis-open").authority("session").appendPath(key.machineId).appendPath(key.sessionId).build())
        .putExtra(MainActivity.EXTRA_SESSION, key.path)
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)

    /** MainActivity on a tab: "agents", "jarvis", or "ask" (Jarvis with its composer focused). */
    fun openTab(ctx: Context, tab: String): Intent = Intent(ctx, MainActivity::class.java)
        .setData(Uri.Builder().scheme("jarvis-open").authority("tab").appendPath(tab).build())
        .putExtra(MainActivity.EXTRA_TAB, tab)
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)

    private fun addActions(ctx: Context, b: NotificationCompat.Builder, item: FleetItem) {
        val ix = item.interaction ?: return
        // One immutable PendingIntent per (interaction, button), told apart by the data URI: a token SystemUI holds
        // while the user unlocks can never be rewritten to another interaction by a later redraw.
        fun act(label: String, what: String, auth: Boolean) = NotificationCompat.Action.Builder(
            0, label,
            PendingIntent.getBroadcast(
                ctx, REQ_ACTION,
                Intent(ctx, FleetActionReceiver::class.java)
                    .setData(Uri.Builder().scheme("jarvis-fleet").authority("ix").appendPath(ix).appendPath(what).build())
                    .putExtra(EXTRA_IX, ix).putExtra(EXTRA_WHAT, what),
                PendingIntent.FLAG_IMMUTABLE,
            ),
        ).setAuthenticationRequired(auth).build()
        when (item.kind) {
            "permission" -> {
                b.addAction(act("拒绝", "deny", false))
                b.addAction(act("允许", "allow", true))
            }
            "plan" -> {
                val view = item.key?.let { openSession(ctx, it) } ?: openTab(ctx, "agents")
                b.addAction(
                    NotificationCompat.Action.Builder(
                        0, "查看", PendingIntent.getActivity(ctx, REQ_VIEW, view, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT),
                    ).build(),
                )
                b.addAction(act("批准", "approve", true))
            }
        }
    }
}

/**
 * The ongoing notification's buttons (iOS `FleetActions`). A plan approved from here uses acceptEdits (the whole plan
 * was not read; commands still ask), as the 「需要你处理」 notification's 批准 does.
 */
class FleetActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val id = intent.getStringExtra(FleetNotification.EXTRA_IX) ?: return
        val r = when (intent.getStringExtra(FleetNotification.EXTRA_WHAT)) {
            "allow" -> Resolution(allow = true)
            "deny" -> Resolution(allow = false)
            "approve" -> Resolution(approve = true, mode = "acceptEdits")
            else -> return
        }
        val app = context.applicationContext as JarvisApp
        val hub = app.hub.value ?: return
        val ix = hub.interactions.value.firstOrNull { it.id == id }
        // Hold the broadcast while the request runs: a process that exists only for this broadcast is frozen as soon
        // as onReceive returns, and the tap would be lost. goAsync allows about 10 seconds.
        val pending = goAsync()
        app.scope.launch {
            try {
                withTimeout(9_000L) { if (ix != null) hub.resolve(ix, r) else hub.api.resolve(id, r) }
                NotificationManagerCompat.from(context).cancel(id.hashCode()) // the 「需要你处理」 one for the same request
            } catch (e: Exception) {
                // 410: answered elsewhere already, nothing to say.
                if ((e as? HubException)?.status != 410) {
                    Handler(Looper.getMainLooper()).post {
                        Toast.makeText(app, "没有送达：${e.message}", Toast.LENGTH_LONG).show()
                    }
                }
            } finally {
                pending.finish()
            }
        }
    }
}
