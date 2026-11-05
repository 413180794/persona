package persona.feature.chat

import persona.core.designsystem.resolve
import android.content.res.Resources
import androidx.compose.runtime.*
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/** Owned by the page so dismissing a sheet cannot start a second operation. */
internal class ChatStatusRecovery(
    private val res: Resources,
    private val service: ChatRecoveryService?,
    private val scope: CoroutineScope,
    private val refresh: suspend () -> Unit,
) {
    val available get() = service != null
    var busy by mutableStateOf(false); private set
    var resumeUnknown by mutableStateOf(false); private set
    var reconnectRequested by mutableStateOf(false); private set
    var restored by mutableStateOf<SessionKey?>(null); private set
    var notice by mutableStateOf<String?>(null); private set

    fun resume(session: Session) {
        val source = service ?: return
        if (busy || restored != null) return
        busy = true
        scope.launch {
            try {
                when (val result = source.resume(session)) {
                    is ChatRecoveryResult.Restored -> {
                        restored = result.session
                        resumeUnknown = false
                        notice = res.getString(R.string.chat_recovery_restored)
                        refreshSafely()
                    }
                    ChatRecoveryResult.Unknown -> unknownResume()
                    is ChatRecoveryResult.Failed -> { resumeUnknown = false; notice = result.reason.shown }
                }
            } catch (error: CancellationException) { throw error }
            catch (_: Exception) { unknownResume() }
            finally { busy = false }
        }
    }

    fun reconnect(machineId: String) {
        val source = service ?: return
        if (busy) return
        busy = true
        scope.launch {
            try {
                if (reconnectRequested) {
                    refresh()
                    notice = res.getString(R.string.chat_recovery_refreshed)
                } else {
                    reconnectRequested = true
                    when (val result = source.reconnect(machineId)) {
                        ChatActionResult.Done -> notice = res.getString(R.string.chat_recovery_reconnect_requested)
                        ChatActionResult.Unknown -> notice = res.getString(R.string.chat_recovery_reconnect_unknown)
                        is ChatActionResult.Failed -> { reconnectRequested = false; notice = result.reason.resolve(res) }
                    }
                    refreshSafely()
                }
            } catch (error: CancellationException) { throw error }
            catch (_: Exception) { notice = res.getString(R.string.chat_recovery_machine_unknown) }
            finally { busy = false }
        }
    }

    private fun unknownResume() {
        resumeUnknown = true
        notice = res.getString(R.string.chat_recovery_resume_unknown)
    }

    private suspend fun refreshSafely() {
        try { refresh() }
        catch (error: CancellationException) { throw error }
        catch (_: Exception) { notice = res.getString(R.string.chat_recovery_list_stale, notice.orEmpty()) }
    }
}
