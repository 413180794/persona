package persona.platform.shortcuts

import android.app.KeyguardManager
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import persona.core.contracts.Interaction
import persona.core.contracts.Machine
import persona.core.contracts.api.InteractionCenter
import persona.core.designsystem.AppStyle
import persona.core.designsystem.AppStyleStore
import persona.core.designsystem.JDialog
import persona.core.designsystem.JSpinner
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

/** What the 「批准第一个待处理」 dialog shows. */
sealed interface ApproveState {
    data object Loading : ApproveState
    data object NotPaired : ApproveState
    data object Nothing : ApproveState
    /** A question or a terminal dialog: it needs the conversation. */
    data class NeedsApp(val request: Interaction, val where: String?) : ApproveState
    data class Confirm(val request: Interaction, val where: String?, val sending: Boolean = false) : ApproveState
    data class Failed(val message: String) : ApproveState
}

/**
 * 「批准第一个待处理」 (iOS ApproveFirstIntent): reads the pending requests fresh from the hub, shows the oldest and settles
 * it after one tap. Nothing is sent twice: the button is off while a send runs, and a send whose result is unknown is
 * not retried (the user checks it in the app).
 */
class ApproveFirstModel(
    private val paired: () -> Boolean,
    private val center: InteractionCenter,
    private val machines: () -> List<Machine>,
) {
    private val mutable = MutableStateFlow<ApproveState>(ApproveState.Loading)
    val state: StateFlow<ApproveState> = mutable.asStateFlow()

    suspend fun load() {
        if (!paired()) { mutable.value = ApproveState.NotPaired; return }
        mutable.value = ApproveState.Loading
        mutable.value = try {
            // The socket may have been asleep: ask the hub, as the iOS intent does.
            val before = center.pending.value
            withTimeoutOrNull(LOAD_TIMEOUT_MILLIS) { center.refresh(); true } ?: throw HubFailure(HubError.Timeout)
            // The app's InteractionCenter republishes the refreshed list from another coroutine: give it a moment
            // (an unchanged list is not published again, so this only waits when nothing changed).
            val requests = withTimeoutOrNull(SETTLE_MILLIS) { center.pending.first { it !== before } } ?: center.pending.value
            val request = ShortcutText.pending(requests)
            val where = request?.let { ShortcutText.where(it, machines()) }
            when {
                request == null -> ApproveState.Nothing
                ShortcutText.resolution(request) == null -> ApproveState.NeedsApp(request, where)
                else -> ApproveState.Confirm(request, where)
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) { ApproveState.Failed(ShortcutText.loadFailure(failure)) }
    }

    /** Sends the answer once; the toast to show when it went through, null otherwise (the state says why). */
    suspend fun approve(locked: Boolean): String? {
        val current = mutable.value as? ApproveState.Confirm ?: return null
        if (current.sending) return null
        // Saying yes needs an unlocked phone (the shared policy's launcher entry).
        val resolution = ShortcutText.resolution(current.request, locked)
            ?: run { mutable.value = ApproveState.Failed("请先解锁手机，再批准。"); return null }
        mutable.value = current.copy(sending = true)
        return try {
            center.resolve(current.request.id, resolution)
            ShortcutText.done(current.request)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) {
            mutable.value = ApproveState.Failed(ShortcutText.sendFailure(failure))
            null
        }
    }

    private companion object {
        const val LOAD_TIMEOUT_MILLIS = 15_000L
        const val SETTLE_MILLIS = 300L
    }
}

/**
 * The launcher's 「批准待处理」. Not exported: only the shortcut the app published, started on its behalf, reaches it, so a
 * link from another app can never approve anything. Only its dialog shows, over whatever was on screen.
 */
class ApproveFirstActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val dependencies = SurfacesDependencies.of(this)
        val model = ApproveFirstModel({ dependencies.pairing().isPaired.value }, dependencies.interactions(),
            { dependencies.sessions().machines.value })
        val keyguard = getSystemService(KeyguardManager::class.java)
        val style = AppStyleStore(this).style.value
        setContent {
            ShortcutTheme(style) {
                val state by model.state.collectAsStateWithLifecycle()
                LaunchedEffect(model) { model.load() }
                ApproveFirstDialog(state, onDismiss = ::finish,
                    onOpen = { request ->
                        startActivity(request?.let {
                            SurfaceLinks.conversation(this, it.sessionKey, dependencies.sessions().assistant(it.sessionKey))
                        } ?: SurfaceLinks.app(this))
                        finish()
                    },
                    onApprove = {
                        fun send() = lifecycleScope.launch {
                            model.approve(keyguard.isDeviceLocked)?.let { done ->
                                Toast.makeText(applicationContext, done, Toast.LENGTH_SHORT).show()
                                finish()
                            }
                        }
                        if (!keyguard.isDeviceLocked) send()
                        else keyguard.requestDismissKeyguard(this, object : KeyguardManager.KeyguardDismissCallback() {
                            override fun onDismissSucceeded() { send() }
                        })
                    })
            }
        }
    }

    companion object {
        const val ACTION_APPROVE = "jarvis.platform.shortcuts.APPROVE"
    }
}

/** The app's theme for a window of its own: the user's style, light or dark with the system. */
@Composable
internal fun ShortcutTheme(style: AppStyle, dark: Boolean? = null, content: @Composable () -> Unit) =
    PersonaTheme(darkTheme = dark ?: isSystemInDarkTheme(), style = style, content = content)

/** The dialog for each state; the debug preview draws the same one. */
@Composable
fun ApproveFirstDialog(
    state: ApproveState,
    onApprove: () -> Unit,
    onOpen: (Interaction?) -> Unit,
    onDismiss: () -> Unit,
) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    when (state) {
        ApproveState.Loading -> JDialog(onDismiss, ShortcutText.TITLE, content = {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                JSpinner()
                Text(ShortcutText.LOADING, style = type.callout, color = palette.ink2)
            }
        })
        ApproveState.NotPaired -> JDialog(onDismiss, ShortcutText.TITLE, text = ShortcutText.NOT_PAIRED,
            confirm = "打开 Persona" to { onOpen(null) })
        ApproveState.Nothing -> JDialog(onDismiss, ShortcutText.TITLE, text = ShortcutText.NO_PENDING,
            confirm = "好" to onDismiss, dismiss = null)
        is ApproveState.NeedsApp -> JDialog(onDismiss, ShortcutText.TITLE, content = {
            Request(ShortcutText.needsApp(state.request.kind), null, state.where)
        }, confirm = "打开 Persona" to { onOpen(state.request) })
        is ApproveState.Confirm -> JDialog(onDismiss = { if (!state.sending) onDismiss() }, title = ShortcutText.TITLE,
            content = {
                Request(ShortcutText.ask(state.request), ShortcutText.PLAN_NOTE.takeIf { state.request.kind == "plan" }, state.where)
            },
            confirm = ShortcutText.confirmLabel(state.request) to onApprove, confirmEnabled = !state.sending,
            dismiss = "取消" to { if (!state.sending) onDismiss() })
        is ApproveState.Failed -> JDialog(onDismiss, ShortcutText.TITLE, text = state.message, confirm = "好" to onDismiss, dismiss = null)
    }
}

@Composable
private fun Request(question: String, note: String?, where: String?) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        Text(question, style = type.body, color = palette.ink)
        note?.let { Text(it, style = type.caption, color = palette.ink2) }
        where?.let { Text(it, style = type.caption, color = palette.ink3, maxLines = 1) }
    }
}
