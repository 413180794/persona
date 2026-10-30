package jarvis.android.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.widget.Toast
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.RemoteInput
import jarvis.android.JarvisApp
import jarvis.android.data.Resolution
import kotlinx.coroutines.launch

/** Buttons on an interaction notification: answer without opening the app. */
class ActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val id = intent.getStringExtra(EXTRA_ID) ?: return
        val act = intent.action ?: return
        val app = context.applicationContext as JarvisApp
        val hub = app.hub.value ?: return
        val ix = hub.interactions.value.firstOrNull { it.id == id }
        val reply = RemoteInput.getResultsFromIntent(intent)?.getCharSequence(EXTRA_TEXT)?.toString()
        val r: Resolution = when {
            act == ALLOW -> Resolution(allow = true)
            act == DENY -> Resolution(allow = false)
            act == APPROVE_PLAN -> Resolution(approve = true, mode = "acceptEdits")
            act == REJECT_PLAN -> Resolution(approve = false, feedback = reply.orEmpty())
            // The node re-reads the screen and checks the dialog's rev before pressing (409 prompt_changed otherwise).
            act.startsWith(OPTION) -> Resolution(option = act.removePrefix(OPTION))
            act.startsWith(ANSWER) -> {
                val q = ix?.questions?.singleOrNull() ?: return
                val label = q.options.getOrNull(act.removePrefix(ANSWER).toIntOrNull() ?: return)?.label ?: return
                Resolution(answers = mapOf(q.question to label))
            }
            else -> return
        }
        val pending = goAsync()
        app.scope.launch {
            try {
                hub.api.resolve(id, r)
                NotificationManagerCompat.from(context).cancel(id.hashCode())
            } catch (e: Exception) {
                android.os.Handler(context.mainLooper).post {
                    Toast.makeText(context, "没有送达：${e.message}", Toast.LENGTH_LONG).show()
                }
            } finally {
                pending.finish()
            }
        }
    }

    companion object {
        const val EXTRA_ID = "id"
        const val EXTRA_TEXT = "text"
        const val ALLOW = "jarvis.allow"
        const val DENY = "jarvis.deny"
        const val APPROVE_PLAN = "jarvis.approve_plan"
        const val REJECT_PLAN = "jarvis.reject_plan"
        const val ANSWER = "jarvis.answer."
        const val OPTION = "jarvis.option."
    }
}
