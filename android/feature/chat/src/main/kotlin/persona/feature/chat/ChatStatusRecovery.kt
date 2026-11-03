package persona.feature.chat

import androidx.compose.runtime.*
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/** Owned by the page so dismissing a sheet cannot start a second operation. */
internal class ChatStatusRecovery(
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
                        notice = "会话已恢复"
                        refreshSafely()
                    }
                    ChatRecoveryResult.Unknown -> unknownResume()
                    is ChatRecoveryResult.Failed -> { resumeUnknown = false; notice = result.reason }
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
                    notice = "已刷新状态，请查看电脑是否在线。"
                } else {
                    reconnectRequested = true
                    when (val result = source.reconnect(machineId)) {
                        ChatActionResult.Done -> notice = "已请求重连，等待电脑上线。"
                        ChatActionResult.Unknown -> notice = "重连结果待确认，请刷新状态核对。"
                        is ChatActionResult.Failed -> { reconnectRequested = false; notice = result.reason }
                    }
                    refreshSafely()
                }
            } catch (error: CancellationException) { throw error }
            catch (_: Exception) { notice = "暂时无法确认电脑状态，请稍后刷新。" }
            finally { busy = false }
        }
    }

    private fun unknownResume() {
        resumeUnknown = true
        notice = "恢复结果待确认。再次核对只会读取会话状态，不会重复启动。"
    }

    private suspend fun refreshSafely() {
        try { refresh() }
        catch (error: CancellationException) { throw error }
        catch (_: Exception) { notice = "${notice.orEmpty()}，列表暂未更新，请稍后查看。" }
    }
}
