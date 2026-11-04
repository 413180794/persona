package persona.feature.chat

import androidx.compose.runtime.*
import persona.core.designsystem.operationFeedback
import persona.core.domain.FailureAction
import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import persona.core.domain.sort.SessionRename
import persona.core.domain.sort.SessionRenamer

internal fun spaceCompanions(session: Session?, sessions: List<Session>): List<Session> {
    if (session == null) return emptyList()
    val space = session.spaceId?.takeIf(String::isNotBlank) ?: session.workspaceId?.takeIf(String::isNotBlank)
    return sessions.filter { other ->
        other.machineId == session.machineId && other.key != session.key && !other.assistant &&
            if (space != null) (other.spaceId?.takeIf(String::isNotBlank) ?: other.workspaceId) == space
            else !session.spaceName.isNullOrBlank() && other.spaceName == session.spaceName
    }.sortedWith(compareBy<Session> { it.agentState.priority }.thenBy { it.slotIndex ?: Int.MAX_VALUE }.thenBy { it.sessionId })
}

internal fun connectionTitle(state: ConnectionState?): String? = when (state) {
    null, ConnectionState.Online -> null
    ConnectionState.Connecting -> "连接中…"
    ConnectionState.Updating -> "更新中…"
    ConnectionState.WaitingForNetwork -> "等待网络"
    ConnectionState.NeedsLocalNetworkPermission -> "需要本地网络权限"
    ConnectionState.CertificateMismatch -> "hub 的证书变了"
    ConnectionState.Revoked -> "配对已失效"
}

internal class ChatChromeState(
    private val actions: ChatActions?, private val scope: CoroutineScope,
    private val refresh: suspend () -> Unit, private val onClosed: () -> Unit,
    private val renamer: SessionRenamer? = null,
) {
    var busy by mutableStateOf(false); private set
    var muted by mutableStateOf<Boolean?>(null); private set
    var notice by mutableStateOf<String?>(null)
    var pending by mutableStateOf<ChatAction?>(null); private set
    val available get() = actions != null && !busy && pending == null
    val canRename get() = renamer != null && !busy && pending == null

    fun rename(session: Session, title: String?, tabName: String?) {
        val service = renamer ?: return
        val edit = SessionRename.changes(session, title, tabName)
        if (!canRename || !edit.changed) return
        busy = true
        scope.launch {
            try {
                if (service.rename(session, edit.title, edit.tabName)) notice = "已重命名"
            } catch (error: CancellationException) { throw error }
            catch (error: Exception) { notice = operationFeedback(error).message }
            finally { busy = false }
        }
    }

    /** 会话信息 (GET …/info); fails when the actions are not wired, which the sheet says. */
    suspend fun info(session: Session): SessionInfo =
        actions?.info(session) ?: throw UnsupportedOperationException("会话信息尚未接入")

    suspend fun loadMute(session: Session) {
        try { muted = actions?.isMuted(session) }
        catch (error: CancellationException) { throw error }
        catch (_: Exception) { muted = null }
    }

    fun run(session: Session, action: ChatAction, verify: Boolean = false) {
        val service = actions ?: return
        if (busy || (!verify && pending != null)) return
        busy = true
        scope.launch {
            try {
                when (val result = if (verify) service.verify(session, action) else service.execute(session, action)) {
                    ChatActionResult.Done -> {
                        pending = null
                        notice = when (action) {
                            ChatAction.Interrupt -> "已发出中断，等电脑确认"
                            is ChatAction.Rename -> "已重命名"
                            is ChatAction.Mute -> if (action.muted) "已屏蔽通知" else "已开启通知"
                            is ChatAction.Modes -> "已更新设置"
                            ChatAction.Close -> "已关闭 Agent"
                            ChatAction.Attach -> "已实时接入"
                            ChatAction.Delete -> "已删除对话"
                        }
                        if (action is ChatAction.Mute) muted = action.muted
                        if (action == ChatAction.Close || action == ChatAction.Delete) onClosed()
                        else try { refresh() } catch (error: CancellationException) { throw error }
                        catch (_: Exception) { notice = "操作已完成，会话信息稍后更新" }
                    }
                    ChatActionResult.Unknown -> { pending = action; notice = "操作结果待确认，请先核对" }
                    is ChatActionResult.Failed -> {
                        val feedback = result.cause?.let { operationFeedback(it) }
                        pending = action.takeIf { feedback?.action == FailureAction.Verify }
                        notice = feedback?.message ?: result.reason
                    }
                }
            } catch (error: CancellationException) { throw error }
            catch (error: Exception) {
                val feedback = operationFeedback(error)
                pending = action.takeIf { feedback.action == FailureAction.Verify }
                notice = feedback.message
            }
            finally { busy = false }
        }
    }
}
