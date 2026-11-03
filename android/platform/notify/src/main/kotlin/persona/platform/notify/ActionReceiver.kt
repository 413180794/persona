package persona.platform.notify

import android.app.KeyguardManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.RemoteInput
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout

/** Resolve through the shared interaction center; ambiguous failures are never retried automatically. */
open class ActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == RESUME) {
            if (!context.getSystemService(KeyguardManager::class.java).isDeviceLocked) Notify.resume(context)
            return
        }
        if (intent.action != RESOLVE) return
        val id = intent.getStringExtra(EXTRA_ID) ?: return
        val action = intent.getStringExtra(EXTRA_ACTION) ?: return
        val reply = RemoteInput.getResultsFromIntent(intent)?.getCharSequence(EXTRA_REPLY)?.toString()
        val dependencies = Notify.dependencies(context)
        val pending = goAsync()
        CoroutineScope(SupervisorJob() + Dispatchers.IO).launch {
            var session = intent.getStringExtra(EXTRA_MACHINE)?.let { machine ->
                intent.getStringExtra(EXTRA_SESSION)?.let { persona.core.contracts.SessionKey(machine, it) }
            }
            val assistant = if (intent.hasExtra(EXTRA_ASSISTANT)) intent.getBooleanExtra(EXTRA_ASSISTANT, false) else null
            // Taps on the ongoing notification: no 核对中 / 发送中 notice of its own (that would pop a new heads-up when no
            // 需要你处理 notification is showing); results and failures still get one.
            val quiet = intent.getBooleanExtra(EXTRA_QUIET, false)
            try {
                // A bounded receiver lifetime is a timeout, not a delivery guarantee.
                withTimeout(8_000) {
                    val sessions = dependencies.sessions()
                    val result = NotificationActionExecutor(
                        dependencies.interactions(), dependencies.actionJournal()::claim,
                        session = { key ->
                            // A cold start may not have the list yet: one refresh, then the hub decides.
                            sessions.sessions.value.firstOrNull { it.key == key } ?: run {
                                try { sessions.refresh() } catch (e: CancellationException) { throw e } catch (_: Exception) { }
                                sessions.sessions.value.firstOrNull { it.key == key }
                            }
                        },
                    ).execute(id, action, reply,
                        locked = { context.getSystemService(KeyguardManager::class.java).isDeviceLocked },
                        onInteraction = { session = it.sessionKey },
                        expiresAt = intent.getStringExtra(EXTRA_EXPIRES_AT),
                        onProgress = { if (!quiet) dependencies.notifier().pending(id, session, it, assistant) },
                    )
                    if (result != NotificationActionResult.Locked) dependencies.notifier().retract(id)
                    if (result != NotificationActionResult.Submitted) {
                        dependencies.notifier().actionResult(id, session, result, assistant)
                    }
                }
            } catch (failure: Exception) {
                dependencies.notifier().failure(id, session, assistant)
                if (failure is CancellationException) throw failure
            } finally {
                pending.finish()
            }
        }
    }

    companion object {
        internal const val RESOLVE = "jarvis.notify.resolve"
        internal const val RESUME = "jarvis.notify.resume"
        internal const val EXTRA_MACHINE = "machine_id"
        internal const val EXTRA_SESSION = "session_id"
        internal const val EXTRA_ASSISTANT = "assistant"
        internal const val EXTRA_ID = "interaction_id"
        internal const val EXTRA_ACTION = "resolution_action"
        internal const val EXTRA_REPLY = "resolution_reply"
        internal const val EXTRA_EXPIRES_AT = "expires_at"
        internal const val EXTRA_QUIET = "quiet"
    }
}
