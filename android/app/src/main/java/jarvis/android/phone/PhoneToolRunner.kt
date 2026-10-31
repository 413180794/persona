package jarvis.android.phone

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import jarvis.android.JarvisApp
import jarvis.android.MainActivity
import jarvis.android.R
import jarvis.android.data.Hub
import jarvis.android.data.HubEvent
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import java.util.concurrent.ConcurrentHashMap

/**
 * Answers the hub's `tool_call`s with the phone's own data (docs/phone-tools.md). Which sources are on is the
 * user's (设置 › 手机数据), kept on the phone; a source that is off isn't declared to the hub, and a call to it
 * (an old session still remembering it) is refused. Writes wait for the user's yes, from the app or a notification.
 */
class PhoneToolRunner(private val context: Context) {
    private val prefs = context.getSharedPreferences("phone_tools", Context.MODE_PRIVATE)
    private val _enabled = MutableStateFlow(load())
    val enabled: StateFlow<Set<Source>> = _enabled.asStateFlow()
    private var job: Job? = null
    private var hub: Hub? = null

    /** A write waiting for the user: the card the app shows, and its answer. */
    data class Pending(val id: String, val tool: String, val summary: String, val answer: CompletableDeferred<Boolean>)
    private val _pending = MutableStateFlow<List<Pending>>(emptyList())
    val pending: StateFlow<List<Pending>> = _pending.asStateFlow()

    private fun load(): Set<Source> =
        prefs.getStringSet("enabled", emptySet())!!.mapNotNull { n -> Source.entries.firstOrNull { it.name == n } }.toSet()

    fun setEnabled(s: Source, on: Boolean) {
        _enabled.update { if (on) it + s else it - s }
        prefs.edit().putStringSet("enabled", _enabled.value.map { it.name }.toSet()).apply()
        hub?.updatePhoneTools(PhoneTools.specs(_enabled.value))
    }

    fun attach(hub: Hub) {
        this.hub = hub
        hub.phoneTools = PhoneTools.specs(_enabled.value) // declared on every (re)connect
        job?.cancel()
        job = JarvisApp.instance.scope.launch {
            hub.events.collect { e -> if (e is HubEvent.ToolCall) launch { answer(hub, e) } }
        }
    }

    private suspend fun answer(hub: Hub, call: HubEvent.ToolCall) {
        val tool = PhoneTools.byName(call.name)
        val out = when {
            tool == null -> ToolOutput.text("This phone has no tool ${call.name}.", true)
            tool.source !in _enabled.value -> ToolOutput.text("The user turned off ${tool.source.name} for Claude on this phone (Jarvis › 设置 › 手机数据).", true)
            else -> try {
                val ok = tool.confirm?.let { summary -> ask(call.callId, tool.name, summary(call.args)) } ?: true
                if (!ok) ToolOutput.text("The user declined on the phone.", true) else tool.run(context, call.args)
            } catch (e: ToolFailure) {
                ToolOutput.text(e.message.orEmpty(), true)
            } catch (e: Exception) {
                ToolOutput.text("${tool.name} failed on the phone: ${e.message ?: e.javaClass.simpleName}", true)
            }
        }
        hub.sendToolResult(call.callId, out.content, out.isError)
    }

    /** Asks the user to allow a write: a card in the app and a notification with 允许 / 拒绝; 90 s, then no. */
    private suspend fun ask(id: String, tool: String, summary: String): Boolean {
        val p = Pending(id, tool, summary, CompletableDeferred())
        _pending.update { it + p }
        postAsk(p)
        val yes = withTimeoutOrNull(90_000) { p.answer.await() } ?: false
        _pending.update { list -> list.filterNot { it.id == id } }
        NotificationManagerCompat.from(context).cancel(id.hashCode())
        return yes
    }

    fun decide(id: String, yes: Boolean) { _pending.value.firstOrNull { it.id == id }?.answer?.complete(yes) }

    @android.annotation.SuppressLint("MissingPermission")
    private fun postAsk(p: Pending) {
        val nm = context.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(CH_ASK, "Claude 要改手机上的数据", NotificationManager.IMPORTANCE_HIGH))
        fun act(yes: Boolean) = PendingIntent.getBroadcast(
            context, (p.id + yes).hashCode(),
            Intent(context, AskReceiver::class.java).putExtra("id", p.id).putExtra("yes", yes),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val open = PendingIntent.getActivity(context, p.id.hashCode(), Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK), PendingIntent.FLAG_IMMUTABLE)
        val n = NotificationCompat.Builder(context, CH_ASK).setSmallIcon(R.drawable.ic_stat)
            .setContentTitle("Claude 想在手机上操作").setContentText(p.summary)
            .setStyle(NotificationCompat.BigTextStyle().bigText(p.summary)).setPriority(NotificationCompat.PRIORITY_HIGH)
            .setContentIntent(open).addAction(0, "允许", act(true)).addAction(0, "拒绝", act(false)).build()
        runCatching { NotificationManagerCompat.from(context).notify(p.id.hashCode(), n) }
    }

    companion object {
        const val CH_ASK = "phone_ask"
    }
}

class AskReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val id = intent.getStringExtra("id") ?: return
        JarvisApp.instance.phoneTools.decide(id, intent.getBooleanExtra("yes", false))
    }
}
